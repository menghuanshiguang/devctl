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
	"sync"
	"time"
)

const dashPath = "/data/local/devctl/dash.json"

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

func peerAdd(name, addr string) {
	peersMu.Lock()
	defer peersMu.Unlock()
	peers[addr] = &peerInfo{Name: name, Addr: addr, Since: time.Now().Format("15:04:05")}
	writeDashLocked()
}

func peerMarkCmd(addr, method string) {
	peersMu.Lock()
	defer peersMu.Unlock()
	if p, ok := peers[addr]; ok {
		p.LastCmd = method
		writeDashLocked()
	}
}

func peerDel(addr string) {
	peersMu.Lock()
	defer peersMu.Unlock()
	if _, ok := peers[addr]; ok {
		delete(peers, addr)
		writeDashLocked()
	}
}

func peerList() []peerInfo {
	peersMu.Lock()
	defer peersMu.Unlock()
	out := make([]peerInfo, 0, len(peers))
	for _, p := range peers {
		out = append(out, *p)
	}
	return out
}

func writeDashLocked() {
	d := dashInfo{AgentVersion: version, Now: time.Now().Format("15:04:05"), Peers: peerList()}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return
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
