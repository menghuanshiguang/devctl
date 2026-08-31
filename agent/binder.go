//go:build ignore

// binder.go: binder 直连实验半成品 (SM 握手未完成), 不参与构建 (见 DESIGN 记录)

package main

// binder.go: Android Binder 客户端 (纯 Go, 无 libbinder 依赖)
// 用途: 直连 SurfaceFlinger 创建渲染 Layer (不 attach, 不写 SF 内存)

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"unsafe"
)

// ============ binder ioctl ============

type binderWriteRead struct {
	writeSize    uintptr
	writeConsumed uintptr
	writeBuffer  uintptr
	readSize     uintptr
	readConsumed uintptr
	readBuffer   uintptr
}

const (
	BC_TRANSACTION    = 0x0040
	BC_REPLY          = 0x0041
	BC_ENTER_LOOPER   = 0x0004
	BC_REQUEST_DEATH_NOTIFICATION = 0x0009
	BC_FREE_BUFFER    = 0x0002
	BC_RELEASE        = 0x0020? // placeholder, 用不到也行
	BC_ACQUIRE        = 0x0007
	BC_INCREFS        = 0x0006
	BR_NOOP           = 0x0006
	BR_TRANSACTION    = 0x0008
	BR_REPLY          = 0x0009
	BR_DEAD_REPLY     = 0x0007
	BR_TRANSACTION_COMPLETE = 0x000E
	BR_ACQUIRE_RELEASE? 
)

// binder driver ioctl 码
const (
	BINDER_WRITE_READ = 0xc0186201 // _IOWR('b', 1, struct binder_write_read)
	BINDER_VERSION    = 0x40046204 // _IOR('b', 3, int)
)

// flat_binder_object + binder_transaction_data 结构 (arm64)
type flatBinderObject struct {
	Type       uint32 // BINDER_TYPE_HANDLE = 0x03
	Flags      uint32
	Handle     uint32
	_          uint32
	Cookie     uint64
	_          [6]uint64
}

// ============ Parcel (最小实现) ============

type parcelWriter struct {
	data    []byte
	offsets []int32
}

func (p *parcelWriter) writeInt32(v int32) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	p.data = append(p.data, b...)
}

func (p *parcelWriter) writeString16(s string) {
	p.writeInt32(int32(len(s)))
	for _, r := range utf16Encode(s) {
		b := make([]byte, 2)
		binary.LittleEndian.PutUint16(b, r)
		p.data = append(p.data, b...)
	}
}

func (p *parcelWriter) writeInterfaceToken(token string) {
	p.writeInt32(0x00060004) // StrictModePolicy diskRead? 写 token 的固定魔术: 4? 用标准: parcel writeInt32(0x00060004)? 实际: writeInterfaceToken = writeInt32(STRICT_MODE_PENALTY_GATHER=0x200000? no.
	// 标准 AIDL: writeInt32(STRICT_MODE_POLICY=0)? 简化: 0. token 前还要 align?
	p.writeInt32(0)
	p.writeString16(token)
}

func utf16Encode(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

// ============ Binder 事务发送 ============

// BinderSend: 打开 binder 驱动并发送一个单事务, 返回 reply parcel 原始数据
func BinderSend(targetHandle uint32, code uint32, req *parcelWriter) ([]byte, error) {
	fd, err := syscall.Open("/dev/binder", syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/binder: %v", err)
	}
	defer syscall.Close(fd)

	// 版本检查
	ver := make([]byte, 4)
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), BINDER_VERSION, uintptr(unsafe.Pointer(&ver[0]))); errno != 0 {
		return nil, fmt.Errorf("BINDER_VERSION: %v", errno)
	}

	// mmap (1MB)
	bmap, err := syscall.Mmap(fd, 0, 1<<20, syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("binder mmap: %v", err)
	}
	defer syscall.Munmap(bmap)

	// 构造 BC_TRANSACTION
	var bait []byte // binder_transaction_data 结构 (arm64)
	bait = make([]byte, 256)
	// 取 parcel data 指针放内核临时? 不在内核: 我们直接写用户态数据指针
	txd := make([]byte, 160) // binder_transaction_data 大小
	// txd 布局 (arm64):
	//   handle u32 @0; pad @4
	//   code u32 @8; flags u32 @12
	//   data_size u32 @16; offsets_size u32 @20
	//   data_ptr u64 @24 (target ptr); offsets_ptr u64 @32
	//   data_size2 u64 @40; data_buf_size u64 @48; offsets_size2 u64 @56; offsets_buf_size u64 @64
	//   sender_pid u64 @72? 不对——重写: binder_transaction_data:
	//   handle,pad(code,flags),data_size,data_size_buf,offsets_size,offsets_size_buf ???
	// 简化: 用标准内存布局 struct

	_ = bait
	_ = txd
	return nil, nil
}

// findServiceBinder: 通过 ServiceManager 查找服务 (code=1 getService)
// handler 位置: /dev/binder 句柄(handle 0) 即 ServiceManager
func findServiceBinder(name string) (uint32, error) {
	return 0, fmt.Errorf("not implemented yet")
}
