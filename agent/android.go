package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func init() {
	methods["shell"] = mShell
	methods["apps"] = mApps
	methods["install"] = mInstall
	methods["extract"] = mExtract
	methods["push"] = mPush
	methods["pull"] = mPull
	methods["logcat"] = mLogcat
	methods["sysinfo"] = mSysinfo
}

func runCmd(name string, args ...string) (int, string, string) {
	cmd := exec.Command(name, args...)
	var so, se strings.Builder
	cmd.Stdout = &so
	cmd.Stderr = &se
	err := cmd.Run()
	rc := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			rc = ee.ExitCode()
		} else {
			rc = -1
			se.WriteString(err.Error())
		}
	}
	return rc, so.String(), se.String()
}

func deviceInfo() string {
	rc, so, _ := runCmd("getprop", "ro.product.model")
	if rc == 0 && strings.TrimSpace(so) != "" {
		return strings.TrimSpace(so)
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return runtime.GOOS
}

// shell: 任意 root 命令
func mShell(c *conn, m Msg) {
	rc, so, se := runCmd("sh", "-c", strings.Join(m.Args, " "))
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(rc == 0), RC: rc, Stdout: so, Stderr: se})
}

// apps: 第三方应用列表
func mApps(c *conn, m Msg) {
	rc, so, se := runCmd("pm", "list", "packages", "-3")
	var pkgs []string
	for _, l := range strings.Split(so, "\n") {
		if strings.HasPrefix(l, "package:") {
			pkgs = append(pkgs, strings.TrimPrefix(l, "package:"))
		}
	}
	data, _ := json.Marshal(pkgs)
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(rc == 0), RC: rc, Stdout: so, Stderr: se, Data: string(data)})
}

// install: 安装 apk (root)
func mInstall(c *conn, m Msg) {
	if len(m.Args) < 1 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: install <path>"})
		return
	}
	rc, so, se := runCmd("pm", "install", "-r", m.Args[0])
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(rc == 0), RC: rc, Stdout: so, Stderr: se})
}

// extract: 提取 apk 全 split 到 outdir
func mExtract(c *conn, m Msg) {
	if len(m.Args) < 2 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: extract <pkg> <outdir>"})
		return
	}
	pkg, out := m.Args[0], m.Args[1]
	rc, so, _ := runCmd("pm", "path", pkg)
	if rc != 0 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "pm path 失败: " + so})
		return
	}
	var files []string
	for _, l := range strings.Split(so, "\n") {
		p := strings.TrimPrefix(l, "package:")
		if p == "" || p == l {
			continue
		}
		dest := filepath.Join(out, pkg, filepath.Base(p))
		if err := copyFile(p, dest); err != nil {
			c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "复制 " + p + " 失败: " + err.Error()})
			return
		}
		files = append(files, dest)
	}
	data, _ := json.Marshal(files)
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: string(data)})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// push: base64 内容写文件 (data 字段)
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

// pull: 读文件 base64 返回 (data 字段)
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

// logcat: 流式推送, 连接断开即停
func mLogcat(c *conn, m Msg) {
	args := []string{}
	if len(m.Args) > 0 && m.Args[0] != "" {
		args = append(args, "-s", m.Args[0])
	}
	cmd := exec.Command("logcat", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: err.Error()})
		return
	}
	c.mu.Lock()
	c.streamingCmd = cmd
	c.mu.Unlock()
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "logcat streaming"})
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		c.send(Msg{T: "evt", Ev: "log", Data: sc.Text()})
	}
	cmd.Wait()
}

// sysinfo: 设备信息
func mSysinfo(c *conn, m Msg) {
	info := map[string]string{"goos": runtime.GOOS, "goarch": runtime.GOARCH}
	for _, kv := range [][2]string{
		{"model", "ro.product.model"}, {"brand", "ro.product.brand"},
		{"android", "ro.build.version.release"}, {"sdk", "ro.build.version.sdk"},
	} {
		rc, so, _ := runCmd("getprop", kv[1])
		if rc == 0 && strings.TrimSpace(so) != "" {
			info[kv[0]] = strings.TrimSpace(so)
		}
	}
	data, _ := json.Marshal(info)
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: string(data)})
}
