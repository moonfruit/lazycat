package cli

import (
	"slices"
	"testing"
)

func TestCommandDispatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		want   []string
		status int
	}{
		{"default VM", []string{}, []string{"vm"}, 42},
		{"explicit VM", []string{"vm"}, []string{"vm"}, 42},
		{"provision", []string{"oidc"}, []string{"provision"}, 0},
		{"recover", []string{"oidc", "--recover"}, []string{"recover"}, 0},
		{"watch", []string{"oidc", "--watch"}, []string{"watch"}, 0},
		{"conflicting modes", []string{"oidc", "--watch", "--recover"}, nil, 1},
		{"unknown command", []string{"typo"}, nil, 1},
		{"VM argument", []string{"vm", "extra"}, nil, 1},
		{"VM recovery flag", []string{"vm", "--recover"}, nil, 1},
		{"OIDC argument", []string{"oidc", "extra"}, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			start := func() (int, error) {
				calls = append(calls, "vm")
				return 42, nil
			}
			provision := func(recoverOnly bool) error {
				if recoverOnly {
					calls = append(calls, "recover")
				} else {
					calls = append(calls, "provision")
				}
				return nil
			}
			status, err := Execute(tc.args, start, provision, func() error {
				calls = append(calls, "watch")
				return nil
			})
			if status != tc.status || (err != nil) != (tc.want == nil) || !slices.Equal(calls, tc.want) {
				t.Fatalf("status=%d err=%v calls=%v", status, err, calls)
			}
		})
	}
}
