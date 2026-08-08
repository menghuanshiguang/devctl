package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	port := flag.Int("port", 5556, "监听端口")
	token := flag.String("token", "devctl", "鉴权 token")
	flag.Parse()
	if err := serve(*port, *token); err != nil {
		fmt.Fprintln(os.Stderr, "agent 退出:", err)
		os.Exit(1)
	}
}
