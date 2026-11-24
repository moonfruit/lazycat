package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestDebianDevicePermissions(t *testing.T) {
	denyAll := `{"type":97,"major":-1,"minor":-1,"permissions":"rwm","allow":false}`
	allowTap := `{"type":99,"major":237,"minor":-1,"permissions":"rwm","allow":true}`
	other := `{"type":99,"major":238,"minor":-1,"permissions":"rwm","allow":true}`
	mknod := `{"type":99,"major":-1,"minor":-1,"permissions":"m","allow":true}`
	for _, tc := range []struct {
		name             string
		rules            []string
		allowed, unknown bool
	}{
		{"new snapshot", []string{denyAll, allowTap, mknod}, true, false},
		{"before module registration", []string{denyAll, other, mknod}, false, false},
		{"read only", []string{denyAll, strings.Replace(allowTap, "rwm", "r", 1)}, false, false},
		{"missing default", []string{allowTap}, false, true},
		{"unknown rule", []string{denyAll, "{}"}, false, true},
		{"minor specific", []string{denyAll, strings.Replace(allowTap, `"minor":-1`, `"minor":2`, 1)}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"id":%q,"init_process_pid":7,"config":{"cgroups":{"devices":[%s]}}}`, debianID, strings.Join(tc.rules, ","))
			got, err := debianAllowsMacvtap(strings.NewReader(raw), 237)
			if got != tc.allowed || (err != nil) != tc.unknown {
				t.Fatalf("allowed=%t err=%v", got, err)
			}
		})
	}
	for _, raw := range []string{"{", "{}", `{"id":"different-instance","init_process_pid":7}`} {
		if _, err := debianAllowsMacvtap(strings.NewReader(raw), 237); err == nil {
			t.Errorf("unexpectedly accepted %q", raw)
		}
	}
}
