//go:build linux

package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/vishvananda/netlink"
)

type RealActions struct {
	mu   sync.Mutex
	Pkgm *Pkgm
}

func (r *RealActions) MacvtapLoaded() bool { return MacvtapLoadedFromProc() }

// LightosRunning 经 7733 instance/status 判断 lightos 实例是否处于运行态（实测码=6）。
// 查询失败按"未运行"处理（保守：宁可尝试启动）。
func (r *RealActions) LightosRunning() bool {
	st, err := r.Pkgm.Status(instanceID)
	if err != nil {
		log.Printf("LightosRunning status check failed: %v", err)
		return false
	}
	return st == statusRunning
}

// LoadMacvtap 在 enp2s0 上建一个临时 macvtap 接口再删除，触发内核加载 macvtap 模块。
// 不需要 CAP_SYS_MODULE：内核在 RTM_NEWLINK(kind=macvtap) 时自动 request_module。
func (r *RealActions) LoadMacvtap() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	parent, err := netlink.LinkByName(parentIf)
	if err != nil {
		return fmt.Errorf("find parent %s: %w", parentIf, err)
	}
	mvt := &netlink.Macvtap{
		Macvlan: netlink.Macvlan{
			LinkAttrs: netlink.LinkAttrs{Name: probeIf, ParentIndex: parent.Attrs().Index},
			Mode:      netlink.MACVLAN_MODE_BRIDGE,
		},
	}
	if old, e := netlink.LinkByName(probeIf); e == nil {
		_ = netlink.LinkDel(old)
	}
	if err := netlink.LinkAdd(mvt); err != nil {
		return fmt.Errorf("add macvtap probe: %w", err)
	}
	if l, e := netlink.LinkByName(probeIf); e == nil {
		_ = netlink.LinkDel(l)
	}
	for i := 0; i < 50; i++ {
		if MacvtapLoadedFromProc() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("macvtap not registered after probe")
}

// RestartLightos 重启 lightos 实例：pause(忽略错误)+resume(忽略400，阻塞至启动完成)。
func (r *RealActions) RestartLightos() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.Pkgm.Pause(instanceID, instanceUID); err != nil {
		log.Printf("pause (ignored): %v", err)
	}
	if err := r.Pkgm.Resume(instanceID, instanceUID); err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	// pkgm 的 instance status 枚举未公开（实测稳定运行态为 6，resume 后曾见瞬时 8），
	// 故不据其判定成败。Resume 已阻塞至实例启动完成（设备白名单在此期间重新快照），
	// best-effort 记录最终状态即可。
	if st, err := r.Pkgm.Status(instanceID); err == nil {
		log.Printf("lightos resumed, instance status=%d", st)
	} else {
		log.Printf("lightos resumed (status check skipped: %v)", err)
	}
	return nil
}

// RunAgent 启动 agent：开机自动逻辑 + unix socket 服务（常驻）。
func RunAgent() {
	acts := &RealActions{Pkgm: NewPkgm()}

	if started, err := BootHeal(acts); err != nil {
		log.Printf("boot BootHeal error: %v", err)
	} else {
		log.Printf("boot BootHeal ok: started=%v", started)
	}

	closer, err := ServeIPC(socketPath, func(req Request) Response {
		switch req.Action {
		case "status":
			st, err := acts.Pkgm.Status(instanceID)
			if err != nil {
				log.Printf("status error: %v", err)
				return Response{OK: false, MacvtapLoaded: acts.MacvtapLoaded(), InstanceStatus: 0, Message: "status error: " + err.Error()}
			}
			return Response{OK: true, MacvtapLoaded: acts.MacvtapLoaded(), InstanceStatus: st, Message: "ok"}
		case "load-macvtap":
			if err := acts.LoadMacvtap(); err != nil {
				return Response{OK: false, MacvtapLoaded: acts.MacvtapLoaded(), Message: err.Error()}
			}
			return Response{OK: true, MacvtapLoaded: true, Message: "macvtap loaded"}
		case "restart-lightos":
			if err := acts.RestartLightos(); err != nil {
				return Response{OK: false, Message: err.Error()}
			}
			return Response{OK: true, Message: "lightos restarted"}
		default:
			return Response{OK: false, Message: "unknown action: " + req.Action}
		}
	})
	if err != nil {
		log.Fatalf("ServeIPC: %v", err)
	}
	defer closer.Close()
	log.Printf("agent listening on %s", socketPath)
	select {}
}
