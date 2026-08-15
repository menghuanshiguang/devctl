//go:build linux && !android

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Linux 版 (iSH 本地测试用): shell/sysinfo

func init() {
	methods["shell"] = mShell
	methods["sysinfo"] = mSysinfo
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

func mShell(c *conn, m Msg) {
	rc, so, se := runCmd("sh", "-c", strings.Join(m.Args, " "))
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(rc == 0), RC: rc, Stdout: so, Stderr: se})
}

func mSysinfo(c *conn, m Msg) {
	info := map[string]string{
		"goos": runtime.GOOS, "goarch": runtime.GOARCH, "host": deviceInfo(),
	}
	data, _ := json.Marshal(info)
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Data: string(data)})
}
