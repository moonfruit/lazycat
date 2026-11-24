package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// runc serializes device types as their numeric rune values ('a', 'b', 'c').
type deviceRule struct {
	Type        rune
	Major       *int
	Minor       *int
	Permissions string
	Allow       *bool
}

func debianAllowsMacvtap(state io.Reader, major int) (bool, error) {
	var snapshot struct {
		ID      string
		InitPID int `json:"init_process_pid"`
		Config  struct {
			Cgroups struct{ Devices []deviceRule }
		}
	}
	if err := json.NewDecoder(state).Decode(&snapshot); err != nil {
		return false, err
	}
	if snapshot.ID != debianID || snapshot.InitPID <= 0 {
		return false, fmt.Errorf("missing live Debian instance identity")
	}
	var readable, writable, defaultKnown bool
	for _, rule := range snapshot.Config.Cgroups.Devices {
		if rule.Major == nil || rule.Minor == nil || rule.Allow == nil ||
			!strings.ContainsRune("abc", rule.Type) || rule.Permissions == "" ||
			strings.Trim(rule.Permissions, "rwm") != "" {
			return false, fmt.Errorf("unrecognized runc device rule")
		}
		if rule.Type == 'b' || (*rule.Major != -1 && *rule.Major != major) ||
			!strings.ContainsAny(rule.Permissions, "rw") {
			continue
		}
		if *rule.Minor != -1 {
			return false, fmt.Errorf("macvtap permissions vary by minor device number")
		}
		if rule.Type == 'a' && *rule.Major == -1 && strings.Contains(rule.Permissions, "r") && strings.Contains(rule.Permissions, "w") {
			defaultKnown = true
		}
		if strings.Contains(rule.Permissions, "r") {
			readable = *rule.Allow
		}
		if strings.Contains(rule.Permissions, "w") {
			writable = *rule.Allow
		}
	}
	if !defaultKnown {
		return false, fmt.Errorf("missing default runc device policy")
	}
	return readable && writable, nil
}
