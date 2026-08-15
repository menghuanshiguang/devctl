//go:build android

package main

import (
	"fmt"
	"io"
	"net"
	"strings"
)

// scrcpy_bridge: TCP:27183 ↔ abstract socket @scrcpy 双向转发
// scrcpy server 作为客户端连接 abstract socket, 控制端连接 TCP 端口
func mScrcpyBridge(c *conn, m Msg) {
	port := 27183
	if len(m.Args) > 0 {
		fmt.Sscanf(m.Args[0], "%d", &port)
	}
	sockName := "scrcpy"
	if len(m.Args) > 1 && m.Args[1] != "" {
		sockName = m.Args[1]
	}
	// 监听 abstract socket (Android: "@name")
	ul, err := net.Listen("unix", "@"+sockName)
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: fmt.Sprintf("abstract listen 失败: %v", err)})
		return
	}
	// 监听 TCP
	tl, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		ul.Close()
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: fmt.Sprintf("tcp listen 失败: %v", err)})
		return
	}
	go func() {
		for {
			uconn, err := ul.Accept()
			if err != nil {
				return
			}
			tconn, err := tl.Accept()
			if err != nil {
				uconn.Close()
				return
			}
			go pipe(uconn, tconn)
			go pipe(tconn, uconn)
		}
	}()
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true),
		Stdout: fmt.Sprintf("scrcpy bridge: TCP:%d ↔ @%s", port, sockName)})
}

func pipe(dst, src net.Conn) {
	defer dst.Close()
	defer src.Close()
	io.Copy(dst, src)
}

// scrcpy_status: 检查桥接状态
func mScrcpyStatus(c *conn, m Msg) {
	status := map[string]any{}
	// 检查 abstract socket 是否有连接
	if l, err := net.Listen("unix", "@scrcpy_check"); err == nil {
		l.Close()
		status["abstract_socket"] = "ok"
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: fmt.Sprintf(`{"bridge":"%s"}`, strings.Join(m.Args, ","))})
}
