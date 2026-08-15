//go:build !android

package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

// push/pull: 通用文件传输 (Android 版在 android.go 内)

func init() {
	methods["push"] = mPush
	methods["pull"] = mPull
}

func mPush(c *conn, m Msg) {
	if len(m.Args) < 1 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: push <remote>"})
		return
	}
	b, err := base64.StdEncoding.DecodeString(m.Data)
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "base64 解码失败"})
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.Args[0]), 0755); err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	if err := os.WriteFile(m.Args[0], b, 0644); err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: fmt.Sprintf("%d bytes -> %s", len(b), m.Args[0])})
}

func mPull(c *conn, m Msg) {
	if len(m.Args) < 1 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: pull <remote>"})
		return
	}
	b, err := os.ReadFile(m.Args[0])
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: base64.StdEncoding.EncodeToString(b), Stdout: fmt.Sprintf("%d bytes", len(b))})
}
