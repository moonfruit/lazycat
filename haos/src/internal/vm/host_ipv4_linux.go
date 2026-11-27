package vm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func parentIPv4(lan ipLink) (netip.Addr, error) {
	if lan.ParentIndex == 0 || lan.ParentNetNS == nil || *lan.ParentNetNS < 0 {
		return netip.Addr{}, errors.New("missing macvlan parent index or namespace")
	}

	// Linux honors IFA_TARGET_NETNSID on an address dump only in strict mode.
	conn, err := netlink.Dial(unix.NETLINK_ROUTE, &netlink.Config{Strict: true})
	if err != nil {
		return netip.Addr{}, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return netip.Addr{}, err
	}
	attrs := netlink.NewAttributeEncoder()
	attrs.Int32(unix.IFA_TARGET_NETNSID, int32(*lan.ParentNetNS))
	filter, err := attrs.Encode()
	if err != nil {
		return netip.Addr{}, err
	}
	header, err := binary.Append(nil, binary.NativeEndian, unix.IfAddrmsg{Family: unix.AF_INET, Index: lan.ParentIndex})
	if err != nil {
		return netip.Addr{}, err
	}
	replies, err := conn.Execute(netlink.Message{
		Header: netlink.Header{Type: unix.RTM_GETADDR, Flags: netlink.Request | netlink.Dump},
		Data:   append(header, filter...),
	})
	if err != nil {
		return netip.Addr{}, err
	}

	var addr netip.Addr
	for _, reply := range replies {
		if reply.Header.Flags&netlink.DumpInterrupted != 0 {
			return netip.Addr{}, errors.New("address dump interrupted")
		}
		if reply.Header.Type != unix.RTM_NEWADDR {
			continue
		}
		ip, err := parentAddress(reply.Data, lan)
		if err != nil {
			return netip.Addr{}, err
		}
		if !ip.IsValid() {
			continue
		}
		if addr.IsValid() && ip != addr {
			return netip.Addr{}, fmt.Errorf("multiple primary IPv4 addresses on parent interface %d", lan.ParentIndex)
		}
		addr = ip
	}
	if !addr.IsValid() {
		return netip.Addr{}, errors.New("no primary IPv4 on the parent interface")
	}
	return addr, nil
}

func parentAddress(data []byte, lan ipLink) (netip.Addr, error) {
	var header unix.IfAddrmsg
	n, err := binary.Decode(data, binary.NativeEndian, &header)
	if err != nil {
		return netip.Addr{}, err
	}
	if header.Family != unix.AF_INET || header.Flags&unix.IFA_F_SECONDARY != 0 ||
		header.Scope != unix.RT_SCOPE_UNIVERSE || header.Index != lan.ParentIndex {
		return netip.Addr{}, nil
	}
	attrs, err := netlink.NewAttributeDecoder(data[n:])
	if err != nil {
		return netip.Addr{}, err
	}
	var ip netip.Addr
	var parentNamespace bool
	for attrs.Next() {
		switch attrs.Type() {
		case unix.IFA_LOCAL:
			ip, _ = netip.AddrFromSlice(attrs.Bytes())
		case unix.IFA_TARGET_NETNSID:
			parentNamespace = attrs.Int32() == int32(*lan.ParentNetNS)
		}
	}
	if err := attrs.Err(); err != nil {
		return netip.Addr{}, err
	}
	if !parentNamespace || !ip.Is4() || !ip.IsGlobalUnicast() {
		return netip.Addr{}, nil
	}
	return ip, nil
}
