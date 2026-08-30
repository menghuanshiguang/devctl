//go:build android

package main

// ui.go: 渲染层 UI 注入 (纯内存操作)
//
// 原理: 注入 surfaceflinger 进程 → hook eglSwapBuffers 入口 → 在每帧 swap 前
// 用 GLES 调用绘制 UI (glEnable/glScissor/glClearColor/glClear), 然后放行原流程。
// 效果: UI 直接合成在屏幕输出层, 游戏/普通 app 完全无感 (不在 window/layer 列表)。
// 无新文件、无新进程、无 Java; 崩坏可由 mUiHide 恢复原指令。
//
// 接口: ui_show / ui_hide / ui_status

import (
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"sync"
)

const (
	GL_SCISSOR_TEST   = 0x0C00
	GL_COLOR_BUFFER   = 0x4000
	uiFrameW          = 840
	uiFrameH          = 180
)

var uiState = struct {
	sync.Mutex
	active         bool
	pid            int
	eglSwapAddr    uintptr
	origBytes      []byte // eglSwapBuffers 原始 12 字节
	codeBase       uintptr
	hookFuncAddr   uintptr
}{}

func init() {
	methods["ui_show"] = mUiShow
	methods["ui_hide"] = mUiHide
	methods["ui_status"] = mUiStatus
}

// ---------- 汇编生成 ----------

// encFmovSW: FMOV Sd, Wn (GPR 位复制到单精度寄存器)
func encFmovSW(d, n uint32) uint32 { return 0x1E270000 | (n&0x1F)<<5 | d&0x1F }

// buildUiHookFunc: 每帧绘制 UI 的 hook 函数
// 布局: [hook 函数 ~1KB] [trampoline] [数据区]
// trampAddr: 原函数+12 的地址 (trampoline), glEnable/glDisable/glScissor/
// glClearColor/glClear: libGLESv2_adreno.so 符号地址; w,h: 屏幕尺寸;
// color: 0xAABBGGRR? 用 (r,g,b,a) 四个 float 位模式
func buildUiHookFunc(trampAddr uint64, glEnableAddr, glDisableAddr, glScissorAddr, glClearColorAddr, glClearAddr uint64,
	w, h uint32, colR, colG, colB, colA uint32) []byte {
	var code []uint32
	// 保存寄存器 (X0=display X1=surface)
	code = append(code, encStpX29())                 // STP X29,X30,[SP,#-16]!
	code = append(code, encStpPre(19, 20))           // STP X19,X20,[SP,#-16]!
	code = append(code, encMovReg(19, 0))            // X19 = display
	code = append(code, encMovReg(20, 1))            // X20 = surface

	// glEnable(GL_SCISSOR_TEST)
	code = append(code, encMovX(0, GL_SCISSOR_TEST, 0))
	code = append(code, ldrX16Imm64(glEnableAddr)...)
	code = append(code, encBlrX16())

	// glScissor(0, h-uiFrameH, uiFrameW, uiFrameH)  (顶部横条)
	code = append(code, encMovX(0, 0, 0))
	code = append(code, encMovX(1, uint64(h)-uiFrameH, 0))
	code = append(code, encMovX(2, uiFrameW, 0))
	code = append(code, encMovX(3, uiFrameH, 0))
	code = append(code, ldrX16Imm64(glScissorAddr)...)
	code = append(code, encBlrX16())

	// glClearColor(r,g,b,a): 位模式经 W8 → FMOV S0-S3
	code = append(code, encMovX(8, uint64(colR)&0xFFFF, 0))
	code = append(code, encMovkX(8, (uint64(colR)>>16)&0xFFFF, 16))
	code = append(code, encFmovSW(0, 8))
	code = append(code, encMovX(8, uint64(colG)&0xFFFF, 0))
	code = append(code, encMovkX(8, (uint64(colG)>>16)&0xFFFF, 16))
	code = append(code, encFmovSW(1, 8))
	code = append(code, encMovX(8, uint64(colB)&0xFFFF, 0))
	code = append(code, encMovkX(8, (uint64(colB)>>16)&0xFFFF, 16))
	code = append(code, encFmovSW(2, 8))
	code = append(code, encMovX(8, uint64(colA)&0xFFFF, 0))
	code = append(code, encMovkX(8, (uint64(colA)>>16)&0xFFFF, 16))
	code = append(code, encFmovSW(3, 8))
	code = append(code, ldrX16Imm64(glClearColorAddr)...)
	code = append(code, encBlrX16())

	// glClear(GL_COLOR_BUFFER_BIT)
	code = append(code, encMovX(0, GL_COLOR_BUFFER, 0))
	code = append(code, ldrX16Imm64(glClearAddr)...)
	code = append(code, encBlrX16())

	// glDisable(GL_SCISSOR_TEST)
	code = append(code, encMovX(0, GL_SCISSOR_TEST, 0))
	code = append(code, ldrX16Imm64(glDisableAddr)...)
	code = append(code, encBlrX16())

	// 恢复原参数, 跳 trampoline (执行原 eglSwapBuffers)
	code = append(code, encMovReg(0, 19))
	code = append(code, encMovReg(1, 20))
	code = append(code, ldrX16Imm64(trampAddr)...)
	code = append(code, encBlrX16())

	// 恢复寄存器返回
	code = append(code, encLdpPost(19, 20))
	code = append(code, encLdpPost(29, 30))
	code = append(code, encRet())

	var out []byte
	for _, ins := range code {
		w := make([]byte, 4)
		binary.LittleEndian.PutUint32(w, ins)
		out = append(out, w...)
	}
	return out
}

// ---------- 注入流程 ----------

// mUiShow: 注入 surfaceflinger, hook eglSwapBuffers 画 UI
func mUiShow(c *conn, m Msg) {
	uiState.Lock()
	if uiState.active {
		uiState.Unlock()
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "UI 注入已在运行"})
		return
	}
	uiState.Unlock()

	pid, err := sfPid()
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "找 surfaceflinger: " + err.Error()})
		return
	}

	codeBase, hookAddr, eglSwapAddr, orig, err := uiInjectHook(pid)
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "注入失败: " + err.Error()})
		return
	}

	uiState.Lock()
	uiState.active = true
	uiState.pid = pid
	uiState.eglSwapAddr = eglSwapAddr
	uiState.origBytes = orig
	uiState.codeBase = uintptr(codeBase)
	uiState.hookFuncAddr = hookAddr
	uiState.Unlock()

	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true),
		Stdout: fmt.Sprintf("UI 注入成功: sf pid=%d hook@%x (每帧绘制 %dx%d, 位置顶部)", pid, hookAddr, uiFrameW, uiFrameH)})
}

// mUiHide: 恢复 eglSwapBuffers 原指令 (内存级回滚)
func mUiHide(c *conn, m Msg) {
	uiState.Lock()
	if !uiState.active {
		uiState.Unlock()
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "UI 注入未运行"})
		return
	}
	pid := uiState.pid
	eglSwapAddr := uiState.eglSwapAddr
	orig := uiState.origBytes
	uiState.active = false
	uiState.Unlock()

	if err := restoreEglSwap(pid, eglSwapAddr, orig); err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "恢复失败: " + err.Error() + " (建议重启设备)"})
		return
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "UI 注入已移除, eglSwapBuffers 已恢复原样"})
}

func mUiStatus(c *conn, m Msg) {
	uiState.Lock()
	active := uiState.active
	pid := uiState.pid
	hookAddr := uiState.hookFuncAddr
	uiState.Unlock()
	st := "关闭"
	if active {
		st = fmt.Sprintf("运行中 (sf pid=%d hook@%#x)", pid, hookAddr)
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "渲染层 UI: " + st})
}

// sfPid: 查找 surfaceflinger 进程 pid
func sfPid() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 {
			continue
		}
		pids = append(pids, pid)
	}
	// 优先按 exe 匹配
	for _, pid := range pids {
		exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
		if err != nil {
			continue
		}
		if strings.HasSuffix(exe, "surfaceflinger") {
			return pid, nil
		}
	}
	// 兜底: comm 匹配
	for _, pid := range pids {
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err == nil && strings.TrimSpace(string(comm)) == "surfaceflinger" {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("surfaceflinger 未找到")
}

// uiInjectHook: 注入核心 (流程与 injectHook 相同, 绘制版)
func uiInjectHook(pid int) (uint64, uintptr, uintptr, []byte, error) {
	libcPath := "/apex/com.android.runtime/lib64/bionic/libc.so"
	eglPath := "/vendor/lib64/egl/libEGL_adreno.so"
	glesPath := "/vendor/lib64/egl/libGLESv2_adreno.so"

	svcAddrs, err := findSvcAddrs(pid, libcPath, 10)
	if err != nil {
		return 0, 0, 0, nil, err
	}
	svcAddr := svcAddrs[0]
	for _, sa := range svcAddrs {
		if gpid, err := injectSyscall(pid, sa, 172, 0, 0, 0, 0, 0, 0); err == nil && gpid == uint64(pid) {
			svcAddr = sa
			break
		}
	}
	if svcAddr == 0 {
		return 0, 0, 0, nil, fmt.Errorf("所有 svc 位置 getpid 均失败")
	}

	eglBase, err := libBaseOf(pid, "libEGL_adreno.so")
	if err != nil {
		return 0, 0, 0, nil, err
	}
	glesBase, err := libBaseOf(pid, "libGLESv2_adreno.so")
	if err != nil {
		return 0, 0, 0, nil, err
	}

	eglSwapAddr, err := elfSymbolAddr(eglPath, eglBase, "eglSwapBuffers")
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("eglSwapBuffers: %v", err)
	}
	glEnableAddr, err := elfSymbolAddr(glesPath, glesBase, "glEnable")
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("glEnable: %v", err)
	}
	glDisableAddr, err := elfSymbolAddr(glesPath, glesBase, "glDisable")
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("glDisable: %v", err)
	}
	glScissorAddr, err := elfSymbolAddr(glesPath, glesBase, "glScissor")
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("glScissor: %v", err)
	}
	glClearColorAddr, err := elfSymbolAddr(glesPath, glesBase, "glClearColor")
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("glClearColor: %v", err)
	}
	glClearAddr, err := elfSymbolAddr(glesPath, glesBase, "glClear")
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("glClear: %v", err)
	}

	if err := syscall.PtraceAttach(pid); err != nil {
		return 0, 0, 0, nil, fmt.Errorf("attach: %v", err)
	}
	defer syscall.PtraceDetach(pid)
	var ws syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
		return 0, 0, 0, nil, err
	}

	w, h := uint32(1216), uint32(2640)
	hookSize := uint64(4096)

	// mmap 代码区 RW
	codeBase, err := injectSyscall(pid, svcAddr, mmapSyscall, 0, hookSize+4096, 6, mapAnon, ^uint64(0), 0)
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("mmap code: %v", err)
	}

	orig, err := peekBytes(pid, eglSwapAddr, 12)
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("peek orig: %v", err)
	}
	hookFuncAddr := uintptr(codeBase)
	trampAddr := codeBase + 2048

	tramp := buildTrampoline(orig, uint64(eglSwapAddr)+12)

	// 紫色 UI (RGBA 位模式: 0.5,0.2,0.8,0.6)
	colR := uint32(0x3F000000) // 0.5
	colG := uint32(0x3E4CCCCD) // 0.2
	colB := uint32(0x3F4CCCCD) // 0.8
	colA := uint32(0x3F19999A) // 0.6
	hook := buildUiHookFunc(trampAddr, uint64(glEnableAddr), uint64(glDisableAddr), uint64(glScissorAddr), uint64(glClearColorAddr), uint64(glClearAddr),
		w, h, colR, colG, colB, colA)

	if err := pokeBytes(pid, hookFuncAddr, hook); err != nil {
		return 0, 0, 0, nil, fmt.Errorf("write hook: %v", err)
	}
	if err := pokeBytes(pid, uintptr(trampAddr), tramp); err != nil {
		return 0, 0, 0, nil, fmt.Errorf("write tramp: %v", err)
	}
	if _, err := injectSyscall(pid, svcAddr, mprotectSyscall, codeBase, hookSize+4096, protRX, 0, 0, 0); err != nil {
		return 0, 0, 0, nil, fmt.Errorf("mprotect rx: %v", err)
	}

	// patch eglSwapBuffers 入口
	pageStart := uintptr(eglSwapAddr &^ 0xFFF)
	if _, err := injectSyscall(pid, svcAddr, mprotectSyscall, uint64(pageStart), 0x4000, 6, 0, 0, 0); err != nil {
		return 0, 0, 0, nil, fmt.Errorf("mprotect rw: %v", err)
	}
	jump := buildJumpPatch(uint64(hookFuncAddr))
	if err := pokeBytes(pid, uintptr(eglSwapAddr), jump); err != nil {
		return 0, 0, 0, nil, fmt.Errorf("patch jump: %v", err)
	}
	if _, err := injectSyscall(pid, svcAddr, mprotectSyscall, uint64(pageStart), 0x4000, protRX, 0, 0, 0); err != nil {
		return 0, 0, 0, nil, fmt.Errorf("mprotect rx: %v", err)
	}

	return codeBase, hookFuncAddr, uintptr(eglSwapAddr), orig, nil
}

// restoreEglSwap: 恢复 eglSwapBuffers 原指令
func restoreEglSwap(pid int, addr uintptr, orig []byte) error {
	svcAddrs, err := findSvcAddrs(pid, "/apex/com.android.runtime/lib64/bionic/libc.so", 10)
	if err != nil {
		return err
	}
	svcAddr := svcAddrs[0]
	for _, sa := range svcAddrs {
		if gpid, err := injectSyscall(pid, sa, 172, 0, 0, 0, 0, 0, 0); err == nil && gpid == uint64(pid) {
			svcAddr = sa
			break
		}
	}
	if err := syscall.PtraceAttach(pid); err != nil {
		return err
	}
	defer syscall.PtraceDetach(pid)
	var ws syscall.WaitStatus
	syscall.Wait4(pid, &ws, 0, nil)

	pageStart := uintptr(addr &^ 0xFFF)
	if _, err := injectSyscall(pid, svcAddr, mprotectSyscall, uint64(pageStart), 0x4000, 6, 0, 0, 0); err != nil {
		return err
	}
	if err := pokeBytes(pid, addr, orig); err != nil {
		return err
	}
	if _, err := injectSyscall(pid, svcAddr, mprotectSyscall, uint64(pageStart), 0x4000, protRX, 0, 0, 0); err != nil {
		return err
	}
	return nil
}
