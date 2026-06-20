package main

const (
	parentIf      = "enp2s0"
	instanceID    = "cloud.lazycat.lightos.entry"
	instanceUID   = "moon"
	socketPath    = "/lzcapp/var/ipc/agent.sock"
	probeIf       = "lzc-mvprobe"
	// statusRunning：7733 instance/status 的"运行态"码（实测=6，非公开 SDK 枚举）。
	// 仅用于 BootHeal 的"已运行则跳过启动"部署安全判定；判错最坏只是 redeploy 多启一次，
	// 不影响冷启动关键路径（停止态 != 6 → 启动）。
	statusRunning = 6
)
