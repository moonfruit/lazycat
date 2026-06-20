package main

import "fmt"

// Actions 抽象出可被测试替换的副作用。
type Actions interface {
	MacvtapLoaded() bool
	LoadMacvtap() error
	LightosRunning() bool
	RestartLightos() error
}

// BootHeal 实现开机自愈逻辑（前提：lightos/debian 实例已设为"非开机自启"）：
//
//	冷启动时 debian 处于停止态——其内的 haos.service 也不运行，故无人抢先加载
//	macvtap，不存在竞态。本函数：确保 macvtap 模块已加载，然后在 lightos 未运行时
//	启动它；此时实例创建会对 /proc/devices 快照，因 macvtap 已加载故白名单含 238。
//	若 lightos 已在运行（例如重新部署 helper 时），则不动它（部署安全，不打断 HAOS）。
//
// 决策依据是"lightos 是否运行"（而非"模块是否已加载"），从而即便他处已加载模块也能正确判定。
// 返回 started 表示是否启动了 lightos。
func BootHeal(a Actions) (started bool, err error) {
	if !a.MacvtapLoaded() {
		if err := a.LoadMacvtap(); err != nil {
			return false, fmt.Errorf("load macvtap: %w", err)
		}
		if !a.MacvtapLoaded() {
			return false, fmt.Errorf("macvtap still not present after load attempt")
		}
	}
	if a.LightosRunning() {
		return false, nil // 已运行，no-op（部署安全）
	}
	if err := a.RestartLightos(); err != nil {
		return false, fmt.Errorf("start lightos: %w", err)
	}
	return true, nil
}
