//go:build android

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// hook: ptrace 系统调用级过滤 (仅影响目标进程, 到期自动 detach, 不改任何系统配置)
// 用法: hook <pid|pkg> <秒数> <deny关键词...>
// 拦截 openat/newfstatat/faccessat/stat/statx 中路径含关键词的调用, 返回 ENOENT

const (
	sysOpenat    = 56
	sysStat      = 4
	sysFaccessat = 48
	sysNewfstat  = 79
	sysStatx     = 291
)

func readCStr(pid int, addr uintptr, max int) string {
	buf := make([]byte, max)
	n, err := vmRead(pid, addr, buf)
	if err != nil || n <= 0 {
		return ""
	}
	if i := bytes.IndexByte(buf[:n], 0); i >= 0 {
		n = i
	}
	return string(buf[:n])
}

// hook 主循环: 拦截目标进程的文件检测
func runHook(pid int, seconds int, denies []string) (int, error) {
	if err := syscall.PtraceAttach(pid); err != nil {
		return 0, fmt.Errorf("attach 失败: %v", err)
	}
	defer syscall.PtraceDetach(pid)
	var ws syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
		return 0, fmt.Errorf("wait 失败: %v", err)
	}

	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	entry := true // true=syscall 入口停点, false=出口
	var lastNo int
	var lastPath string
	blocked := 0

	for time.Now().Before(deadline) {
		if err := syscall.PtraceSyscall(pid, 0); err != nil {
			break
		}
		if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
			break
		}
		if ws.Exited() || ws.Signaled() {
			break
		}
		var regs syscall.PtraceRegs
		if err := syscall.PtraceGetRegs(pid, &regs); err != nil {
			break
		}
		if entry {
			// 入口: x8 = syscall 号
			lastNo = int(regs.Regs[8])
			lastPath = ""
			switch lastNo {
			case sysOpenat, sysNewfstat, sysFaccessat:
				lastPath = readCStr(pid, uintptr(regs.Regs[1]), 256)
			case sysStat, sysStatx:
				lastPath = readCStr(pid, uintptr(regs.Regs[0]), 256)
			}
		} else {
			// 出口: 修改返回值
			if lastPath != "" {
				for _, d := range denies {
					if strings.Contains(lastPath, d) {
						regs.Regs[0] = ^uint64(2 - 1) // -ENOENT
						if err := syscall.PtraceSetRegs(pid, &regs); err == nil {
							blocked++
						}
						break
					}
				}
			}
		}
		entry = !entry
	}
	return blocked, nil
}

// hook <pid|pkg> <秒数> <deny关键词...>
func mHook(c *conn, m Msg) {
	if len(m.Args) < 3 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: hook <pid> <秒数> <deny关键词...>"})
		return
	}
	pid, err := resolvePid(m.Args[0])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	secs, err := strconv.Atoi(m.Args[1])
	if err != nil || secs <= 0 || secs > 300 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "秒数需在 1-300"})
		return
	}
	denies := m.Args[2:]
	blocked, err := runHook(pid, secs, denies)
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	data, _ := json.Marshal(map[string]any{"pid": pid, "secs": secs, "blocked": blocked, "denies": denies})
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: string(data),
		Stdout: fmt.Sprintf("hook 结束: 拦截 %d 次文件检测 (关键词: %s)", blocked, strings.Join(denies, ","))})
}
