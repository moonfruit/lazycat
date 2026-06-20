package main

import "fmt"

// Actions 抽象出可被测试替换的副作用。
type Actions interface {
	MacvtapLoaded() bool
	LoadMacvtap() error
	RestartLightos() error
}

// EnsureMacvtap 实现修正后的开机逻辑：
//   - macvtap 已加载 → no-op（部署安全：不打断正在跑的 HAOS）
//   - 未加载 → 加载 → 复查 → 仍无则报错（不重启）→ 有则重启 lightos
//
// 返回 restarted 表示是否触发了 lightos 重启。
func EnsureMacvtap(a Actions) (restarted bool, err error) {
	if a.MacvtapLoaded() {
		return false, nil
	}
	if err := a.LoadMacvtap(); err != nil {
		return false, fmt.Errorf("load macvtap: %w", err)
	}
	if !a.MacvtapLoaded() {
		return false, fmt.Errorf("macvtap still not present after load attempt")
	}
	if err := a.RestartLightos(); err != nil {
		return false, fmt.Errorf("restart lightos: %w", err)
	}
	return true, nil
}
