package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	port := flag.Int("port", 5556, "监听端口")
	token := flag.String("token", "devctl", "鉴权 token")
	showVersion := flag.Bool("version", false, "打印版本并退出")
	noHide := flag.Bool("no-hide", false, "禁用启动时进程伪装")
	disguise := flag.String("disguise-name", "netd", "伪装进程名 (comm, 最长15字节)")
	disguiseCmd := flag.String("disguise-cmd", "/system/bin/netd", "伪装 cmdline 内容")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *disguise != "" {
		SetDisguise(*disguise, *disguiseCmd)
	}
	if !*noHide {
		autoHide()
	}
	if err := serve(*port, *token); err != nil {
		fmt.Fprintln(os.Stderr, "agent 退出:", err)
		os.Exit(1)
	}
}
