package main

import (
	"bufio"
	"os"
	"strings"
)

// MacvtapLoaded 解析 /proc/devices 文本，判断字符设备表里是否存在名为 "macvtap" 的设备
// （即 macvtap 内核模块已加载、major 已注册）。
func MacvtapLoaded(procDevices string) bool {
	sc := bufio.NewScanner(strings.NewReader(procDevices))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		// 形如 "238 macvtap"：两列，第二列精确等于 macvtap
		if len(fields) == 2 && fields[1] == "macvtap" {
			return true
		}
	}
	return false
}

// MacvtapLoadedFromProc 读取宿主 /proc/devices 后判断。
func MacvtapLoadedFromProc() bool {
	b, err := os.ReadFile("/proc/devices")
	if err != nil {
		return false
	}
	return MacvtapLoaded(string(b))
}
