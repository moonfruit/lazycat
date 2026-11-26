package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type ipLink struct {
	Name        string `json:"ifname"`
	ParentIndex uint32 `json:"link_index"`
	ParentNetNS *int   `json:"link_netnsid"`
	Info        struct {
		Kind string `json:"info_kind"`
		Data struct {
			Mode string `json:"mode"`
		} `json:"info_data"`
	} `json:"linkinfo"`
	Addresses []struct {
		Family, Scope, Local string
		PrefixLen            int `json:"prefixlen"`
	} `json:"addr_info"`
}

var interfaceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,14}$`)

func dedicatedInterfaces(data []byte) (ipLink, netip.Prefix, error) {
	var links []ipLink
	var bridge netip.Prefix
	if err := json.Unmarshal(data, &links); err != nil {
		return ipLink{}, bridge, err
	}
	var lan ipLink
	for _, link := range links {
		switch link.Info.Kind {
		case "macvlan":
			if lan.Name != "" || link.ParentNetNS == nil || link.Info.Data.Mode != "bridge" || !interfaceName.MatchString(link.Name) {
				return ipLink{}, bridge, errors.New("expected one dedicated bridge-mode macvlan with a parent outside the service namespace")
			}
			lan = link
		case "veth":
			for _, address := range link.Addresses {
				if address.Family != "inet" || address.Scope != "global" {
					continue
				}
				ip, err := netip.ParseAddr(address.Local)
				if err != nil || !ip.Is4() || bridge.IsValid() {
					return ipLink{}, bridge, errors.New("expected one service bridge IPv4 address")
				}
				if address.PrefixLen != 26 {
					return ipLink{}, bridge, fmt.Errorf("expected a /26 application network on %s, got %s/%d", link.Name, ip, address.PrefixLen)
				}
				bridge = netip.PrefixFrom(ip, address.PrefixLen)
			}
		}
	}
	if lan.Name == "" || !bridge.IsValid() {
		return ipLink{}, bridge, errors.New("expected one dedicated macvlan and one service bridge IPv4 address")
	}
	return lan, bridge, nil
}

func networkInterfaces(ctx context.Context) (ipLink, netip.Prefix, error) {
	cmd := exec.CommandContext(ctx, "ip", "-d", "-j", "address", "show")
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return ipLink{}, netip.Prefix{}, fmt.Errorf("read network interfaces: %w", err)
	}
	return dedicatedInterfaces(data)
}

func configureLAN(ctx context.Context, name, mac string) error {
	ip := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "ip", args...)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ip %s: %w", strings.Join(args, " "), err)
		}
		return nil
	}
	if err := ip("link", "set", "dev", name, "down"); err != nil {
		return err
	}
	if err := ip("address", "flush", "dev", name); err != nil {
		return err
	}
	dir := filepath.Join("/proc/sys/net/ipv6/conf", name)
	disabled := filepath.Join(dir, "disable_ipv6")
	value, err := os.ReadFile(disabled)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && strings.TrimSpace(string(value)) != "1" {
		if err := os.WriteFile(disabled, []byte("1\n"), 0); err != nil {
			// Docker may mount sysctls read-only. Suppress link-local addresses
			// only if router advertisements cannot install global addresses.
			sysctl := func(key string) string {
				b, err := os.ReadFile(filepath.Join(dir, key))
				if err != nil {
					return ""
				}
				return strings.TrimSpace(string(b))
			}
			ra := sysctl("accept_ra")
			if ra != "0" && sysctl("autoconf") != "0" && !(ra == "1" && sysctl("forwarding") == "1") {
				return fmt.Errorf("set the Docker endpoint sysctl net.ipv6.conf.IFNAME.disable_ipv6=1: %w", err)
			}
			log.Printf("IPv6 router autoconfiguration is already inactive on %s", name)
		}
		if err := ip("link", "set", "dev", name, "addrgenmode", "none"); err != nil {
			return err
		}
	}
	if err := ip("link", "set", "dev", name, "address", mac); err != nil {
		return err
	}
	return ip("link", "set", "dev", name, "up")
}
