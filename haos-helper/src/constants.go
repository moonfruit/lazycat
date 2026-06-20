package main

const (
	parentIf      = "enp2s0"
	instanceID    = "cloud.lazycat.lightos.entry"
	instanceUID   = "moon"
	socketPath    = "/lzcapp/var/ipc/agent.sock"
	probeIf       = "lzc-mvprobe"
	statusRunning = 8 // 实测观测值：pkgm instance/status 运行态=8（未公开，随版本或变）
)
