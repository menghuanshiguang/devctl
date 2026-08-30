package main

import (
	"fmt"
	"os"
	"time"
)

// meta.go: 通用 meta 方法 (Android/Windows 共享)

func init() {
	methods["exit"] = mExit
}

// mExit: 优雅退出自身进程
// 注: Magisk 模块的 service.sh 会在 10s 后自动拉起, 因此等价于"重启到最新二进制";
// 若需彻底关闭, 请在设备上停用模块或改用 `service.sh` 的 while 循环控制。
func mExit(c *conn, m Msg) {
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: "agent 正在退出..."})
	go func() {
		time.Sleep(300 * time.Millisecond)
		fmt.Fprintln(os.Stderr, "devctl-agent exiting (self)")
		os.Exit(0)
	}()
}
