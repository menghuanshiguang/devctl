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
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if err := serve(*port, *token); err != nil {
		fmt.Fprintln(os.Stderr, "agent 退出:", err)
		os.Exit(1)
	}
}
