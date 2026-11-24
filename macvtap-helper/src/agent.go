//go:build linux

package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/vishvananda/netlink"
)

type RealActions struct {
	mu   sync.Mutex
	Pkgm *Pkgm
}

func (r *RealActions) MacvtapLoaded() bool {
	_, err := hostMacvtapMajor()
	return err == nil
}

// LightosRunning 经 7733 instance/status 判断 lightos 实例是否处于运行态（实测码=6）。
func (r *RealActions) LightosRunning() (bool, error) {
	st, err := r.Pkgm.Status(instanceID)
	return st == statusRunning, err
}

func (r *RealActions) DebianAllowsMacvtap() (bool, error) {
	major, err := hostMacvtapMajor()
	if err != nil {
		return false, err
	}
	f, err := os.Open("/run/lightos-runc/" + debianID + "/state.json")
	if err != nil {
		return false, err
	}
	defer f.Close()
	return debianAllowsMacvtap(f, major)
}

func (r *RealActions) StartLightos() error {
	log.Print("requesting LightOS resume")
	return r.Pkgm.Resume(instanceID, instanceUID)
}

// LoadMacvtap 在 enp2s0 上建一个临时 macvtap 接口再删除，触发内核加载 macvtap 模块。
// 不需要 CAP_SYS_MODULE：内核在 RTM_NEWLINK(kind=macvtap) 时自动 request_module。
func (r *RealActions) LoadMacvtap() error {
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
		if r.MacvtapLoaded() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("macvtap not registered after probe")
}

// RestartLightos 重启 lightos 实例：pause(忽略错误)+resume(忽略400，阻塞至启动完成)。
func (r *RealActions) RestartLightos() error {
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

	closer, err := ServeIPC(socketPath, func(req Request) Response {
		switch req.Action {
		case "status":
			st, err := acts.Pkgm.Status(instanceID)
			if err != nil {
				log.Printf("status error: %v", err)
				return Response{OK: false, MacvtapLoaded: acts.MacvtapLoaded(), InstanceStatus: 0, Message: "status error: " + err.Error()}
			}
			resp := Response{OK: true, MacvtapLoaded: acts.MacvtapLoaded(), InstanceStatus: st, Message: "等待 LightOS 和 macvtap 就绪"}
			if st == statusRunning && resp.MacvtapLoaded {
				allowed, err := acts.DebianAllowsMacvtap()
				switch {
				case err != nil:
					resp.Message = "暂时无法读取 Debian 设备规则：" + err.Error()
				case allowed:
					resp.Message = "Debian 已允许读写 macvtap"
				default:
					resp.Message = "Debian 缺少 macvtap 读写权限"
				}
			}
			return resp
		case "load-macvtap":
			acts.mu.Lock()
			defer acts.mu.Unlock()
			if err := acts.LoadMacvtap(); err != nil {
				return Response{OK: false, MacvtapLoaded: acts.MacvtapLoaded(), Message: err.Error()}
			}
			return Response{OK: true, MacvtapLoaded: true, Message: "macvtap loaded"}
		case "restart-lightos":
			acts.mu.Lock()
			defer acts.mu.Unlock()
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

	// 开机自愈放后台：冷启动时 BootHeal 会重启 lightos（可能耗时数十秒）。
	// 先开 socket 让应用快速就绪（healthcheck 通过 → "运行中"），再后台自愈，
	// 避免应用长时间卡在"启动中"。
	go func() {
		var boot bootRecovery
		deadline := time.Now().Add(5 * time.Minute)
		var previous string
		for {
			acts.mu.Lock()
			err := boot.check(acts)
			acts.mu.Unlock()
			if err == nil {
				log.Printf("Debian allows macvtap read/write (resume requested=%t, restart requested=%t)", boot.startRequested, boot.restartRequested)
				return
			}
			if time.Now().After(deadline) {
				log.Printf("boot recovery timed out: %v", err)
				return
			}
			if err.Error() != previous {
				log.Printf("boot recovery: %v", err)
				previous = err.Error()
			}
			time.Sleep(5 * time.Second)
		}
	}()

	select {}
}
