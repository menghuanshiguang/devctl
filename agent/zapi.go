//go:build android

package main

// zapi.go: 挂载注入器 — 把 devctl 的 zapi 载荷注入到目标进程
// (默认 SystemUI), 在宿主内内存加载 devui.dex (零落盘, go:embed 内嵌),
// 通过 JNI 在宿主内动态调用 Java API。
//
// 原理: ptrace 注入 → 远程 mmap (数据+代码) → 三段远程函数调用:
//   dlopen(soPath, RTLD_NOW) → dlsym(handle, "devui_entry") → devui_entry(dexAddr, dexSize)
// 载荷在宿主内 (pthread) 引导: InMemoryDexClassLoader → DevBridge.bootstrap。
//
// 接口: zapi_attach [pid]      pid 缺省自动找 com.android.systemui

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

//go:embed assets/devui.dex
var embeddedDex []byte

func init() {
	methods["zapi_attach"] = mZapiAttach
	methods["zapi_status"] = mZapiStatus
}

const (
	rtldNow   = 2
	libdlPath = "/system/lib64/libdl.so"
)

// mZapiAttach: 注入 zapi 载荷到目标进程
func mZapiAttach(c *conn, m Msg) {
	soPath := "/data/local/tmp/devctl/libdevui.so"
	if _, err := os.Stat(soPath); err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "载荷缺失: " + soPath + " (先 client zapi push)"})
		return
	}
	pid, err := resolveTarget(m.Args)
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: err.Error()})
		return
	}
	soPathC := soPath + "\x00"
	fnName := "devui_entry\x00"

	// ---- 1. attach ----
	if err := syscall.PtraceAttach(pid); err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "attach: " + err.Error() + " (内核可能硬封)"})
		return
	}
	var ws syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "wait: " + err.Error()})
		return
	}

	// 保存原寄存器 (每个线程? 只处理主线程: attach 停的是主线程)
	regs, err := ptraceGetRegs(pid)
	if err != nil {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "getregs: " + err.Error()})
		return
	}

	// ---- 2. 找 svc (远程 mmap 用) ----
	svcAddrs, err := findSvcAddrs(pid, "/apex/com.android.runtime/lib64/bionic/libc.so", 10)
	if err != nil {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "svc: " + err.Error()})
		return
	}
	svcAddr := svcAddrs[0]
	for _, sa := range svcAddrs {
		if gpid, err := injectSyscall(pid, sa, 172, 0, 0, 0, 0, 0, 0); err == nil && gpid == uint64(pid) {
			svcAddr = sa
			break
		}
	}

	// ---- 3. 远程 mmap 数据区 + 代码区 ----
	// 数据布局: [0..] soPath [256..] fnName [512..] dex 字节流 (内嵌, 零落盘)
	dataBase, err := injectSyscall(pid, svcAddr, mmapSyscall, 0, 4096+uint64(len(embeddedDex))+32, 7, mapAnon, ^uint64(0), 0)
	if err != nil || dataBase == ^uint64(0) {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "mmap data: " + errStr(err)})
		return
	}
	codeBase, err := injectSyscall(pid, svcAddr, mmapSyscall, 0, 4096, 7, mapAnon, ^uint64(0), 0)
	if err != nil || codeBase == ^uint64(0) {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "mmap code: " + errStr(err)})
		return
	}

	// 写入 soPath / fnName / dex 字节流
	pokeBytes(pid, uintptr(dataBase), []byte(soPathC))
	pokeBytes(pid, uintptr(dataBase+256), []byte(fnName))
	pokeBytes(pid, uintptr(dataBase+512), embeddedDex)
	// 结果槽放在 dex 流之后 (对齐 8)
	resultOff := uint64(512 + len(embeddedDex) + 8)
	_ = resultOff

	// ---- 4. 远程函数地址 ----
	dlBase, err := libBaseOf(pid, "libdl.so")
	if err != nil {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "libdl: " + err.Error()})
		return
	}
	dlopenAddr, err := elfSymbolAddr(libdlPath, dlBase, "dlopen")
	if err != nil {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "dlopen sym: " + err.Error()})
		return
	}
	dlsymAddr, err := elfSymbolAddr(libdlPath, dlBase, "dlsym")
	if err != nil {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "dlsym sym: " + err.Error()})
		return
	}

	// ---- 5. 三段远程调用 ----
	// 调用1: dlopen(soPath, 0, RTLD_NOW)
	handle, err := remoteCall(pid, codeBase, dataBase, uint64(dlopenAddr),
		[]uint64{dataBase, 0, rtldNow}, resultOff)
	if err != nil {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "dlopen: " + err.Error()})
		return
	}
	if handle == 0 {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "dlopen returned NULL (so 加载失败?)"})
		return
	}
	// 调用2: dlsym(handle, "devui_entry")
	fnAddr, err := remoteCall(pid, codeBase, dataBase, uint64(dlsymAddr),
		[]uint64{handle, dataBase + 256}, resultOff+8)
	if err != nil {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "dlsym: " + err.Error()})
		return
	}
	if fnAddr == 0 {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "dlsym returned NULL"})
		return
	}
	// 调用3: devui_entry(dexAddr, dexSize) — dex 内嵌字节流直传宿主内存
	_, err = remoteCall(pid, codeBase, dataBase, fnAddr,
		[]uint64{dataBase + 512, uint64(len(embeddedDex))}, 0)
	if err != nil {
		syscall.PtraceDetach(pid)
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stdout: "entry: " + err.Error()})
		return
	}

	// ---- 6. 恢复寄存器 + detach ----
	ptraceSetRegs(pid, regs)
	syscall.PtraceCont(pid, 0)
	ptraceDetachQuiet(pid)

	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true),
		Stdout: fmt.Sprintf("zapi 注入成功: pid=%d so=%s dex=%dBytes handle=%#x entry=%#x", pid, soPath, len(embeddedDex), handle, fnAddr)})
}

// remoteCall: 把目标线程 PC 改为 stub, 执行一次远程函数调用后恢复
// 参数: pid, codeBase(代码区), dataBase(数据区), fnAddr(目标函数), args(x0..x5), resultSlot(数据区偏移)
func remoteCall(pid int, codeBase, dataBase uint64, fnAddr uint64, args []uint64, resultSlot uint64) (uint64, error) {
	// stub: ldr x16, [pc, #16]; blr x16; str x0, [x15]; brk #0; .word fn lo, fn hi
	stub := make([]byte, 24)
	// ldr x16, [pc, #16]  => PC+16 处两个 word
	put32(stub, 0, 0x58000050)             // ldr x16, #16
	put32(stub, 4, 0xD63F0200)             // blr x16
	put32(stub, 8, 0xF80001F0)             // str x0, [x15]
	put32(stub, 12, 0xD4200000)            // brk #0
	binary.LittleEndian.PutUint32(stub[16:], uint32(fnAddr))
	binary.LittleEndian.PutUint32(stub[20:], uint32(fnAddr>>32))
	if err := pokeBytes(pid, uintptr(codeBase), stub); err != nil {
		return 0, err
	}

	regs, err := ptraceGetRegs(pid)
	if err != nil {
		return 0, err
	}
	orig := *regs
	// 设置: PC=stub, X15=resultSlot 指针, 参数
	regs.Pc = uint64(codeBase)
	regs.Regs[15] = dataBase + resultSlot
	for i, a := range args {
		if i < 8 {
			regs.Regs[i] = a
		}
	}
	if err := ptraceSetRegs(pid, regs); err != nil {
		return 0, err
	}
	if err := syscall.PtraceCont(pid, 0); err != nil {
		return 0, err
	}
	// 等 SIGTRAP (brk)
	for {
		var ws2 syscall.WaitStatus
		if _, err := syscall.Wait4(pid, &ws2, 0, nil); err != nil {
			return 0, err
		}
		if ws2.StopSignal() == syscall.SIGTRAP || ws2.StopSignal() == syscall.SIGSTOP {
			break
		}
	}
	// 读结果
	var res uint64
	if resultSlot != 0 {
		b, err := peekBytes(pid, uintptr(dataBase+resultSlot), 8)
		if err == nil {
			res = binary.LittleEndian.Uint64(b)
		}
	}
	// 恢复原寄存器
	ptraceSetRegs(pid, &orig)
	return res, nil
}

// resolveTarget: 解析注入目标 pid
func resolveTarget(args []string) (int, error) {
	if len(args) > 0 {
		p, err := strconv.Atoi(args[0])
		if err != nil || p <= 0 {
			return 0, fmt.Errorf("非法 pid: %v", args[0])
		}
		return p, nil
	}
	// 自动找 com.android.systemui
	_, out, _ := runCmd("pidof", "com.android.systemui")
	for _, line := range strings.Fields(out) {
		if p, err := strconv.Atoi(line); err == nil && p > 0 {
			return p, nil
		}
	}
	return 0, fmt.Errorf("未找到 SystemUI, 请指定 pid")
}

func mZapiStatus(c *conn, m Msg) {
	_, out, _ := runCmd("pidof", "com.android.systemui")
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "SystemUI pid: " + strings.TrimSpace(out) + " (载荷运行状态请查 127.0.0.1:8288)"})
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func put32(b []byte, off int, v uint32) {
	binary.LittleEndian.PutUint32(b[off:], v)
}

func ptraceDetachQuiet(pid int) {
	_ = syscall.PtraceDetach(pid)
}

func ptraceGetRegs(pid int) (*syscall.PtraceRegs, error) {
	var r syscall.PtraceRegs
	err := syscall.PtraceGetRegs(pid, &r)
	return &r, err
}

func ptraceSetRegs(pid int, r *syscall.PtraceRegs) error {
	return syscall.PtraceSetRegs(pid, r)
}
