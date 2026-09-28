package main

// peers.go: 活跃客户端连接追踪 + 状态文件 dash.json
//
// dash.json: agent 在连接/断开时刷新 (连接数/客户端列表/时间戳)。
// 用途: 设备端 `cat /data/local/devctl/dash.json` 即可查看谁在连接本机, 无需悬浮窗。
//
// 安全: 只记录已鉴权 (hello 通过) 的连接; 客户端名取 hello.name (控制端自报)。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// 平台化路径: Windows 用 %ProgramData%\devctl\, 其余用 /data/local/devctl/。
// 变量而非常量, 便于测试重定向到临时目录。
func defaultDashPath() string {
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "devctl", "dash.json")
	}
	return "/data/local/devctl/dash.json"
}

var dashPath = defaultDashPath()

type peerInfo struct {
	Name    string `json:"name"`
	Addr    string `json:"addr"`
	Since   string `json:"since"`
	LastCmd string `json:"last_cmd"`
}

type dashInfo struct {
	AgentVersion string     `json:"agent_version"`
	Now          string     `json:"now"`
	Peers        []peerInfo `json:"peers"`
}

var (
	peersMu sync.Mutex
	peers   = map[string]*peerInfo{} // key = remoteAddr
)

// peerAdd 登记一条已鉴权连接。
//
// 注意: 这里刻意"先在锁内取快照, 再在锁外落盘"。
// 早期实现是 peerAdd(持锁) → writeDashLocked → peerList(再次拿同一把锁),
// Go 的 sync.Mutex 不可重入, 于是每个连上来的客户端都会让该 goroutine
// 永久死锁 (表现: hello 有正确 token 却永远收不到 hello_ack, 而错 token
// 因为走的是另一条分支能正常回包)。同时落盘也不再占用锁。
func peerAdd(name, addr string) {
	peersMu.Lock()
	peers[addr] = &peerInfo{Name: name, Addr: addr, Since: time.Now().Format("15:04:05")}
	snap := peerListLocked()
	peersMu.Unlock()
	writeDash(snap)
}

func peerMarkCmd(addr, method string) {
	peersMu.Lock()
	p, ok := peers[addr]
	if ok {
		p.LastCmd = method
	}
	var snap []peerInfo
	if ok {
		snap = peerListLocked()
	}
	peersMu.Unlock()
	if ok {
		writeDash(snap)
	}
}

func peerDel(addr string) {
	peersMu.Lock()
	_, ok := peers[addr]
	if ok {
		delete(peers, addr)
	}
	snap := peerListLocked()
	peersMu.Unlock()
	if ok {
		writeDash(snap)
	}
}

// peerListLocked 只做纯内存读取, 调用方必须已持有 peersMu。
func peerListLocked() []peerInfo {
	out := make([]peerInfo, 0, len(peers))
	for _, p := range peers {
		out = append(out, *p)
	}
	return out
}

func peerList() []peerInfo {
	peersMu.Lock()
	defer peersMu.Unlock()
	return peerListLocked()
}

// writeDash 落盘状态快照。任何失败都静默忽略 —— 状态文件只是便利功能,
// 绝不能因为它出错而影响握手或命令链路。
func writeDash(list []peerInfo) {
	d := dashInfo{AgentVersion: version, Now: time.Now().Format("15:04:05"), Peers: list}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return
	}
	if dir := filepath.Dir(dashPath); dir != "" && dir != "." {
		if os.MkdirAll(dir, 0755) != nil {
			return
		}
	}
	tmp := dashPath + ".tmp"
	if os.WriteFile(tmp, b, 0644) != nil {
		return
	}
	_ = os.Rename(tmp, dashPath)
}

func init() {
	methods["peers"] = mPeers
}

func mPeers(c *conn, m Msg) {
	b, _ := json.Marshal(peerList())
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: string(b)})
}
