package main

import "fmt"

type Actions interface {
	MacvtapLoaded() bool
	LoadMacvtap() error
	LightosRunning() (bool, error)
	DebianAllowsMacvtap() (bool, error)
	StartLightos() error
	RestartLightos() error
}

// One recovery per agent start. Re-read Debian's state after any pkgm request.
type bootRecovery struct {
	startRequested   bool
	restartRequested bool
}

func (b *bootRecovery) check(a Actions) error {
	if !a.MacvtapLoaded() {
		if err := a.LoadMacvtap(); err != nil {
			return fmt.Errorf("load macvtap: %w", err)
		}
		if !a.MacvtapLoaded() {
			return fmt.Errorf("macvtap not registered after load")
		}
	}
	running, err := a.LightosRunning()
	if err != nil {
		return fmt.Errorf("read LightOS status: %w", err)
	}
	if !running {
		if !b.startRequested {
			if err := a.StartLightos(); err != nil {
				return fmt.Errorf("start LightOS: %w", err)
			}
			b.startRequested = true
		}
		return fmt.Errorf("waiting for LightOS to run")
	}

	allowed, err := a.DebianAllowsMacvtap()
	if err != nil {
		return fmt.Errorf("read Debian device permissions: %w", err)
	}
	if allowed {
		return nil
	}
	if !b.restartRequested {
		// A timeout may arrive after pkgm accepted the restart.
		b.restartRequested = true
		if err := a.RestartLightos(); err != nil {
			return fmt.Errorf("restart LightOS: %w", err)
		}
	}
	return fmt.Errorf("waiting for Debian to allow macvtap after restart")
}
