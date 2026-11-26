package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func provisionGuestHTTP(t *testing.T, root, subnet string) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("guest HTTP provisioning uses jq")
	}
	seed := filepath.Join(root, "seed.json")
	data := guestTemplateData{AppSubnet: subnet, CreatedAt: "2026-01-01T00:00:00Z"}
	config, err := data.render("http.json.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seed, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "guest/prepare.sh", root, seed).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
}

func TestManagedProxyNetworkChange(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "homeassistant/.storage/http")
	const initial = "172.28.11.128/26"
	provisionGuestHTTP(t, root, initial)
	seed, err := os.ReadFile(filepath.Join(root, "seed.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(store); err != nil || string(got) != string(seed) {
		t.Fatalf("initial HTTP config: %s (%v)", got, err)
	}
	const settings = `{"data":{"stable":{"trusted_proxies":["%[1]s","192.0.2.90/32"],"server_port":9090},"pending":{"trusted_proxies":["%[1]s"],"server_port":9091}}}`
	edited := fmt.Sprintf(settings, initial)
	if err := os.WriteFile(store, []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	provisionGuestHTTP(t, root, initial)
	if got, err := os.ReadFile(store); err != nil || string(got) != edited {
		t.Fatalf("changed user settings on restart: %s (%v)", got, err)
	}
	for _, subnet := range []string{"172.28.11.192/26", "172.28.11.64/26"} {
		provisionGuestHTTP(t, root, subnet)
		data, err := os.ReadFile(store)
		if err != nil {
			t.Fatal(err)
		}
		var got, want any
		if err := json.Unmarshal([]byte(fmt.Sprintf(settings, subnet)), &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &got); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected HTTP settings after switch to %s: %s (%v)", subnet, data, err)
		}
	}
	custom := []byte(`{"data":{"stable":{"trusted_proxies":[]},"pending":null}}`)
	if err := os.WriteFile(store, custom, 0600); err != nil {
		t.Fatal(err)
	}
	provisionGuestHTTP(t, root, initial)
	got, err := os.ReadFile(store)
	if err != nil || string(got) != string(custom) {
		t.Fatalf("overrode user trust: %s %v", got, err)
	}
}

func TestLeaveExistingHTTPConfigUnmanaged(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "homeassistant/.storage")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dir, "http")
	const existing = `{"data":{"stable":{"trusted_proxies":["172.28.11.128/26"]},"pending":null}}`
	if err := os.WriteFile(store, []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}
	provisionGuestHTTP(t, root, "172.28.11.128/26")
	provisionGuestHTTP(t, root, "172.28.11.192/26")
	if got, err := os.ReadFile(store); err != nil || string(got) != existing {
		t.Fatalf("changed pre-existing HTTP config: %s (%v)", got, err)
	}
}
