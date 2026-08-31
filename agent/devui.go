//go:build android

package main

// devui.go: 悬浮控件应用的生命周期管理
//
// devui.dex 由 app_process 运行 (与 agent 解耦的组件, agent 只做编排):
//   devui_start  拉起 app_process (记录 pid 到 /data/local/devctl/devui.pid)
//   devui_stop   优雅停止: CmdFile quit 命令 → ShutdownHook 清理层; 超时 SIGTERM
//   devui_status 存活检查 + 层存在性提示
//
// 安全红线 (与 hide.go 一致): agent 只操作自身/自拉起的组件进程, 绝不 pkill
// 通配匹配 (曾有 pkill -f app_process 误杀系统进程的事故), 一律按 pid 精确操作。

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	devuiPidFile = "/data/local/devctl/devui.pid"
	devuiCmdFile = "/data/local/tmp/devctl/cmd"
	devuiDex     = "/data/local/tmp/devctl/devui.dex"
)

func init() {
	methods["devui_start"] = mDevuiStart
	methods["devui_stop"] = mDevuiStop
	methods["devui_status"] = mDevuiStatus
}

func mDevuiStart(c *conn, m Msg) {
	if _, err := os.Stat(devuiDex); err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false),
			Stdout: "devui.dex 缺失: " + err.Error() + " (先 client devui push)"})
		return
	}
	// 已在跑则提示
	if pid, err := readPidFile(); err == nil && pidAlive(pid) {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: fmt.Sprintf("devui 已在运行 pid=%d", pid)})
		return
	}
	// 清理旧 pid 文件, 拉起
	os.Remove(devuiPidFile)
	cmd := fmt.Sprintf("rm -f %s; setsid nohup app_process -Djava.class.path=%s /system/bin DevctlOverlay > /data/local/devctl/devui.log 2>&1 < /dev/null & echo $!",
		devuiPidFile, devuiDex)
	_, out, _ := runCmd("sh", "-c", cmd)
	pidStr := strings.TrimSpace(out)
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "启动失败: " + out})
		return
	}
	writePidFile(pid)
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true),
		Stdout: fmt.Sprintf("devui 已启动 pid=%d (日志 /data/local/devctl/devui.log)", pid)})
}

func mDevuiStop(c *conn, m Msg) {
	pid, err := readPidFile()
	if err != nil || !pidAlive(pid) {
		os.Remove(devuiPidFile)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "devui 未在运行"})
		return
	}
	// 1) 优雅: CmdFile quit (应用层 ShutdownHook 清理全部层后退出)
	writeFile(devuiCmdFile, "quit\n")
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			os.Remove(devuiPidFile)
			c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "devui 已优雅退出 (层已清理)"})
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	// 2) 超时: SIGTERM (ShutdownHook 仍会执行清理)
	runCmd("kill", "-TERM", strconv.Itoa(pid))
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !pidAlive(pid) {
			os.Remove(devuiPidFile)
			c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "devui 已退出 (SIGTERM+Hook 清理)"})
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "devui 未响应, 需人工处理 pid=" + strconv.Itoa(pid)})
}

func mDevuiStatus(c *conn, m Msg) {
	pid, err := readPidFile()
	if err != nil || !pidAlive(pid) {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "devui: 未运行"})
		return
	}
	// 进程 comm 确认 (伪装后应为 netd)
	comm := ""
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err == nil {
		comm = strings.TrimSpace(string(b))
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true),
		Stdout: fmt.Sprintf("devui: 运行中 pid=%d comm=%s (日志 /data/local/devctl/devui.log)", pid, comm)})
}

// ---- 工具 ----

func readPidFile() (int, error) {
	b, err := os.ReadFile(devuiPidFile)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

func writePidFile(pid int) {
	_ = os.WriteFile(devuiPidFile, []byte(strconv.Itoa(pid)+"\n"), 0644)
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}

func writeFile(path, content string) {
	_ = os.WriteFile(path, []byte(content), 0644)
}
