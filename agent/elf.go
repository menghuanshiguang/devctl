//go:build android

package main

import (
	"debug/elf"
	"fmt"
	"os"
	"strings"
)

// 解析 so 文件符号地址: 返回 (运行地址, 错误)
// baseAddr: so 在进程中的加载基址 (maps 里第一个映射的起始)
func elfSymbolAddr(soPath string, baseAddr uintptr, symName string) (uintptr, error) {
	f, err := elf.Open(soPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	// 动态符号表
	syms, err := f.DynamicSymbols()
	if err != nil {
		return 0, err
	}
	for _, s := range syms {
		if s.Name == symName && s.Value != 0 {
			return baseAddr + uintptr(s.Value), nil
		}
	}
	return 0, fmt.Errorf("符号 %s 未找到", symName)
}

// findSvcInstr: 在 libc.so 中搜索 svc #0 指令地址 (返回文件偏移)
// arm64 svc #0 = 0xD4000001, 内存字节: 01 00 00 D4
func findSvcInstr(libcPath string) (int64, error) {
	data, err := os.ReadFile(libcPath)
	if err != nil {
		return 0, err
	}
	for i := 0; i+4 <= len(data); i++ {
		if data[i] == 0x01 && data[i+1] == 0x00 && data[i+2] == 0x00 && data[i+3] == 0xD4 {
			return int64(i), nil
		}
	}
	return 0, fmt.Errorf("libc 中未找到 svc 指令")
}

// libBaseOf: 从 maps 找库的加载基址 (第一个包含 lib 的映射起始)
func libBaseOf(pid int, lib string) (uintptr, error) {
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
	return 0, fmt.Errorf("库 %s 未加载", lib)
}
