//go:build android

package main

// 进程自身轻量隐藏 (用户态 v2)
//
// 设计原则 (按要求重构):
//   1. 绝不 ptrace / hook / attach 任何第三方进程 (游戏, 应用, 系统进程一律不碰)
//   2. 只操作自身进程: comm/cmdline 伪装 = 内存级操作, 不修改任何系统文件
//   3. 伪装名从 kworker 改为系统守护进程风格 (默认 netd), 消除"用户态 kworker"这种
//      一眼假的特征
//   4. 完整"系统 API 检测不到"超出用户态能力边界 (exe/maps/Secctx/端口 inode 由
//      内核供给), 需要内核层配合 (susfs/KernelSU 或 LKM), 见 hide_notes.md
//
// 接口: hide_start / hide_stop / hide_status (与 client 兼容)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

const (
	// TASK_COMM_LEN = 16 (含结尾 NUL), comm 最长 15 字节
	commMax = 15
)

var (
	disguiseName = "netd"        // comm 名
	disguiseCmd  = "/system/bin/netd" // cmdline 伪装
)

var hideState = struct {
	sync.Mutex
	running    bool
	myPid      int
	origName   string
	oldCmdline []byte
}{}

func init() {
	methods["hide_start"] = mHideStart
	methods["hide_stop"] = mHideStop
	methods["hide_status"] = mHideStatus
}

// autoHide: 启动时自动伪装 (默认开启, -no-hide 可禁用)
// panic 防护: 伪装失败绝不拖垮 agent 主流程
func autoHide() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "hide: panic recovered:", r)
		}
	}()
	startDisguise()
}

// SetDisguise 由 main 注入自定义伪装名 (--disguise-name)
func SetDisguise(name, cmd string) {
	disguiseName = name
	disguiseCmd = cmd
}

func startDisguise() {
	myPid := os.Getpid()

	origComm, _ := os.ReadFile("/proc/self/comm")
	oldCmdline, _ := os.ReadFile("/proc/self/cmdline")
	hideState.origName = strings.TrimRight(string(origComm), "\n")
	hideState.oldCmdline = oldCmdline

	setComm(disguiseName)
	overwriteCmdline(disguiseCmd)

	hideState.myPid = myPid
	hideState.running = true
}

func mHideStart(c *conn, m Msg) {
	hideState.Lock()
	if hideState.running {
		hideState.Unlock()
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "已在伪装模式: " + disguiseName})
		return
	}
	startDisguise()
	hideState.Unlock()

	data, _ := json.Marshal(map[string]any{"pid": hideState.myPid, "name": disguiseName})
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: string(data),
		Stdout: fmt.Sprintf("伪装已启动: pid %d, comm=%s cmdline=%s (仅自身进程, 无第三方接触)",
			hideState.myPid, disguiseName, disguiseCmd)})
}

func mHideStop(c *conn, m Msg) {
	hideState.Lock()
	if !hideState.running {
		hideState.Unlock()
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "未在伪装模式"})
		return
	}
	origName := hideState.origName
	oldCmdline := hideState.oldCmdline
	hideState.running = false
	hideState.Unlock()

	// 恢复自身进程 comm + cmdline (内存级, 无需重启)
	if origName != "" {
		_ = os.WriteFile("/proc/self/comm", []byte(origName)[:commMax], 0)
	}
	if len(oldCmdline) > 0 {
		restoreCmdline(oldCmdline)
	}

	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "伪装已停止, 进程名已恢复"})
}

func mHideStatus(c *conn, m Msg) {
	hideState.Lock()
	running := hideState.running
	pid := hideState.myPid
	hideState.Unlock()

	status := "关闭"
	if running {
		status = fmt.Sprintf("开启 (pid %d, comm=%s)", pid, disguiseName)
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true),
		Stdout: "伪装模式: " + status})
}

// ---------- comm / cmdline 伪装 (仅自身进程) ----------

func setComm(name string) {
	b := []byte(name)
	if len(b) > commMax {
		b = b[:commMax]
	}
	// 写 /proc/self/comm = 修改自身进程的内核 task 字段, 非文件系统写入
	if err := os.WriteFile("/proc/self/comm", b, 0); err != nil {
		fmt.Fprintln(os.Stderr, "setComm:", err)
	}
}

// overwriteCmdline: 在自身进程栈内存中原地覆盖 argv[0] 字符串
// (读 /proc/self/maps 定位 stack, 读内存找 cmdline 原文, 写回新名)
func overwriteCmdline(newName string) {
	cmdline, err := os.ReadFile("/proc/self/cmdline")
	if err != nil || len(cmdline) == 0 {
		return
	}
	addr, err := findCmdlineAddr(os.Getpid(), cmdline)
	if err != nil {
		return
	}
	// 等长覆盖, 余下补 0 (保留原缓冲区长度, 内存等长 = 安全)
	buf := make([]byte, len(cmdline))
	copy(buf, newName)
	if len(newName) > len(buf) {
		buf = []byte(newName[:len(buf)])
	}
	if _, err := vmWrite(os.Getpid(), addr, buf); err != nil {
		fmt.Fprintln(os.Stderr, "overwriteCmdline:", err)
	}
}

func restoreCmdline(oldCmdline []byte) {
	newCmdline, err := os.ReadFile("/proc/self/cmdline")
	if err != nil || len(newCmdline) == 0 {
		return
	}
	addr, err := findCmdlineAddr(os.Getpid(), newCmdline)
	if err != nil {
		return
	}
	// 等长恢复, 余下补 0
	buf := make([]byte, len(newCmdline))
	copy(buf, oldCmdline)
	if len(oldCmdline) > len(buf) {
		buf = oldCmdline[:len(buf)]
	}
	if _, err := vmWrite(os.Getpid(), addr, buf); err != nil {
		fmt.Fprintln(os.Stderr, "restoreCmdline:", err)
	}
}

func findCmdlineAddr(pid int, needle []byte) (uintptr, error) {
	maps, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return 0, err
	}
	var stackLo, stackHi uintptr
	for _, line := range strings.Split(string(maps), "\n") {
		if strings.Contains(line, "[stack]") {
			var lo, hi uint64
			if _, err := fmt.Sscanf(line, "%x-%x", &lo, &hi); err == nil {
				stackLo, stackHi = uintptr(lo), uintptr(hi)
			}
			break
		}
	}
	if stackLo == 0 {
		return 0, fmt.Errorf("no stack mapping")
	}
	for addr := stackLo; addr < stackHi; addr += 4096 {
		n := int(stackHi - addr)
		if n > 4096 {
			n = 4096
		}
		buf := make([]byte, n)
		got, err := vmRead(pid, addr, buf)
		if err != nil || got == 0 {
			continue
		}
		if idx := bytes.Index(buf[:got], needle); idx >= 0 {
			return addr + uintptr(idx), nil
		}
	}
	return 0, fmt.Errorf("cmdline not found on stack")
}

// ensureHideState 提供给 ops 审计: 当前伪装状态摘要
func hideSummary() string {
	hideState.Lock()
	defer hideState.Unlock()
	if !hideState.running {
		return "hide: off"
	}
	return fmt.Sprintf("hide: on (pid=%d comm=%s)", hideState.myPid, disguiseName)
}
