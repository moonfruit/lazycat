package vm

import (
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
)

func TestGuestRoutesAndBackup(t *testing.T) {
	for _, tc := range []struct{ bridge, subnet, host, guest, route string }{
		{"172.28.11.130/26", "172.28.11.128/26", "192.168.1.1", "10.0.2.15", "route2=192.168.1.1/32,10.0.2.2"},
		{"172.28.11.194/26", "172.28.11.192/26", "10.0.2.50", "172.31.249.15", "route2=10.0.2.50/32,172.31.249.2"},
		{"172.28.11.66/26", "172.28.11.64/26", "", "10.0.2.15", ""},
	} {
		t.Run(tc.bridge, func(t *testing.T) {
			c := vmConfig{bridge: netip.MustParsePrefix(tc.bridge)}
			if tc.host != "" {
				c.host = netip.MustParseAddr(tc.host)
			}
			args, err := c.qemuArgs()
			if err != nil {
				t.Fatal(err)
			}
			line := strings.Join(args, " ")
			for _, want := range []string{"path=/proc/self/fd/3,", "hostfwd=tcp:" + c.bridge.Addr().String() + ":8123-" + tc.guest + ":80"} {
				if !strings.Contains(line, want) {
					t.Errorf("missing %s", want)
				}
			}
			credentials := map[string]string{}
			for _, arg := range args {
				if value, ok := strings.CutPrefix(arg, "type=11,value=io.systemd.credential.binary:"); ok {
					name, data, _ := strings.Cut(value, "=")
					decoded, err := base64.StdEncoding.DecodeString(data)
					if err != nil {
						t.Fatal(err)
					}
					credentials[name] = string(decoded)
				}
			}
			if !strings.Contains(credentials["fstab.extra"], "haosbackup /mnt/data/supervisor/backup 9p") {
				t.Fatal("missing Supervisor backup mount")
			}
			if !strings.Contains(credentials["systemd.unit-dropin.docker.service~haos-lpk"], "ExecStartPre=/bin/sh /sys/firmware/qemu_fw_cfg/by_name/opt/haos-lpk/prepare/raw ") ||
				!strings.Contains(line, "name=opt/haos-lpk/prepare,file=/lzcapp/pkg/content/prepare.sh") {
				t.Error("missing first-start provisioning before Docker")
			}
			if !strings.Contains(credentials["systemd.unit-dropin.docker.service~haos-lpk"], "ExecStartPre=-/usr/sbin/nft -f /sys/firmware/qemu_fw_cfg/by_name/opt/haos-lpk/lan-http/raw\n") ||
				!strings.Contains(line, "name=opt/haos-lpk/lan-http,file=/lzcapp/pkg/content/lan-http.nft") {
				t.Error("missing guest LAN port forwarding rules at Docker startup")
			}
			for name, content := range credentials {
				if strings.Contains(name, "oidc") || strings.Contains(content, "haos-lpk-oidc") || strings.Contains(content, "/run/haos-oidc") {
					t.Fatalf("unexpected OIDC unit or dependency with OIDC disabled: %s", name)
				}
			}
			network, httpFile, _ := strings.Cut(strings.TrimSpace(credentials["tmpfiles.extra"]), "\n")
			seed := strings.Fields(httpFile)
			if len(seed) != 7 {
				t.Errorf("missing HTTP settings for %s", tc.guest)
			} else {
				payload, err := base64.StdEncoding.DecodeString(seed[6])
				var http struct {
					Data struct {
						Stable struct {
							Port      int      `json:"server_port"`
							Forwarded bool     `json:"use_x_forwarded_for"`
							Proxies   []string `json:"trusted_proxies"`
						}
						Pending json.RawMessage
					}
				}
				if err != nil || json.Unmarshal(payload, &http) != nil || !http.Data.Stable.Forwarded || http.Data.Stable.Port != 80 ||
					strings.Join(http.Data.Stable.Proxies, ",") != tc.subnet || string(http.Data.Pending) != "null" {
					t.Fatalf("bad initial HTTP config: %s", payload)
				}
			}
			fields := strings.Fields(network)
			if len(fields) != 7 {
				t.Fatalf("tmpfiles fields: %q", fields)
			}
			raw, err := base64.StdEncoding.DecodeString(fields[6])
			if err != nil {
				t.Fatal(err)
			}
			profile := string(raw)
			for _, want := range []string{"interface-name=enp0s2", "never-default=true", "route1=" + tc.subnet + "," + strings.TrimSuffix(tc.guest, "15") + "2", "address1=" + tc.guest + "/24", tc.route} {
				if !strings.Contains(profile, want+"\n") && want != "" {
					t.Errorf("missing %q in %q", want, profile)
				}
			}
			if tc.route == "" && strings.Contains(profile, "route2=") {
				t.Fatalf("unexpected route: %q", profile)
			}
		})
	}
}

func TestFirmwareWithAllDocumentMounts(t *testing.T) {
	c := vmConfig{host: netip.MustParseAddr("10.255.255.254"), bridge: netip.MustParsePrefix("192.168.255.194/26")}
	// HAOS reads the SMBIOS OEM strings through a 4 KiB sysfs buffer.
	size := 8 // type 11 header, count and terminators
	args, err := c.qemuArgs()
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "type=11,value="); ok {
			size += len(value) + 1
		}
	}
	if size > 4095 {
		t.Fatalf("OEM strings need %d bytes with all shares selected", size)
	}
}
