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

// RestartLightos 重启 lightos 实例：pause(忽略错误)+resume(忽略400)+轮询至运行态。
func (r *RealActions) RestartLightos() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.Pkgm.Pause(instanceID, instanceUID); err != nil {
		log.Printf("pause (ignored): %v", err)
	}
	if err := r.Pkgm.Resume(instanceID, instanceUID); err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := r.Pkgm.Status(instanceID); err == nil && st == statusRunning {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("lightos did not reach running state within timeout")
}

// RunAgent 启动 agent：开机自动逻辑 + unix socket 服务（常驻）。
func RunAgent() {
	acts := &RealActions{Pkgm: NewPkgm()}

	if restarted, err := EnsureMacvtap(acts); err != nil {
		log.Printf("boot EnsureMacvtap error: %v", err)
	} else {
		log.Printf("boot EnsureMacvtap ok: restarted=%v", restarted)
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
