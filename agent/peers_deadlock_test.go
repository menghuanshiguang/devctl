package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 把 dash.json 重定向到临时目录, 避免依赖 /data/local 且不污染真实状态文件。
func withTempDash(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dashPath = filepath.Join(dir, "sub", "dash.json")
	t.Cleanup(func() { dashPath = defaultDashPath() })
	return dashPath
}

// 每个测试都用超时保护：若实现里存在"持锁后再调用会加锁的函数"，
// 就会永久挂起，被这里捕获为失败。
func mustNotHang(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s 死锁挂起 (持锁后再调用会加锁的函数)", name)
	}
}

func TestPeerAddDoesNotDeadlock(t *testing.T) {
	withTempDash(t)
	mustNotHang(t, "peerAdd", func() { peerAdd("probe", "10.0.0.9:1234") })
}

func TestPeerMarkCmdDoesNotDeadlock(t *testing.T) {
	withTempDash(t)
	mustNotHang(t, "peerAdd", func() { peerAdd("probe", "10.0.0.9:1234") })
	mustNotHang(t, "peerMarkCmd", func() { peerMarkCmd("10.0.0.9:1234", "sysinfo") })
}

func TestPeerDelDoesNotDeadlock(t *testing.T) {
	withTempDash(t)
	mustNotHang(t, "peerAdd", func() { peerAdd("probe", "10.0.0.9:1234") })
	mustNotHang(t, "peerDel", func() { peerDel("10.0.0.9:1234") })
}

func TestPeerListDoesNotDeadlock(t *testing.T) {
	mustNotHang(t, "peerList", func() { peerList() })
}

// 功能验证: 连接登记后 dash.json 内容正确, 断开后清空。
func TestDashFileWritten(t *testing.T) {
	path := withTempDash(t)
	peerAdd("myctl", "10.0.0.9:1234")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dash.json 未写出: %v", err)
	}
	var d dashInfo
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("dash.json 无法解析: %v", err)
	}
	if len(d.Peers) != 1 || d.Peers[0].Name != "myctl" || d.Peers[0].Addr != "10.0.0.9:1234" {
		t.Fatalf("peers 内容不对: %+v", d.Peers)
	}

	peerMarkCmd("10.0.0.9:1234", "install")
	raw, _ = os.ReadFile(path)
	d = dashInfo{}
	_ = json.Unmarshal(raw, &d)
	if len(d.Peers) != 1 || d.Peers[0].LastCmd != "install" {
		t.Fatalf("last_cmd 未更新: %+v", d.Peers)
	}

	peerDel("10.0.0.9:1234")
	raw, _ = os.ReadFile(path)
	d = dashInfo{}
	_ = json.Unmarshal(raw, &d)
	if len(d.Peers) != 0 {
		t.Fatalf("断开后 peers 未清空: %+v", d.Peers)
	}
}

// 并发压测: 多客户端同时连入/命令/断开, 修复前必然死锁。
func TestPeerConcurrent(t *testing.T) {
	withTempDash(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			addr := "10.0.0.9:1" + string(rune('0'+i%10))
			peerAdd("c", addr)
			peerMarkCmd(addr, "shell")
			peerList()
			peerDel(addr)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("并发场景死锁")
	}
}
