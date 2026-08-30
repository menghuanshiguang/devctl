//go:build !android

package main

// 非 Android 平台: 无伪装逻辑
func autoHide()  {}
func SetDisguise(name, cmd string) {}
