package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// ptrace 注入器: 注入 hook 代码到 SurfaceFlinger, hook eglSwapBuffers 截帧

const (
	mmapSyscall    = 222 // arm64
	mprotectSyscall = 226
	protRWX        = 7
	protRX         = 5
	mapAnon        = 0x22 // MAP_PRIVATE|MAP_ANONYMOUS
)

// findSvcAddrs: 返回 libc 可执行段内所有 svc 位置 (最多 N 个)
func findSvcAddrs(pid int, libcPath string, maxN int) ([]uintptr, error) {
	data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	type seg struct {
		start, size, fileOff uint64
	}
	var segs []seg
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "libc.so") || !strings.Contains(line, "r-xp") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		var start, end, fileOff uint64
		fmt.Sscanf(fields[0], "%x-%x", &start, &end)
		fmt.Sscanf(fields[2], "%x", &fileOff)
		segs = append(segs, seg{start, end - start, fileOff})
	}
	file, err := os.ReadFile(libcPath)
	if err != nil {
		return nil, err
	}
	var out []uintptr
	for _, s := range segs {
		start := int(s.fileOff)
		end := int(s.fileOff + s.size)
		if start >= len(file) {
			continue
		}
		if end > len(file) {
			end = len(file)
		}
		for i := start; i+4 <= end && len(out) < maxN; i++ {
			if file[i] == 0x01 && file[i+1] == 0x00 && file[i+2] == 0x00 && file[i+3] == 0xD4 {
				out = append(out, uintptr(s.start+uint64(i)-s.fileOff))
			}
		}
		if len(out) >= maxN {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("libc 可执行段中未找到 svc 位置")
	}
	return out, nil
}

// injectSyscall: 在目标进程中执行一个 syscall (断点法: BRK 在 svc+4, CONT 执行)
func injectSyscall(pid int, svcAddr uintptr, x8, x0, x1, x2, x3, x4, x5 uint64) (uint64, error) {
	var ws syscall.WaitStatus
	// 1. 在 svc+4 设断点 (BRK #0 = 0xD4200000)
	svcNext := svcAddr + 4
	origNext, err := peekBytes(pid, svcNext, 4)
	if err != nil {
		return 0, err
	}
	brk := make([]byte, 4)
	brk[0], brk[1], brk[2], brk[3] = 0x00, 0x00, 0x20, 0xD4 // LE: D4200000
	if err := pokeBytes(pid, svcNext, brk); err != nil {
		return 0, err
	}
	// 2. 设置注入寄存器
	var regs syscall.PtraceRegs
	if err := syscall.PtraceGetRegs(pid, &regs); err != nil {
		return 0, err
	}
	saved := regs
	regs.Regs[8] = x8
	regs.Regs[0] = x0
	regs.Regs[1] = x1
	regs.Regs[2] = x2
	regs.Regs[3] = x3
	regs.Regs[4] = x4
	regs.Regs[5] = x5
	regs.Pc = uint64(svcAddr)
	if err := syscall.PtraceSetRegs(pid, &regs); err != nil {
		return 0, err
	}
	// 3. 继续执行: svc → syscall → svc+4 断点停
	if err := syscall.PtraceCont(pid, 0); err != nil {
		return 0, err
	}
	if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
		return 0, err
	}
	if err := syscall.PtraceGetRegs(pid, &regs); err != nil {
		return 0, err
	}
	ret := regs.Regs[0]
	// 4. 恢复断点处原指令 + 原寄存器
	if err := pokeBytes(pid, svcNext, origNext); err != nil {
		return 0, err
	}
	if err := syscall.PtraceSetRegs(pid, &saved); err != nil {
		return 0, err
	}
	if err := syscall.PtraceCont(pid, 0); err != nil {
		return 0, err
	}
	if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
		return 0, err
	}
	return ret, nil
}

// pokeBytes: 用 POKEDATA 写字节到目标进程
func pokeBytes(pid int, addr uintptr, data []byte) error {
	for i := 0; i < len(data); i += 8 {
		var buf [8]byte
		copy(buf[:], data[i:])
		if _, err := syscall.PtracePokeData(pid, addr+uintptr(i), buf[:]); err != nil {
			return err
		}
	}
	return nil
}

// peekBytes: 读目标进程字节
func peekBytes(pid int, addr uintptr, n int) ([]byte, error) {
	out := make([]byte, 0, n)
	for i := 0; i < n; i += 8 {
		var buf [8]byte
		if _, err := syscall.PtracePeekData(pid, addr+uintptr(i), buf[:]); err != nil {
			return nil, err
		}
		out = append(out, buf[:min(8, n-i)]...)
	}
	return out, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// injectHook: 完整注入流程
// returns: mmapBase (hook 区), frameBufAddr, 错误
func injectHook(pid int) (uint64, uint64, error) {
	// 解析符号地址
	libcPath := "/apex/com.android.runtime/lib64/bionic/libc.so"
	eglPath := "/vendor/lib64/egl/libEGL_adreno.so"
	glesPath := "/vendor/lib64/egl/libGLESv2_adreno.so"

	svcAddrs, err := findSvcAddrs(pid, libcPath, 10)
	if err != nil {
		return 0, 0, err
	}
	// 自检: 逐个 svc 位置试 getpid, 找到能用的
	svcAddr := svcAddrs[0]
	for _, sa := range svcAddrs {
		if gpid, err := injectSyscall(pid, sa, 172, 0, 0, 0, 0, 0, 0); err == nil && gpid == uint64(pid) {
			svcAddr = sa
			break
		}
	}
	if svcAddr == 0 {
		return 0, 0, fmt.Errorf("所有 svc 位置 getpid 均失败")
	}
	eglBase, err := libBaseOf(pid, "libEGL_adreno.so")
	if err != nil {
		return 0, 0, err
	}
	glesBase, err := libBaseOf(pid, "libGLESv2_adreno.so")
	if err != nil {
		return 0, 0, err
	}
	eglSwapAddr, err := elfSymbolAddr(eglPath, eglBase, "eglSwapBuffers")
	if err != nil {
		return 0, 0, fmt.Errorf("eglSwapBuffers: %v", err)
	}
	glReadAddr, err := elfSymbolAddr(glesPath, glesBase, "glReadPixels")
	if err != nil {
		return 0, 0, fmt.Errorf("glReadPixels: %v", err)
	}

	// attach
	if err := syscall.PtraceAttach(pid); err != nil {
		return 0, 0, fmt.Errorf("attach: %v", err)
	}
	defer syscall.PtraceDetach(pid)
	var ws syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
		return 0, 0, err
	}

	w, h := uint32(1216), uint32(2640)
	frameSize := uint64(w) * uint64(h) * 4
	hookSize := uint64(4096) // hook 函数 + trampoline 空间

	// 0. 注入机制自检: getpid (arm64 = 172)
	if gpid, err := injectSyscall(pid, svcAddr, 172, 0, 0, 0, 0, 0, 0); err != nil || gpid != uint64(pid) {
		return 0, 0, fmt.Errorf("注入自检失败: getpid=%v err=%v (期望 %d)", gpid, err, pid)
	}
	// 1. mmap RW (代码区 + 帧区分开, Android W^X 禁止 RWX)
	codeBase, err := injectSyscall(pid, svcAddr, mmapSyscall, 0, hookSize+4096, 6 /*RW*/, mapAnon, ^uint64(0), 0)
	if err != nil {
		return 0, 0, fmt.Errorf("mmap code: %v", err)
	}
	if codeBase&0xFFFFFFFF00000000 == 0xFFFFFFFF00000000 {
		return 0, 0, fmt.Errorf("mmap code 失败: %#x", codeBase)
	}
	frameBase, err := injectSyscall(pid, svcAddr, mmapSyscall, 0, frameSize+4096, 6 /*RW*/, mapAnon, ^uint64(0), 0)
	if err != nil {
		return 0, 0, fmt.Errorf("mmap frame: %v", err)
	}
	if frameBase&0xFFFFFFFF00000000 == 0xFFFFFFFF00000000 {
		return 0, 0, fmt.Errorf("mmap frame 失败: %#x", frameBase)
	}

	// 2. 构造 hook 代码
	orig, err := peekBytes(pid, eglSwapAddr, 12)
	if err != nil {
		return 0, 0, fmt.Errorf("peek orig: %v", err)
	}
	hookFuncAddr := uintptr(codeBase)
	trampAddr := hookFuncAddr + 2048
	frameBufAddr := uintptr(frameBase)

	tramp := buildTrampoline(orig, uint64(eglSwapAddr)+12)
	hook := buildHookFunc(uint64(trampAddr), uint64(glReadAddr), w, h, uint64(frameBufAddr))

	// 3. 写入 hook 区 (hook 函数 + trampoline), 然后 mprotect RX
	if err := pokeBytes(pid, hookFuncAddr, hook); err != nil {
		return 0, 0, fmt.Errorf("write hook: %v", err)
	}
	if err := pokeBytes(pid, trampAddr, tramp); err != nil {
		return 0, 0, fmt.Errorf("write tramp: %v", err)
	}
	if _, err := injectSyscall(pid, svcAddr, mprotectSyscall, codeBase, hookSize+4096, protRX, 0, 0, 0); err != nil {
		return 0, 0, fmt.Errorf("mprotect code rx: %v", err)
	}

	// 4. patch eglSwapBuffers (mprotect RW → 写跳转 → 恢复 RX)
	pageStart := eglSwapAddr &^ 0xFFF
	pageSize := uint64(0x4000) // 覆盖 4 页 (eglSwapBuffers 附近)
	if _, err := injectSyscall(pid, svcAddr, mprotectSyscall, uint64(pageStart), pageSize, 6 /*RW*/, 0, 0, 0); err != nil {
		return 0, 0, fmt.Errorf("mprotect rw: %v", err)
	}
	jump := buildJumpPatch(uint64(hookFuncAddr))
	if err := pokeBytes(pid, eglSwapAddr, jump); err != nil {
		return 0, 0, fmt.Errorf("patch jump: %v", err)
	}
	if _, err := injectSyscall(pid, svcAddr, mprotectSyscall, uint64(pageStart), pageSize, protRX, 0, 0, 0); err != nil {
		return 0, 0, fmt.Errorf("mprotect rx: %v", err)
	}

	return codeBase, uint64(frameBufAddr), nil
}

// readFrame: 读取最新帧 (RGBA)
func readFrame(pid int, frameBufAddr uintptr, w, h uint32) ([]byte, error) {
	size := int(w * h * 4)
	buf := make([]byte, size)
	off := 0
	for off < size {
		n := size - off
		if n > 1<<20 {
			n = 1 << 20
		}
		got, err := vmRead(pid, frameBufAddr+uintptr(off), buf[off:off+n])
		if err != nil || got <= 0 {
			return nil, fmt.Errorf("读帧失败 @ %x: %v", frameBufAddr+uintptr(off), err)
		}
		off += got
	}
	return buf, nil
}
