//go:build linux

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"syscall"
)

// IFA_TARGET_NETNSID selects the namespace that owns the other end of a
// macvlan. Linux parses this dump filter only when strict checking is on.
const ifaTargetNetnsid = 10

func parentIPv4(lan ipLink) (netip.Addr, error) {
	if lan.ParentIndex == 0 || lan.ParentNetNS == nil || *lan.ParentNetNS < 0 {
		return netip.Addr{}, errors.New("missing macvlan parent index or namespace")
	}
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_ROUTE)
	if err != nil {
		return netip.Addr{}, err
	}
	defer syscall.Close(fd)
	const solNetlink, netlinkGetStrictChk = 270, 12
	if err := syscall.SetsockoptInt(fd, solNetlink, netlinkGetStrictChk, 1); err != nil {
		return netip.Addr{}, err
	}
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &syscall.Timeval{Sec: 2}); err != nil {
		return netip.Addr{}, err
	}
	order := binary.NativeEndian
	request := make([]byte, syscall.NLMSG_HDRLEN+syscall.SizeofIfAddrmsg+8)
	order.PutUint32(request, uint32(len(request)))
	order.PutUint16(request[4:], syscall.RTM_GETADDR)
	order.PutUint16(request[6:], syscall.NLM_F_REQUEST|syscall.NLM_F_DUMP)
	order.PutUint32(request[8:], 1)
	body := request[syscall.NLMSG_HDRLEN:]
	body[0] = syscall.AF_INET
	order.PutUint32(body[4:], lan.ParentIndex)
	attr := body[syscall.SizeofIfAddrmsg:]
	order.PutUint16(attr, uint16(len(attr)))
	order.PutUint16(attr[2:], ifaTargetNetnsid)
	order.PutUint32(attr[4:], uint32(*lan.ParentNetNS))
	if err := syscall.Sendto(fd, request, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return netip.Addr{}, err
	}

	var addr netip.Addr
	buf := make([]byte, 32768)
	for {
		n, from, err := syscall.Recvfrom(fd, buf, 0)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return netip.Addr{}, err
		}
		peer, ok := from.(*syscall.SockaddrNetlink)
		if !ok || peer.Pid != 0 {
			continue
		}
		messages, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return netip.Addr{}, err
		}
		for _, msg := range messages {
			if msg.Header.Seq != 1 {
				continue
			}
			const nlmDumpIntr = 0x10
			if msg.Header.Flags&nlmDumpIntr != 0 {
				return netip.Addr{}, errors.New("address dump interrupted")
			}
			switch msg.Header.Type {
			case syscall.NLMSG_ERROR, syscall.NLMSG_DONE:
				if len(msg.Data) < 4 {
					return netip.Addr{}, errors.New("missing netlink result")
				}
				if status := int32(order.Uint32(msg.Data)); status < 0 {
					return netip.Addr{}, syscall.Errno(-status)
				}
				if msg.Header.Type == syscall.NLMSG_DONE {
					if !addr.IsValid() {
						return netip.Addr{}, errors.New("no primary IPv4 on the parent interface")
					}
					return addr, nil
				}
			case syscall.RTM_NEWADDR:
				ip, err := parentAddress(msg, lan)
				if err != nil {
					return netip.Addr{}, err
				}
				if ip.IsValid() {
					if addr.IsValid() && addr != ip {
						return netip.Addr{}, fmt.Errorf("multiple primary IPv4 addresses on parent interface %d", lan.ParentIndex)
					}
					addr = ip
				}
			}
		}
	}
}

func parentAddress(msg syscall.NetlinkMessage, lan ipLink) (netip.Addr, error) {
	data := msg.Data
	if len(data) < syscall.SizeofIfAddrmsg {
		return netip.Addr{}, errors.New("truncated address message")
	}
	if data[0] != syscall.AF_INET || data[2]&syscall.IFA_F_SECONDARY != 0 || data[3] != syscall.RT_SCOPE_UNIVERSE ||
		binary.NativeEndian.Uint32(data[4:]) != lan.ParentIndex {
		return netip.Addr{}, nil
	}
	attrs, err := syscall.ParseNetlinkRouteAttr(&msg)
	if err != nil {
		return netip.Addr{}, err
	}
	var ip netip.Addr
	var parentNamespace bool
	for _, attr := range attrs {
		if len(attr.Value) != 4 {
			continue
		}
		switch attr.Attr.Type {
		case syscall.IFA_LOCAL:
			ip = netip.AddrFrom4([4]byte(attr.Value))
		case ifaTargetNetnsid:
			parentNamespace = binary.NativeEndian.Uint32(attr.Value) == uint32(*lan.ParentNetNS)
		}
	}
	if !parentNamespace || !ip.IsGlobalUnicast() {
		return netip.Addr{}, nil
	}
	return ip, nil
}
