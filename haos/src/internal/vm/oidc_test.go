package vm

import (
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOIDCProvisioningAcrossVMBoundary(t *testing.T) {
	const secret = "fixture-client-secret"
	t.Setenv("LAZYCAT_AUTH_OIDC_ISSUER_URI", "https://box.example/sys/oauth/")
	t.Setenv("LAZYCAT_AUTH_OIDC_CLIENT_ID", "id-for-haos")
	t.Setenv("LAZYCAT_AUTH_OIDC_CLIENT_SECRET", secret)
	c, err := readConfig()
	if err != nil {
		t.Fatal(err)
	}
	c.bridge = netip.MustParsePrefix("192.168.255.194/26")
	c.host = netip.MustParseAddr("10.255.255.254")
	args, err := c.qemuArgs()
	if err != nil {
		t.Fatal(err)
	}
	var mount, export bool
	var profile string
	units := map[string]string{}
	firmwareSize := 8
	for _, arg := range args {
		if strings.Contains(arg, secret) {
			t.Fatal("client secret exposed in QEMU arguments")
		}
		if value, ok := strings.CutPrefix(arg, "type=11,value="); ok {
			firmwareSize += len(value) + 1
			credential, encoded, _ := strings.Cut(value, "=")
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil || strings.Contains(string(data), secret) {
				t.Fatal("invalid firmware credential or leaked secret")
			}
			mount = mount || strings.Contains(string(data), "haosoidc /run/haos-oidc 9p ro,")
			if unit, ok := strings.CutPrefix(credential, "io.systemd.credential.binary:systemd."); ok {
				units[unit] = string(data)
			}
			if credential == "io.systemd.credential.binary:tmpfiles.extra" {
				first, _, _ := strings.Cut(string(data), "\n")
				fields := strings.Fields(first)
				if len(fields) != 7 {
					t.Fatal("invalid NetworkManager tmpfiles rule")
				}
				raw, err := base64.StdEncoding.DecodeString(fields[6])
				if err != nil {
					t.Fatal(err)
				}
				profile = string(raw)
			}
		}
		if strings.Contains(arg, "id=haosoidc,") && strings.Contains(arg, "readonly=on") {
			export = true
		}
	}
	if !mount || !export || firmwareSize > 4095 {
		t.Fatalf("OIDC mount=%v read-only export=%v firmware size=%d", mount, export, firmwareSize)
	}
	docker := units["unit-dropin.docker.service~haos-lpk"]
	if !strings.Contains(docker, "Wants=haos-lpk-oidc.service\n") || !strings.Contains(docker, "ExecStartPre=-/run/haos-oidc/haos-vm oidc --recover\n") {
		t.Errorf("missing OIDC startup dependencies in Docker drop-in: %s", docker)
	}
	service := units["extra-unit.haos-lpk-oidc.service"]
	if !strings.Contains(service, "After=docker.service\n") || !strings.Contains(service, "ExecStart=/run/haos-oidc/haos-vm oidc --watch\n") {
		t.Errorf("missing OIDC service or Docker ordering: %s", service)
	}
	if _, periodic := units["extra-unit.haos-lpk-oidc.timer"]; periodic {
		t.Error("periodic check remains enabled after bootstrap")
	}
	if !strings.Contains(strings.Join(args, " "), "ipv4=on,ipv6=on,ipv6-net="+managementIPv6+"/64,hostfwd=") {
		t.Fatal("missing dual-stack management network for OIDC")
	}
	for _, want := range []string{"address1=" + managementIPv6 + "15/64\nnever-default=true",
		"route1=fc03:1136:3800::/40," + managementIPv6 + "2", "route1_options=src=" + managementIPv6 + "15"} {
		if !strings.Contains(profile, want) {
			t.Errorf("missing management profile setting %q", want)
		}
	}
	pkg, runtime := t.TempDir(), t.TempDir()
	for _, name := range []string{"hass-oidc-auth.zip", "haos-vm"} {
		if err := os.WriteFile(filepath.Join(pkg, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.prepareOIDCExport(runtime, pkg); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(runtime, "haos-vm")
	binary, err := os.ReadFile(executable)
	if err != nil || string(binary) != "haos-vm" {
		t.Fatal("missing shared HAOS executable")
	}
	if info, err := os.Stat(executable); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("shared HAOS executable must be executable only by root")
	}
	config := filepath.Join(runtime, "config.json")
	info, err := os.Stat(config)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("secret must be readable only by root in the guest")
	}
	data, err := os.ReadFile(config)
	var settings struct {
		ClientID     string `json:"client_id"`
		Secret       string `json:"client_secret"`
		DiscoveryURL string `json:"discovery_url"`
	}
	if err != nil || json.Unmarshal(data, &settings) != nil || settings.ClientID != "id-for-haos" || settings.Secret != secret || settings.DiscoveryURL != "https://box.example/sys/oauth/.well-known/openid-configuration" {
		t.Fatal("incorrect OIDC client")
	}
}

func TestIncompleteOIDCDoesNotPreventLocalLogin(t *testing.T) {
	t.Setenv("LAZYCAT_AUTH_OIDC_ISSUER_URI", "https://box.example/sys/oauth")
	t.Setenv("LAZYCAT_AUTH_OIDC_CLIENT_ID", "haos")
	t.Setenv("LAZYCAT_AUTH_OIDC_CLIENT_SECRET", "")
	c, err := readConfig()
	if err != nil || c.oidc != nil {
		t.Fatalf("expected VM with local authentication available, got oidc=%v err=%v", c.oidc != nil, err)
	}
}

func TestValidateOIDCIssuer(t *testing.T) {
	for _, raw := range []string{"https://box.example.net", "https://Box.Example.NET:443/oidc"} {
		if err := validateOIDCIssuer(raw); err != nil {
			t.Errorf("issuer %q: %v", raw, err)
		}
	}
	for _, raw := range []string{"http://box.example.net", "https://box.example.net:8443",
		"https://user@box.example.net", "https://box.example.net/path?key=value",
		"https://box.example.net/#x", "https://127.0.0.1", "https://[::1]",
		"https://box.localhost", "https://bad..example", "https://x_1.net", "https://-bad.net",
		"https://box.example.net%0anext", "https://box.example.net:", "https://"} {
		if err := validateOIDCIssuer(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}
