package vm

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParentAddress(t *testing.T) {
	ns := 0
	link := ipLink{ParentIndex: 2, ParentNetNS: &ns}
	for _, tc := range []struct {
		name         string
		index, nsid  uint32
		flags, scope byte
		want         bool
	}{
		{"physical parent", 2, 0, 0, 0, true},
		{"another interface", 3, 0, 0, 0, false},
		{"another namespace", 2, 1, 0, 0, false},
		{"secondary address", 2, 0, unix.IFA_F_SECONDARY, 0, false},
		{"link local scope", 2, 0, 0, unix.RT_SCOPE_LINK, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, unix.SizeofIfAddrmsg+16)
			data[0], data[2], data[3] = unix.AF_INET, tc.flags, tc.scope
			order := binary.NativeEndian
			order.PutUint32(data[4:], tc.index)
			order.PutUint16(data[8:], 8)
			order.PutUint16(data[10:], unix.IFA_TARGET_NETNSID)
			order.PutUint32(data[12:], tc.nsid)
			order.PutUint16(data[16:], 8)
			order.PutUint16(data[18:], unix.IFA_LOCAL)
			copy(data[20:], []byte{192, 168, 50, 11})
			ip, err := parentAddress(data, link)
			if err != nil || ip.IsValid() != tc.want || (tc.want && ip.String() != "192.168.50.11") {
				t.Fatalf("got %s, %v", ip, err)
			}
		})
	}
}
