package main

import (
	"embed"
	"encoding/base64"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const (
	stateDir     = "/lzcapp/var/haos"
	runDir       = "/run/haos"
	qmpSocket    = runDir + "/qmp.sock"
	agentSocket  = runDir + "/guest-agent.sock"
	serialSocket = runDir + "/serial.sock"
)

type documentMount struct {
	directory, Tag, Target string
	slot                   int
}

// This order is shared with the directory descriptors passed in ExtraFiles.
var documentMounts = []documentMount{
	{"Backups", "haosbackup", "/mnt/data/supervisor/backup", 4},
	{"Media", "haosmedia", "/mnt/data/supervisor/media/Host", 7},
	{"Share", "haosshare", "/mnt/data/supervisor/share/Host", 8},
}

type vmConfig struct {
	cpus, memory               int
	host                       netip.Addr
	bridge                     netip.Prefix
	lan, lanMAC, managementMAC string
	disk, code, variables      string
}

func readConfig() (vmConfig, error) {
	var c vmConfig
	for _, setting := range []struct {
		name     string
		fallback int
		value    *int
	}{
		{"HAOS_CPUS", 2, &c.cpus},
		{"HAOS_MEMORY_MIB", 4096, &c.memory},
	} {
		value, set := os.LookupEnv(setting.name)
		if !set {
			*setting.value = setting.fallback
			continue
		}
		n, err := strconv.ParseUint(value, 10, strconv.IntSize-1)
		if err != nil || n == 0 {
			return c, fmt.Errorf("%s must be a positive integer", setting.name)
		}
		*setting.value = int(n)
	}
	if host := os.Getenv("HAOS_HOST_IP"); host != "" {
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.Is4() {
			return c, fmt.Errorf("HAOS_HOST_IP must be an IPv4 address: %q", host)
		}
		c.host = ip
	}
	return c, nil
}

func (c vmConfig) managementPrefix() string {
	if c.host.Is4() && c.host.As4()[0] == 10 {
		return "172.31.249."
	}
	return "10.0.2."
}

//go:embed guest/*.tmpl
var guestTemplateFiles embed.FS

var guestTemplates = template.Must(template.ParseFS(guestTemplateFiles, "guest/*.tmpl"))

//go:embed guest/prepare.conf
var dockerDropIn string

type guestTemplateData struct {
	GuestCIDR, Gateway, AppSubnet, HostRoute string
	CreatedAt                                string
	Mounts                                   []documentMount
}

func (data guestTemplateData) render(name string) (string, error) {
	var output strings.Builder
	if err := guestTemplates.ExecuteTemplate(&output, name, data); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	return output.String(), nil
}

func (c vmConfig) systemdCredentials(prefix string) ([]string, error) {
	data := guestTemplateData{
		GuestCIDR: prefix + "15/24",
		Gateway:   prefix + "2",
		AppSubnet: c.bridge.Masked().String(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Mounts:    documentMounts,
	}
	if c.host.IsValid() {
		data.HostRoute = c.host.String() + "/32"
	}
	profile, err := data.render("mgmt.nmconnection.tmpl")
	if err != nil {
		return nil, err
	}
	httpConfig, err := data.render("http.json.tmpl")
	if err != nil {
		return nil, err
	}
	fstab, err := data.render("fstab.tmpl")
	if err != nil {
		return nil, err
	}
	encode := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	tmpfiles := "f~ /run/NetworkManager/system-connections/haos-lpk-mgmt.nmconnection 0600 root root - " + encode(profile) + "\n"
	tmpfiles += "f~ /run/haos-lpk-http.json 0600 root root - " + encode(httpConfig) + "\n"
	const smbios = "type=11,value=io.systemd.credential.binary:"
	return []string{
		"-smbios", smbios + "fstab.extra=" + encode(fstab),
		"-smbios", smbios + "tmpfiles.extra=" + encode(tmpfiles),
		"-fw_cfg", "name=opt/haos-lpk/prepare,file=/lzcapp/pkg/content/prepare.sh",
		"-smbios", smbios + "systemd.unit-dropin.docker.service~haos-lpk=" + encode(dockerDropIn),
	}, nil
}

func (c vmConfig) qemuArgs() ([]string, error) {
	prefix := c.managementPrefix()
	credentials, err := c.systemdCredentials(prefix)
	if err != nil {
		return nil, err
	}
	args := []string{
		"-name", "haos", "-machine", "q35,accel=kvm", "-cpu", "host",
		"-m", strconv.Itoa(c.memory), "-smp", strconv.Itoa(c.cpus), "-nodefaults", "-display", "none",
		"-drive", "if=pflash,unit=0,format=raw,readonly=on,file=" + c.code,
		"-drive", "if=pflash,unit=1,format=raw,file=" + c.variables,
		"-drive", "if=none,id=os,format=qcow2,discard=unmap,file=" + c.disk,
		"-device", "virtio-blk-pci,drive=os,bus=pcie.0,addr=0x3,bootindex=1",
		"-netdev", "vde,id=lan,sock=pcap://" + c.lan,
		"-device", "virtio-net-pci,netdev=lan,bus=pcie.0,addr=0x1,mac=" + c.lanMAC,
		"-netdev", "user,id=mgmt,net=" + prefix + "0/24,ipv6=off,hostfwd=tcp:" + c.bridge.Addr().String() + ":8123-" + prefix + "15:80",
		"-device", "virtio-net-pci,netdev=mgmt,bus=pcie.0,addr=0x2,mac=" + c.managementMAC,
		"-device", "virtio-serial-pci,id=guestserial,bus=pcie.0,addr=0x5",
		"-chardev", "socket,id=agent,path=" + agentSocket + ",server=on,wait=off",
		"-device", "virtserialport,bus=guestserial.0,chardev=agent,name=org.qemu.guest_agent.0",
		"-qmp", "unix:" + qmpSocket + ",server=on,wait=off",
		"-serial", "unix:" + serialSocket + ",server=on,wait=off",
	}
	for i, mount := range documentMounts {
		args = append(args, "-fsdev",
			fmt.Sprintf("local,id=%s,path=/proc/self/fd/%d,security_model=mapped-xattr,fmode=0644,dmode=0755", mount.Tag, i+3),
			"-device", fmt.Sprintf("virtio-9p-pci,fsdev=%s,mount_tag=%s,bus=pcie.0,addr=0x%x", mount.Tag, mount.Tag, mount.slot))
	}
	return append(args, credentials...), nil
}
