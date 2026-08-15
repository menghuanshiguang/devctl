//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Windows 接收器: shell/sysinfo/ps 方法

func init() {
	methods["shell"] = mShell
	methods["sysinfo"] = mSysinfo
	methods["ps"] = mPs
}

func deviceInfo() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return runtime.GOOS
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

// shell: Windows 命令 (cmd /c)
func mShell(c *conn, m Msg) {
	rc, so, se := runCmd("cmd", "/c", strings.Join(m.Args, " "))
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(rc == 0), RC: rc, Stdout: so, Stderr: se})
}

// sysinfo: Windows 系统信息
func mSysinfo(c *conn, m Msg) {
	info := map[string]string{
		"goos":   runtime.GOOS,
		"goarch": runtime.GOARCH,
		"host":   deviceInfo(),
		"version": func() string {
			rc, so, _ := runCmd("cmd", "/c", "ver")
			if rc == 0 {
				return strings.TrimSpace(so)
			}
			return "unknown"
		}(),
	}
	// CPU/内存
	if rc, so, _ := runCmd("cmd", "/c", "wmic cpu get name /value 2>nul"); rc == 0 {
		for _, l := range strings.Split(so, "\n") {
			if strings.HasPrefix(l, "Name=") {
				info["cpu"] = strings.TrimPrefix(strings.TrimSpace(l), "Name=")
			}
		}
	}
	data, _ := json.Marshal(info)
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: string(data)})
}

// ps: 进程列表 (tasklist)
func mPs(c *conn, m Msg) {
	rc, so, se := runCmd("tasklist")
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(rc == 0), RC: rc, Stdout: so, Stderr: se})
}

var _ = fmt.Sprintf
