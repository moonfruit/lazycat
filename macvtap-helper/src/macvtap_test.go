package main

import "testing"

func TestMacvtapLoaded(t *testing.T) {
	withMacvtap := "Character devices:\n  1 mem\n 10 misc\n238 macvtap\n239 aux\n"
	withoutMacvtap := "Character devices:\n  1 mem\n 10 misc\n239 aux\n"
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"present", withMacvtap, true},
		{"absent", withoutMacvtap, false},
		{"empty", "", false},
		// 不能被 "macvtap" 子串误伤其它名字
		{"substring-safe", "Character devices:\n 60 macvtapx\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MacvtapLoaded(c.in); got != c.want {
				t.Fatalf("MacvtapLoaded(%q)=%v want %v", c.name, got, c.want)
			}
		})
	}
}
