package main

import (
	"encoding/binary"
)

// arm64 机器码生成 (hook libGLESv2_adreno.so 的 eglSwapBuffers)

// encLdrX16Lit: LDR X16, [PC+8] (literal load)
func encLdrX16Lit() uint32 { return 0x58000050 }

// encBrX16: BR X16
func encBrX16() uint32 { return 0xD61F0200 }

// encMovX16Imm: MOVZ X16, #imm16, LSL #(shift)
func encMovX16Imm(imm uint64, shift uint32) uint32 {
	return 0xD2800000 | uint32(imm&0xFFFF)<<5 | shift<<21 | 16
}

// encMovkX16Imm: MOVK X16, #imm16, LSL #shift
func encMovkX16Imm(imm uint64, shift uint32) uint32 {
	return 0xF2800000 | uint32(imm&0xFFFF)<<5 | shift<<21 | 16
}

// encMovX: MOVZ Xd, #imm16 (d 0-30)
func encMovX(d uint32, imm uint64, shift uint32) uint32 {
	return 0xD2800000 | uint32(imm&0xFFFF)<<5 | shift<<21 | d
}

// encMovkX: MOVK Xd, #imm16
func encMovkX(d uint32, imm uint64, shift uint32) uint32 {
	return 0xF2800000 | uint32(imm&0xFFFF)<<5 | shift<<21 | d
}

// encStpX29: STP X29, X30, [SP, #-16]!
func encStpX29() uint32 { return 0xA9BF7BFD }

// encStpPre: STP Xt1, Xt2, [SP, #-16]! (t1,t2 0-30)
func encStpPre(t1, t2 uint32) uint32 {
	return 0xA9800000 | (t2&0x1F)<<10 | (t1&0x1F)<<5 | 0x1F<<16 | 0x3
}

// encLdpPost: LDP Xt1, Xt2, [SP], #16
func encLdpPost(t1, t2 uint32) uint32 {
	return 0xA8C00000 | (t2&0x1F)<<10 | (t1&0x1F)<<5 | 0x1F<<16 | 0x3
}

// encMovReg: MOV Xd, Xm (ORR Xd, XZR, Xm)
func encMovReg(d, m uint32) uint32 {
	return 0xAA0003E0 | (m&0x1F)<<16 | d&0x1F
}

// encBlrX16: BLR X16
func encBlrX16() uint32 { return 0xD63F0200 }

// encRet: RET
func encRet() uint32 { return 0xD65F03C0 }

// encNop: NOP
func encNop() uint32 { return 0xD503201F }

// ldrX16Imm64: 生成加载 64 位立即数到 X16 的指令序列 (MOVZ+MOVK x4)
func ldrX16Imm64(addr uint64) []uint32 {
	return []uint32{
		encMovX16Imm(addr, 0),
		encMovkX16Imm(addr, 16),
		encMovkX16Imm(addr, 32),
		encMovkX16Imm(addr, 48),
	}
}

// buildJumpPatch: 12 字节跳转补丁 (LDR X16,#8; BR X16; addr)
func buildJumpPatch(target uint64) []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b[0:], encLdrX16Lit())
	binary.LittleEndian.PutUint32(b[4:], encBrX16())
	binary.LittleEndian.PutUint64(b[8:], target)
	return b
}

// buildTrampoline: 原指令 + 跳回原函数+len(orig)
func buildTrampoline(orig []byte, resumeAddr uint64) []byte {
	t := make([]byte, 0, 12+16)
	t = append(t, orig...)
	for _, ins := range ldrX16Imm64(resumeAddr) {
		w := make([]byte, 4)
		binary.LittleEndian.PutUint32(w, ins)
		t = append(t, w...)
	}
	w := make([]byte, 4)
	binary.LittleEndian.PutUint32(w, encBrX16())
	t = append(t, w...)
	return t
}

// buildHookFunc: hook 函数机器码
// 布局: [hook 函数][trampoline][frame buffer]
// 参数: trampolineAddr 原函数地址, glReadPixels 地址, w/h, frameBufAddr
func buildHookFunc(trampolineAddr, glReadPixelsAddr uint64, w, h uint32, frameBufAddr uint64) []byte {
	var code []uint32
	// 保存寄存器
	code = append(code, encStpX29())                 // STP X29,X30,[SP,#-16]!
	code = append(code, encStpPre(19, 20))           // STP X19,X20,[SP,#-16]!
	code = append(code, encStpPre(21, 22))           // STP X21,X22,[SP,#-16]!
	// X19=display(X0), X20=surface(X1), X21=glReadPixels 地址
	code = append(code, encMovReg(19, 0))
	code = append(code, encMovReg(20, 1))
	// 调原函数: LDR X16,=trampoline; BLR X16
	code = append(code, ldrX16Imm64(trampolineAddr)...)
	code = append(code, encBlrX16())
	code = append(code, encMovReg(22, 0)) // X22 = 原返回值
	// glReadPixels(0,0,w,h,GL_RGBA=0x1908,GL_UNSIGNED_BYTE=0x1401,frameBuf)
	code = append(code, encMovX(0, 0, 0))
	code = append(code, encMovX(1, 0, 0))
	code = append(code, encMovX(2, uint64(w), 0))
	code = append(code, encMovX(3, uint64(h), 0))
	code = append(code, encMovX(4, 0x1908, 0))
	code = append(code, encMovX(5, 0x1401, 0))
	code = append(code, ldrX16Imm64(frameBufAddr)...)
	code = append(code, encMovReg(6, 16)) // X6 = X16 (frameBuf)
	code = append(code, ldrX16Imm64(glReadPixelsAddr)...)
	code = append(code, encBlrX16())
	// 恢复
	code = append(code, encMovReg(0, 22))
	code = append(code, encLdpPost(21, 22))
	code = append(code, encLdpPost(19, 20))
	code = append(code, encLdpPost(29, 30))
	code = append(code, encRet())
	// 转字节
	var out []byte
	for _, ins := range code {
		w := make([]byte, 4)
		binary.LittleEndian.PutUint32(w, ins)
		out = append(out, w...)
	}
	return out
}
