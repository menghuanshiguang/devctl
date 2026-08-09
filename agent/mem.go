package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// 内存读写 (root, process_vm_readv/writev, 无需 ptrace attach)
// 搜索状态: 每个 pid 缓存候选地址 + 上次值, 支持 changed/increased/decreased 过滤

type searchCtx struct {
	addrs []uintptr
	vals  []byte // 每个地址上次读到的值 (等宽)
	width int
}

var searchState = struct {
	sync.Mutex
	m map[int]*searchCtx
}{m: map[int]*searchCtx{}}

// ---------- syscall 封装 ----------

func vmRead(pid int, addr uintptr, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	local := syscall.Iovec{Base: &buf[0], Len: uint64(len(buf))}
	remote := syscall.Iovec{Base: (*byte)(unsafe.Pointer(addr)), Len: uint64(len(buf))}
	n, _, errno := syscall.Syscall6(syscall.SYS_PROCESS_VM_READV,
		uintptr(pid), uintptr(unsafe.Pointer(&local)), 1,
		uintptr(unsafe.Pointer(&remote)), 1, 0)
	if errno != 0 {
		return int(n), errno
	}
	return int(n), nil
}

func vmWrite(pid int, addr uintptr, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	local := syscall.Iovec{Base: &buf[0], Len: uint64(len(buf))}
	remote := syscall.Iovec{Base: (*byte)(unsafe.Pointer(addr)), Len: uint64(len(buf))}
	n, _, errno := syscall.Syscall6(syscall.SYS_PROCESS_VM_WRITEV,
		uintptr(pid), uintptr(unsafe.Pointer(&local)), 1,
		uintptr(unsafe.Pointer(&remote)), 1, 0)
	if errno != 0 {
		return int(n), errno
	}
	return int(n), nil
}

// ---------- 辅助 ----------

// 内存区域 (path 为空=匿名区, 否则=文件映射)
type memRegion struct {
	start, end uintptr
	path       string
}

// 所有可读区域 (反向指针搜索用, 含文件映射段; includeExec=true 时含代码段)
func allRegions(pid int, includeExec bool) ([]memRegion, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return nil, err
	}
	var regs []memRegion
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		perms := parts[1]
		if len(perms) < 1 || perms[0] != 'r' {
			continue
		}
		if !includeExec && strings.Contains(perms, "x") {
			continue // 代码段无数据值且拖慢扫描
		}
		path := ""
		if len(parts) >= 6 {
			path = strings.Join(parts[5:], " ")
		}
		var s, e uint64
		if _, err := fmt.Sscanf(parts[0], "%x-%x", &s, &e); err != nil {
			continue
		}
		if e > s {
			regs = append(regs, memRegion{uintptr(s), uintptr(e), path})
		}
	}
	return regs, nil
}

func memRegions(pid int) ([]memRegion, error) {
	regs, err := allRegions(pid, false)
	if err != nil {
		return nil, err
	}
	var out []memRegion
	for _, r := range regs {
		if r.path != "" && !strings.HasPrefix(r.path, "[") {
			continue // 只保留匿名区 + [heap]/[stack]
		}
		out = append(out, r)
	}
	return out, nil
}

func resolvePid(arg string) (int, error) {
	if pid, err := strconv.Atoi(arg); err == nil {
		return pid, nil
	}
	rc, so, _ := runCmd("pidof", arg)
	if rc != 0 || strings.TrimSpace(so) == "" {
		return 0, fmt.Errorf("找不到进程: %s", arg)
	}
	return strconv.Atoi(strings.Fields(so)[0])
}

func parseAddr(s string) (uintptr, error) {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	v, err := strconv.ParseUint(s, 16, 64)
	return uintptr(v), err
}

// 解析 hex 模式, 支持 ?? 通配 (AOB 签名用): 返回 (pattern, mask)
func parsePattern(s string) ([]byte, []byte, error) {
	s = strings.TrimSpace(s)
	if len(s)%2 != 0 {
		return nil, nil, fmt.Errorf("hex 长度须为偶数")
	}
	var pat, mask []byte
	for i := 0; i < len(s); i += 2 {
		p := s[i : i+2]
		if p == "??" || p == "?" {
			pat = append(pat, 0)
			mask = append(mask, 0)
		} else {
			b, err := hex.DecodeString(p)
			if err != nil {
				return nil, nil, fmt.Errorf("hex 解析失败: %s", p)
			}
			pat = append(pat, b[0])
			mask = append(mask, 0xFF)
		}
	}
	return pat, mask, nil
}

func maskedEqual(buf, pat, mask []byte) bool {
	for i := range pat {
		if buf[i]&mask[i] != pat[i] {
			return false
		}
	}
	return true
}

// 值编码: 返回 (字节串, 宽度)
func encodeValue(vtype, val string) ([]byte, int, error) {	switch vtype {
	case "i8":
		n, err := strconv.ParseInt(val, 0, 8)
		return []byte{byte(n)}, 1, err
	case "i16":
		n, err := strconv.ParseInt(val, 0, 16)
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, uint16(n))
		return b, 2, err
	case "i32":
		n, err := strconv.ParseInt(val, 0, 32)
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, uint32(n))
		return b, 4, err
	case "i64":
		n, err := strconv.ParseInt(val, 0, 64)
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, uint64(n))
		return b, 8, err
	case "f32":
		f, err := strconv.ParseFloat(val, 32)
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, math.Float32bits(float32(f)))
		return b, 4, err
	case "f64":
		f, err := strconv.ParseFloat(val, 64)
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, math.Float64bits(f))
		return b, 8, err
	case "str":
		return []byte(val), len(val), nil
	case "hex":
		b, err := hex.DecodeString(val)
		return b, len(b), err
	}
	return nil, 0, fmt.Errorf("未知类型 %s (支持 i8/i16/i32/i64/f32/f64/str/hex)", vtype)
}

// ---------- 方法 ----------

func init() {
	methods["mem_read"] = mMemRead
	methods["mem_write"] = mMemWrite
	methods["mem_search"] = mMemSearch
	methods["mem_pid"] = mMemPid
	methods["mem_refs"] = mMemRefs
	methods["mem_patch"] = mMemPatch
	methods["mem_patchlib"] = mMemPatch
	methods["hook"] = mHook
	methods["play"] = mPlay
	methods["scrcpy_bridge"] = mScrcpyBridge
	methods["vscreen_start"] = mVscreenStart
	methods["vscreen_stop"] = mVscreenStop
}

// libBase: 进程内共享库基址
func libBase(pid int, lib string) (uintptr, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, lib) {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 1 {
			continue
		}
		var s uint64
		if _, err := fmt.Sscanf(parts[0], "%x-", &s); err != nil {
			continue
		}
		return uintptr(s), nil
	}
	return 0, fmt.Errorf("库 %s 未在进程中加载", lib)
}

// patchMem4: ptrace 写 4 字节到任意段 (代码段), 保留相邻 4 字节
func patchMem4(pid int, addr uintptr, b []byte) error {
	if len(b) != 4 {
		return fmt.Errorf("仅支持 4 字节 patch")
	}
	if err := syscall.PtraceAttach(pid); err != nil {
		return fmt.Errorf("attach 失败: %v", err)
	}
	defer syscall.PtraceDetach(pid)
	var ws syscall.WaitStatus
	if _, err := syscall.Wait4(pid, &ws, 0, nil); err != nil {
		return fmt.Errorf("wait 失败: %v", err)
	}
	buf := make([]byte, 8)
	if _, err := syscall.PtracePeekData(pid, addr, buf); err != nil {
		return fmt.Errorf("读取原指令失败: %v", err)
	}
	orig := binary.LittleEndian.Uint64(buf)
	newW := (orig & uint64(0xFFFFFFFF00000000)) | uint64(binary.LittleEndian.Uint32(b))
	nb := make([]byte, 8)
	binary.LittleEndian.PutUint64(nb, newW)
	if _, err := syscall.PtracePokeData(pid, addr, nb); err != nil {
		return fmt.Errorf("写入指令失败: %v", err)
	}
	return nil
}

// mem_patch <pid|pkg> <addr_hex> <hex4>: ptrace 写 4 字节 (代码段 patch)
// mem_patchlib <pid|pkg> <lib> <offset_hex> <hex4>: 自动算库基址 + 偏移
func mMemPatch(c *conn, m Msg) {
	if len(m.Args) < 3 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: mem_patch <pid> <addr_hex> <hex4> | mem_patchlib <pid> <lib> <offset_hex> <hex4>"})
		return
	}
	pid, err := resolvePid(m.Args[0])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	var addr uintptr
	var hexstr string
	if m.Method == "mem_patchlib" {
		if len(m.Args) < 4 {
			c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: mem_patchlib <pid> <lib> <offset_hex> <hex4>"})
			return
		}
		base, err := libBase(pid, m.Args[1])
		if err != nil {
			c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
			return
		}
		off, err := parseAddr(m.Args[2])
		if err != nil {
			c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "偏移格式错误"})
			return
		}
		addr = base + off
		hexstr = m.Args[3]
	} else {
		var err error
		addr, err = parseAddr(m.Args[1])
		if err != nil {
			c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "地址格式错误"})
			return
		}
		hexstr = m.Args[2]
	}
	b, err := hex.DecodeString(hexstr)
	if err != nil || len(b) != 4 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "hex4 格式错误"})
		return
	}
	if err := patchMem4(pid, addr, b); err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: fmt.Sprintf("patched %x -> %s", addr, hexstr)})
}

// mem_read <pid|pkg> <addr_hex> <len>
func mMemRead(c *conn, m Msg) {
	if len(m.Args) < 3 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: mem_read <pid> <addr_hex> <len>"})
		return
	}
	pid, err := resolvePid(m.Args[0])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	addr, err := parseAddr(m.Args[1])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "地址格式错误: " + m.Args[1]})
		return
	}
	n, err := strconv.Atoi(m.Args[2])
	if err != nil || n <= 0 || n > 1<<20 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "长度需在 1-1MB"})
		return
	}
	buf := make([]byte, n)
	got, err := vmRead(pid, addr, buf)
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: fmt.Sprintf("读失败: %v", err)})
		return
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: fmt.Sprintf("%d bytes @ %x", got, addr), Data: hex.EncodeToString(buf[:got])})
}

// mem_write <pid|pkg> <addr_hex> <type> <value>
func mMemWrite(c *conn, m Msg) {
	if len(m.Args) < 4 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: mem_write <pid> <addr_hex> <i32|i64|f32|...> <value>"})
		return
	}
	pid, err := resolvePid(m.Args[0])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	addr, err := parseAddr(m.Args[1])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "地址格式错误: " + m.Args[1]})
		return
	}
	b, _, err := encodeValue(m.Args[2], m.Args[3])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	got, err := vmWrite(pid, addr, b)
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: fmt.Sprintf("写失败: %v", err)})
		return
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: fmt.Sprintf("%d bytes -> %x", got, addr)})
}

// mem_search <pid|pkg> <type> <value>   精确搜索 (全匿名可写区)
// mem_search <pid|pkg> <changed|increased|decreased>   过滤搜索 (基于上次结果)
func mMemSearch(c *conn, m Msg) {
	if len(m.Args) < 2 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: mem_search <pid> <type> <value> | mem_search <pid> <changed|increased|decreased>"})
		return
	}
	pid, err := resolvePid(m.Args[0])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	mode := m.Args[1]

	searchState.Lock()
	defer searchState.Unlock()
	ctx := searchState.m[pid]

	if mode == "changed" || mode == "increased" || mode == "decreased" {
		if ctx == nil || len(ctx.addrs) == 0 {
			c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "没有上次搜索结果, 先做一次精确搜索"})
			return
		}
		var newVals []byte
		var keep []uintptr
		for i, a := range ctx.addrs {
			buf := make([]byte, ctx.width)
			n, err := vmRead(pid, a, buf)
			if err != nil || n != ctx.width {
				continue // 地址失效(内存被释放)则丢弃
			}
			old := ctx.vals[i*ctx.width : (i+1)*ctx.width]
			keepIt := false
			switch mode {
			case "changed":
				keepIt = !bytesEqual(buf, old)
			case "increased":
				keepIt = cmpBytes(buf, old) > 0
			case "decreased":
				keepIt = cmpBytes(buf, old) < 0
			}
			if keepIt {
				keep = append(keep, a)
				newVals = append(newVals, buf...)
			}
		}
		ctx.addrs, ctx.vals = keep, newVals
		searchResult(c, m, pid, ctx)
		return
	}

	// 精确搜索: 全扫
	if len(m.Args) < 3 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: mem_search <pid> <type> <value> [max] [all]"})
		return
	}
	max := 5000
	if len(m.Args) >= 4 {
		if v, err := strconv.Atoi(m.Args[3]); err == nil && v > 0 {
			max = v
		}
	}
	all := false
	code := false
	if len(m.Args) >= 5 {
		all = m.Args[4] == "all"
		code = m.Args[4] == "code"
	}
	var pattern []byte
	var width int
	pattern, width, err = encodeValue(mode, m.Args[2])
	if err != nil && !(mode == "hex" && strings.Contains(m.Args[2], "?")) {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	regs, err := memRegions(pid)
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	if all || code {
		regs, err = allRegions(pid, code)
		if err != nil {
			c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
			return
		}
	}
	if len(regs) == 0 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "无可读写匿名区域 (进程不存在或权限不足)"})
		return
	}
	// ?? 通配模式 (AOB 签名)
	var mask []byte
	if mode == "hex" && strings.Contains(m.Args[2], "?") {
		var err error
		pattern, mask, err = parsePattern(m.Args[2])
		if err != nil {
			c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
			return
		}
		width = len(pattern)
	}
	var addrs []uintptr
	var vals []byte
	chunk := make([]byte, 1<<20) // 1MB 块
	for _, r := range regs {
		off := r.start
		for off < r.end {
			n := int(r.end - off)
			if n > len(chunk) {
				n = len(chunk)
			}
			got, err := vmRead(pid, off, chunk[:n])
			if err != nil || got <= 0 {
				break
			}
			// 找所有匹配位置 (重叠允许)
			for i := 0; i+width <= got; i++ {
				hit := false
				if mask != nil {
					hit = maskedEqual(chunk[i:i+width], pattern, mask)
				} else {
					hit = bytesEqual(chunk[i:i+width], pattern)
				}
				if hit {
					addrs = append(addrs, off+uintptr(i))
					vals = append(vals, chunk[i:i+width]...)
					if len(addrs) >= max {
						off = r.end
						break
					}
				}
			}
			if off == r.end {
				break
			}
			off += uintptr(got)
		}
		if len(addrs) >= max {
			break
		}
	}
	if ctx == nil {
		ctx = &searchCtx{}
		searchState.m[pid] = ctx
	}
	ctx.addrs, ctx.vals, ctx.width = addrs, vals, width
	searchResult(c, m, pid, ctx)
}

func searchResult(c *conn, m Msg, pid int, ctx *searchCtx) {
	type item struct {
		Addr  string `json:"addr"`
		Value string `json:"value"`
	}
	var items []item
	show := len(ctx.addrs)
	if show > 200 {
		show = 200
	}
	for i := 0; i < show; i++ {
		items = append(items, item{Addr: fmt.Sprintf("%x", ctx.addrs[i]), Value: hex.EncodeToString(ctx.vals[i*ctx.width : (i+1)*ctx.width])})
	}
	data, _ := json.Marshal(map[string]any{
		"pid": pid, "count": len(ctx.addrs), "width": ctx.width, "sample": items,
	})
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: string(data),
		Stdout: fmt.Sprintf("匹配 %d 个地址 (显示前 %d)", len(ctx.addrs), show)})
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 无符号比较 (i32 值 0xFFFFFFFF 视为大数)
func cmpBytes(a, b []byte) int {
	if len(a) != len(b) {
		return 0
	}
	for i := len(a) - 1; i >= 0; i-- {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// mem_pid <pkg>
func mMemPid(c *conn, m Msg) {
	if len(m.Args) < 1 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: mem_pid <pkg>"})
		return
	}
	pid, err := resolvePid(m.Args[0])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: strconv.Itoa(pid)})
}

// mem_refs <pid|pkg> <addr_hex> [max] [range]
// 反向指针搜索: 找指向 addr±range 的 8 字节指针 (range>0 用于对象内字段)
// 返回: [{ref, path, rel, dist}] ref=指针所在地址, path=所在映射, rel=段内偏移, dist=指向地址与 target 的距离
func mMemRefs(c *conn, m Msg) {
	if len(m.Args) < 2 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: mem_refs <pid> <addr_hex> [max] [range]"})
		return
	}
	pid, err := resolvePid(m.Args[0])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	addr, err := parseAddr(m.Args[1])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "地址格式错误: " + m.Args[1]})
		return
	}
	max := 200
	if len(m.Args) >= 3 {
		if v, err := strconv.Atoi(m.Args[2]); err == nil && v > 0 {
			max = v
		}
	}
	rng := uint64(0)
	if len(m.Args) >= 4 {
		if v, err := strconv.ParseUint(m.Args[3], 0, 64); err == nil {
			rng = v
		}
	}
	lo, hi := uint64(addr)-rng, uint64(addr)+rng
	regs, err := allRegions(pid, false)
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	type refItem struct {
		Ref  string `json:"ref"`
		Path string `json:"path"`
		Rel  string `json:"rel"`
		Dist string `json:"dist"`
	}
	var refs []refItem
	chunk := make([]byte, 1<<20)
	for _, r := range regs {
		off := r.start
		for off < r.end {
			n := int(r.end - off)
			if n > len(chunk) {
				n = len(chunk)
			}
			got, err := vmRead(pid, off, chunk[:n])
			if err != nil || got <= 0 {
				break
			}
			for i := 0; i+8 <= got; i++ {
				v := binary.LittleEndian.Uint64(chunk[i : i+8])
				if v >= lo && v <= hi {
					refs = append(refs, refItem{
						Ref:  fmt.Sprintf("%x", off+uintptr(i)),
						Path: r.path,
						Rel:  fmt.Sprintf("%x", off+uintptr(i)-r.start),
						Dist: fmt.Sprintf("%x", int64(v)-int64(addr)),
					})
					if len(refs) >= max {
						off = r.end
						break
					}
				}
			}
			if off == r.end {
				break
			}
			off += uintptr(got)
		}
		if len(refs) >= max {
			break
		}
	}
	data, _ := json.Marshal(map[string]any{"target": fmt.Sprintf("%x", addr), "range": rng, "count": len(refs), "refs": refs})
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: string(data),
		Stdout: fmt.Sprintf("找到 %d 个引用 (range=0x%x)", len(refs), rng)})
}
