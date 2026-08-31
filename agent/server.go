package main

import (
	"bufio"
	"encoding/json"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

// ops.log: 接收端操作审计记录 (每次命令一行 JSON)
const opsLogPath = "/data/local/devctl/ops.log"

var opsMu sync.Mutex

func logOps(m Msg, ms int64) {
	opsMu.Lock()
	defer opsMu.Unlock()
	f, err := os.OpenFile(opsLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	args, _ := json.Marshal(m.Args)
	fmt.Fprintf(f, `{"t":"%s","method":"%s","args":%s,"ms":%d}`+"\n",
		time.Now().Format(time.RFC3339), m.Method, args, ms)
}

type conn struct {
	nc           net.Conn
	r            *bufio.Reader
	mu           sync.Mutex
	authed       bool
	streamingCmd *exec.Cmd
	peerAddr     string // 远端地址 (peers 追踪 key)
	peerName     string // 客户端自报名 (hello.name)
}

var methods = map[string]func(*conn, Msg){}

func serve(port int, token string) error {
	cert, err := loadOrCreateCert()
	if err != nil {
		return fmt.Errorf("TLS 证书初始化失败: %v", err)
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", port), tlsCfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "devctl-agent %s listening on :%d (TLS)\n", version, port)
	for {
		nc, err := ln.Accept()
		if err != nil {
			return err
		}
		go handle(nc, token)
	}
}

func handle(nc net.Conn, token string) {
	c := &conn{nc: nc, r: bufio.NewReaderSize(nc, 1<<20), peerAddr: nc.RemoteAddr().String()}
	defer nc.Close()
	defer func() {
		peerDel(c.peerAddr)
		c.mu.Lock()
		if c.streamingCmd != nil {
			c.streamingCmd.Process.Kill()
		}
		c.mu.Unlock()
	}()
	nc.SetDeadline(time.Now().Add(300 * time.Second)) // 300s idle 断开, 每次收到消息刷新
	for {
		line, err := c.r.ReadBytes('\n')
		if err != nil {
			return
		}
		nc.SetDeadline(time.Now().Add(300 * time.Second))
		var m Msg
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if !c.authed {
			if m.T != "hello" || m.Token != token {
				c.send(Msg{T: "hello_ack", Ok: boolp(false), Stderr: "bad token"})
				return
			}
			c.authed = true
			c.peerName = m.Name
			peerAdd(m.Name, c.peerAddr)
			c.send(Msg{T: "hello_ack", Ok: boolp(true), Version: version, Device: deviceInfo()})
			continue
		}
		switch m.T {
		case "ping":
			c.send(Msg{T: "pong"})
		case "cmd":
			go c.dispatch(m)
		case "bye":
			return
		}
	}
}

func (c *conn) send(m Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := json.Marshal(m)
	c.nc.Write(append(b, '\n'))
}

func (c *conn) dispatch(m Msg) {
	fn, ok := methods[m.Method]
	if !ok {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "unknown method: " + m.Method})
		return
	}
	peerMarkCmd(c.peerAddr, m.Method)
	start := time.Now()
	fn(c, m)
	logOps(m, time.Since(start).Milliseconds())
}
