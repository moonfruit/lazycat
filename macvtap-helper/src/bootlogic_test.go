package main

import (
	"errors"
	"testing"
)

type fakeActions struct {
	loaded, running, allowed            bool
	loadErr, statusErr, rulesErr        error
	loadCalls, startCalls, restartCalls int
}

func (f *fakeActions) MacvtapLoaded() bool { return f.loaded }
func (f *fakeActions) LoadMacvtap() error {
	f.loadCalls++
	f.loaded = f.loadErr == nil
	return f.loadErr
}
func (f *fakeActions) LightosRunning() (bool, error)      { return f.running, f.statusErr }
func (f *fakeActions) DebianAllowsMacvtap() (bool, error) { return f.allowed, f.rulesErr }
func (f *fakeActions) StartLightos() error                { f.startCalls++; return nil }
func (f *fakeActions) RestartLightos() error              { f.restartCalls++; return nil }

func TestHealthyDebian(t *testing.T) {
	f := &fakeActions{loaded: true, running: true, allowed: true}
	var boot bootRecovery
	if err := boot.check(f); err != nil || f.loadCalls+f.startCalls+f.restartCalls != 0 {
		t.Fatalf("%+v err=%v", f, err)
	}
}

func TestMissingPermissionRestartsOnceAndVerifies(t *testing.T) {
	f := &fakeActions{running: true}
	var boot bootRecovery
	for i := 0; i < 2; i++ {
		if err := boot.check(f); err == nil {
			t.Fatal("reported success before new device rules were ready")
		}
	}
	f.allowed = true
	if err := boot.check(f); err != nil || f.loadCalls != 1 || f.restartCalls != 1 || f.startCalls != 0 {
		t.Fatalf("%+v err=%v", f, err)
	}
}

func TestColdStartOnlyResumes(t *testing.T) {
	f := &fakeActions{}
	var boot bootRecovery
	for i := 0; i < 2; i++ {
		if err := boot.check(f); err == nil {
			t.Fatal("expected to wait for Debian")
		}
	}
	f.running, f.allowed = true, true
	if err := boot.check(f); err != nil || f.loadCalls != 1 || f.startCalls != 1 || f.restartCalls != 0 {
		t.Fatalf("%+v err=%v", f, err)
	}
}

func TestUncertainStateNeverRestarts(t *testing.T) {
	failure := errors.New("not ready")
	for _, f := range []*fakeActions{{loadErr: failure}, {loaded: true, statusErr: failure}, {loaded: true, running: true, rulesErr: failure}} {
		var boot bootRecovery
		if err := boot.check(f); err == nil || f.startCalls+f.restartCalls != 0 {
			t.Fatalf("%+v err=%v", f, err)
		}
	}
}
