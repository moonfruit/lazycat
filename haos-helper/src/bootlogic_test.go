package main

import (
	"errors"
	"testing"
)

type fakeActions struct {
	loaded       bool
	loadCalled   int
	restartCalls int
	loadErr      error
	loadMakesIt  bool // LoadMacvtap 后是否让 MacvtapLoaded 变 true
}

func (f *fakeActions) MacvtapLoaded() bool { return f.loaded }
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

func TestEnsureMacvtap_AlreadyLoaded_NoOp(t *testing.T) {
	f := &fakeActions{loaded: true}
	restarted, err := EnsureMacvtap(f)
	if err != nil || restarted {
		t.Fatalf("got restarted=%v err=%v want false,nil", restarted, err)
	}
	if f.loadCalled != 0 || f.restartCalls != 0 {
		t.Fatalf("should be no-op: load=%d restart=%d", f.loadCalled, f.restartCalls)
	}
}

func TestEnsureMacvtap_Absent_LoadsAndRestarts(t *testing.T) {
	f := &fakeActions{loaded: false, loadMakesIt: true}
	restarted, err := EnsureMacvtap(f)
	if err != nil || !restarted {
		t.Fatalf("got restarted=%v err=%v want true,nil", restarted, err)
	}
	if f.loadCalled != 1 || f.restartCalls != 1 {
		t.Fatalf("load=%d restart=%d want 1,1", f.loadCalled, f.restartCalls)
	}
}

func TestEnsureMacvtap_LoadFails_NoRestart(t *testing.T) {
	f := &fakeActions{loaded: false, loadErr: errors.New("nope")}
	restarted, err := EnsureMacvtap(f)
	if err == nil || restarted {
		t.Fatalf("got restarted=%v err=%v want false,err", restarted, err)
	}
	if f.restartCalls != 0 {
		t.Fatalf("must not restart on load failure")
	}
}

func TestEnsureMacvtap_LoadButStillAbsent_NoRestart(t *testing.T) {
	f := &fakeActions{loaded: false, loadMakesIt: false}
	restarted, err := EnsureMacvtap(f)
	if err == nil || restarted {
		t.Fatalf("want error when still absent after load")
	}
	if f.restartCalls != 0 {
		t.Fatalf("must not restart when macvtap still absent")
	}
}
