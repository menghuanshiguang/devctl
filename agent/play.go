//go:build android

package main

import (
	"fmt"
	"os/exec"
)

// play <path>: 设置音频路由并播放 wav (48kHz stereo 16bit)
// 封装: tinymix 设 RX MUX + tinyplay 异步播放
func mPlay(c *conn, m Msg) {
	if len(m.Args) < 1 {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: "usage: play <wav路径>"})
		return
	}
	// 设置路由: RX0 输入 = AIF1_PB (tinyplay 直出需要)
	if _, _, se := runCmd("tinymix", "'RX_MACRO RX0 MUX'", "AIF1_PB"); se != "" {
		// 忽略路由设置失败, 继续尝试播放
	}
	// 异步播放
	cmd := exec.Command("tinyplay", m.Args[0])
	if err := cmd.Start(); err != nil {
		c.send(Msg{T: "res", ID: m.ID, Ok: boolp(false), Stderr: fmt.Sprintf("播放失败: %v", err)})
		return
	}
	go cmd.Wait() // 后台回收
	c.send(Msg{T: "res", ID: m.ID, Ok: boolp(true), Stdout: fmt.Sprintf("正在播放: %s", m.Args[0])})
}
