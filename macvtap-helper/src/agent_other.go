//go:build !linux

package main

import "log"

// RunAgent 在非 linux 平台不可用（agent 依赖 netlink/macvtap）。
// 仅为让包在开发机(darwin)上可编译、跑纯逻辑单测。
func RunAgent() {
	log.Fatal("agent mode requires linux")
}
