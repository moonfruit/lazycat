package main

import (
	"strings"
	"testing"
)

func TestDedicatedInterfaces(t *testing.T) {
	const lan = `{"ifname":"eth1","link_index":2,"link_netnsid":0,"linkinfo":{"info_kind":"macvlan","info_data":{"mode":"bridge"}}}`
	const bridge = `{"ifname":"eth0","linkinfo":{"info_kind":"veth"},"addr_info":[{"family":"inet","scope":"global","local":"172.22.0.8","prefixlen":26}]}`
	for _, tc := range []struct {
		name, links string
		ok          bool
	}{
		{"dedicated", lan + "," + bridge, true},
		{"visible parent", strings.Replace(lan, `"link_netnsid":0,`, "", 1) + "," + bridge, false},
		{"wrong mode", strings.Replace(lan, "bridge", "private", 1) + "," + bridge, false},
		{"ambiguous service addresses", lan + "," + bridge + "," + bridge, false},
		{"no dedicated interface", bridge, false},
		{"missing prefix", lan + "," + strings.Replace(bridge, `,"prefixlen":26`, "", 1), false},
		{"different prefix", lan + "," + strings.Replace(bridge, `"prefixlen":26`, `"prefixlen":24`, 1), false},
		{"invalid bridge address", lan + "," + strings.Replace(bridge, "172.22.0.8", "::1", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			link, addr, err := dedicatedInterfaces([]byte("[" + tc.links + "]"))
			if (err == nil) != tc.ok || (tc.ok && (link.Name != "eth1" || link.ParentIndex != 2 || addr.String() != "172.22.0.8/26")) {
				t.Errorf("interface %q, bridge %v, err %v", link.Name, addr, err)
			}
		})
	}
}
