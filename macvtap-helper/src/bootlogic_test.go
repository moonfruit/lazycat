package main

import (
	"errors"
	"testing"
)

type fakeActions struct {
	loaded       bool
	running      bool
	loadCalled   int
	restartCalls int
	loadErr      error
	loadMakesIt  bool // LoadMacvtap 后是否让 MacvtapLoaded 变 true
}

func (f *fakeActions) MacvtapLoaded() bool  { return f.loaded }
func (f *fakeActions) LightosRunning() bool { return f.running }
func (f *fakeActions) LoadMacvtap() error {
	f.loadCalled++
	if f.loadErr != nil {
		return f.loadErr
	}
	if f.loadMakesIt {
		f.loaded = true
	}
	return nil
}
func (f *fakeActions) RestartLightos() error { f.restartCalls++; return nil }

// 部署安全：macvtap 已载 + lightos 已运行 → 完全 no-op。
func TestBootHeal_LoadedAndRunning_NoOp(t *testing.T) {
	f := &fakeActions{loaded: true, running: true}
	started, err := BootHeal(f)
	if err != nil || started {
		t.Fatalf("got started=%v err=%v want false,nil", started, err)
	}
	if f.loadCalled != 0 || f.restartCalls != 0 {
		t.Fatalf("should be no-op: load=%d restart=%d", f.loadCalled, f.restartCalls)
	}
}

// 典型冷启动：macvtap 缺失 + lightos 未运行 → 加载 + 启动。
func TestBootHeal_AbsentAndStopped_LoadsAndStarts(t *testing.T) {
	f := &fakeActions{loaded: false, running: false, loadMakesIt: true}
	started, err := BootHeal(f)
	if err != nil || !started {
		t.Fatalf("got started=%v err=%v want true,nil", started, err)
	}
	if f.loadCalled != 1 || f.restartCalls != 1 {
		t.Fatalf("load=%d restart=%d want 1,1", f.loadCalled, f.restartCalls)
	}
}

// 模块已被他处加载、但 lightos 未运行 → 仍要启动 lightos（基于"是否运行"判定，破解竞态）。
func TestBootHeal_LoadedButStopped_Starts(t *testing.T) {
	f := &fakeActions{loaded: true, running: false}
	started, err := BootHeal(f)
	if err != nil || !started {
		t.Fatalf("got started=%v err=%v want true,nil", started, err)
	}
	if f.loadCalled != 0 || f.restartCalls != 1 {
		t.Fatalf("load=%d restart=%d want 0,1", f.loadCalled, f.restartCalls)
	}
}

// macvtap 缺失但 lightos 已运行（异常组合）→ 补加载模块，但不动 lightos。
func TestBootHeal_AbsentButRunning_LoadsNoStart(t *testing.T) {
	f := &fakeActions{loaded: false, running: true, loadMakesIt: true}
	started, err := BootHeal(f)
	if err != nil || started {
		t.Fatalf("got started=%v err=%v want false,nil", started, err)
	}
	if f.loadCalled != 1 || f.restartCalls != 0 {
		t.Fatalf("load=%d restart=%d want 1,0", f.loadCalled, f.restartCalls)
	}
}

// 加载失败 → 报错、不启动 lightos。
func TestBootHeal_LoadFails_NoStart(t *testing.T) {
	f := &fakeActions{loaded: false, running: false, loadErr: errors.New("nope")}
	started, err := BootHeal(f)
	if err == nil || started {
		t.Fatalf("got started=%v err=%v want false,err", started, err)
	}
	if f.restartCalls != 0 {
		t.Fatalf("must not start on load failure")
	}
}

// 加载后仍缺失 → 报错、不启动。
func TestBootHeal_LoadButStillAbsent_NoStart(t *testing.T) {
	f := &fakeActions{loaded: false, running: false, loadMakesIt: false}
	started, err := BootHeal(f)
	if err == nil || started {
		t.Fatalf("want error when still absent after load")
	}
	if f.restartCalls != 0 {
		t.Fatalf("must not start when macvtap still absent")
	}
}
