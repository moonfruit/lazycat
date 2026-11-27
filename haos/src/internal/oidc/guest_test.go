package oidc

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const defaults = "# 保留注释\r\n---\r\ndefault_config:\r\nautomation: !include automations.yaml\r\n...\r\n"
const desired = `{"client_id":"app","client_secret":"a: #\"b","discovery_url":"https://id.test/.well-known/openid-configuration"}`

type fixture struct {
	t                   *testing.T
	dir, share, pending string
	root                *os.Root
	calls               []string
	state               string
	fail                string
	started             string
	now                 time.Time
}

func newFixture(t *testing.T) *fixture {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	f := &fixture{
		t:       t,
		dir:     dir,
		root:    root,
		share:   t.TempDir(),
		pending: filepath.Join(t.TempDir(), "restart"),
		state:   "running",
		started: "2026-11-24T01:02:03Z",
		now:     time.Now(),
	}
	f.write("configuration.yaml", defaults)
	f.write(".storage/auth", `{"data":{"users":[{"is_owner":true,"is_active":true}]}}`)
	f.write(".storage/onboarding", `{"data":{"done":["user","core_config","integration","analytics"]}}`)
	f.write(".storage/core.config_entries", `{"data":{"entries":[]}}`)
	f.input("config.json", desired)
	f.release("1.3.0")
	return f
}

func (f *fixture) write(path, content string) {
	f.t.Helper()
	p := filepath.Join(f.dir, path)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) read(path string) string {
	f.t.Helper()
	b, err := f.root.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

func (f *fixture) input(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.share, name), []byte(content), 0600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) release(version string) {
	f.t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range map[string]string{"manifest.json": `{"domain":"auth_oidc","version":"` + version + `"}`, "__init__.py": "# plugin " + version, "static/device.js": "// compiled"} {
		file, err := w.Create(name)
		if err != nil {
			f.t.Fatal(err)
		}
		if _, err = file.Write([]byte(data)); err != nil {
			f.t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		f.t.Fatal(err)
	}
	f.input("hass-oidc-auth.zip", buf.String())
	f.input("hass-oidc-auth.zip.sha256", fmt.Sprintf("%x  hass-oidc-auth.zip\n", sha256.Sum256(buf.Bytes())))
}

func (f *fixture) sync() error {
	_, err := f.check()
	return err
}

func (f *fixture) check() (time.Duration, error) {
	return reconcile(f.root, f.share, f.pending, f.now, coreControl{started: func() (string, error) { return f.started, nil }, ha: func(args ...string) ([]byte, error) {
		call := strings.Join(args, " ")
		f.calls = append(f.calls, call)
		if call == f.fail {
			return nil, fmt.Errorf("exit 1")
		}
		return []byte(`{"result":"ok","data":{"state":"` + f.state + `"}}`), nil
	}})
}

func (f *fixture) run() {
	f.t.Helper()
	if err := f.sync(); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) expectCalls(want ...string) {
	f.t.Helper()
	if !reflect.DeepEqual(f.calls, want) {
		f.t.Fatalf("commands %v; want %v", f.calls, want)
	}
	f.calls = nil
}

func (f *fixture) interruptCheck() {
	f.t.Helper()
	const powerLoss = "power loss during configuration check"
	defer func() {
		if recover() != powerLoss {
			f.t.Fatal("fixture did not interrupt an active configuration check")
		}
	}()
	_, _ = reconcile(f.root, f.share, f.pending, f.now, coreControl{
		started: func() (string, error) { return f.started, nil },
		ha: func(args ...string) ([]byte, error) {
			if strings.Join(args, " ") == "core check" {
				panic(powerLoss)
			}
			return []byte(`{"result":"ok","data":{"state":"running"}}`), nil
		},
	})
}

func TestInstallAndReconcileRestoredCredentials(t *testing.T) {
	f := newFixture(t)
	f.run()
	f.expectCalls("info --raw-json", "core check", "core restart")
	conf := f.read("configuration.yaml")
	if strings.Count(conf, "auth_oidc:") != 1 || !strings.Contains(conf, "automation: !include automations.yaml\r\n...\r\n") || !strings.HasPrefix(conf, "# 保留注释\r\n---\r\n") || strings.Contains(conf, "client_secret") {
		t.Fatal(conf)
	}
	private := f.read(privateConfig)
	provider := f.read(providerConfig)
	if !strings.Contains(provider, "admin: ADMIN") || !strings.Contains(provider, "display_name: 懒猫微服") || strings.Contains(private, "roles") {
		t.Fatal("initial role mapping and provider settings must be in the editable provider YAML")
	}
	info, err := f.root.Stat(privateConfig)
	if err != nil || info.Mode().Perm() != 0600 || !strings.Contains(private, `a: #\"b`) {
		t.Fatalf("private include absent, invalid or wrong permissions")
	}
	if f.read("custom_components/auth_oidc/static/device.js") != "// compiled" {
		t.Fatal("missing release resources")
	}
	if err := os.Remove(filepath.Join(f.share, "hass-oidc-auth.zip")); err != nil {
		t.Fatal(err)
	}
	f.run()
	f.expectCalls()
	f.input("config.json", strings.Replace(desired, "app", "new-client", 1))
	f.run()
	f.expectCalls("info --raw-json", "core check", "core restart")
	f.write(privateConfig, private)
	f.run()
	f.expectCalls("info --raw-json", "core check", "core restart")
	if !strings.Contains(f.read(privateConfig), "new-client") {
		t.Fatal("restored secret did not converge")
	}
	f.run()
	f.expectCalls()
	f.release("1.4.0")
	f.run()
	f.expectCalls()
	if !strings.Contains(f.read("custom_components/auth_oidc/__init__.py"), "1.3.0") {
		t.Fatal("LPK replaced the installed integration")
	}
}

func TestUserManagedOrIncompleteConfigIsUntouched(t *testing.T) {
	for _, tc := range []struct{ name, path, value string }{
		{"new HA", "configuration.yaml", ""},
		{"no owner", ".storage/auth", `{"data":{"users":[{"is_owner":false}]}}`},
		{"onboarding underway", ".storage/onboarding", `{"data":{"done":["user"]}}`},
		{"quoted manual provider", "configuration.yaml", `"auth_oidc": !include mine.yaml`},
		{"duplicate HA domain", "configuration.yaml", "auth_oidc old: !include mine.yaml\n"},
		{"ui provider", ".storage/core.config_entries", `{"data":{"entries":[{"domain":"auth_oidc"}]}}`},
		{"installed plugin", "custom_components/auth_oidc/__init__.py", "# HACS installed"},
		{"unmarked include", privateConfig, "user-owned\n"},
		{"nested package", "configuration.yaml", "homeassistant:\n  packages:\n    custom:\n      auth_oidc: !include mine.yaml\n"},
		{"external packages", "configuration.yaml", "homeassistant:\n  packages: !include_dir_named packages\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.write(tc.path, tc.value)
			if tc.name == "external packages" {
				f.write("packages/oidc.yaml", "auth_oidc: {}")
			}
			original := f.read("configuration.yaml")
			f.run()
			f.expectCalls()

			if f.read("configuration.yaml") != original {
				t.Fatal("changed root configuration")
			}
			if f.read(tc.path) != tc.value {
				t.Fatal("changed existing owner or config")
			}
		})
	}
}

func TestSupervisorFreezeAndFailedCheck(t *testing.T) {
	f := newFixture(t)
	f.state = "freeze"
	f.run()
	f.expectCalls("info --raw-json")
	if f.read("configuration.yaml") != defaults {
		t.Fatal("modified during restore")
	}
	f.state = "running"
	f.fail = "core check"
	if f.sync() == nil {
		t.Fatal("failed config check hidden")
	}
	f.expectCalls("info --raw-json", "core check")
	f.run()
	f.expectCalls()
	f.now = f.now.Add(15 * time.Minute)
	f.fail = ""
	f.run()
	f.expectCalls("info --raw-json", "core check", "core restart")
	f.run()
	f.expectCalls()
}

func TestCheckSchedule(t *testing.T) {
	f := newFixture(t)
	check := func(want time.Duration) {
		t.Helper()
		delay, err := f.check()
		if err != nil || delay != want {
			t.Fatalf("next check in %v, want %v (err=%v)", delay, want, err)
		}
	}
	steps := f.read(".storage/onboarding")
	f.write(".storage/onboarding", `{"data":{"done":["user"]}}`)
	check(30 * time.Second)
	f.write(".storage/onboarding", steps)
	f.state = "freeze"
	check(30 * time.Second)
	f.state, f.fail = "running", "core check"
	if _, err := f.check(); err == nil {
		t.Fatal("expected failed check")
	}
	f.fail = ""
	f.now = f.now.Add(time.Minute)
	check(4 * time.Minute)
	f.now = f.now.Add(4 * time.Minute)
	check(0) // Installed: wait for a file event.
	check(0)
	if err := f.root.RemoveAll(component); err != nil {
		t.Fatal(err)
	}
	check(0) // An intentional uninstall must not restart the onboarding poll.
}

func TestUnreadableArchiveAndSymlinkLeaveUserFilesIntact(t *testing.T) {
	f := newFixture(t)
	f.input("hass-oidc-auth.zip", "corrupt")
	if f.sync() == nil {
		t.Fatal("bad release accepted")
	}
	if f.read("configuration.yaml") != defaults {
		t.Fatal("changed config before validating release")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(f.dir, "custom_components")); err != nil {
		t.Fatal(err)
	}
	f.release("1.3.0")
	f.now = f.now.Add(10 * time.Minute)
	if err := f.sync(); err == nil {
		t.Fatal("symlinked custom_components accepted")
	}
	items, err := os.ReadDir(outside)
	if err != nil || len(items) != 0 {
		t.Fatal("wrote outside Home Assistant config")
	}
}

func TestInspectPackagesBeforeChangingOIDC(t *testing.T) {
	for _, tc := range []struct {
		name, config, file, value string
		install                   bool
	}{
		{"inline without OIDC", "homeassistant:\n  packages:\n    meter:\n      template: !include meter.yaml\n", "", "", true},
		{"named directory", "homeassistant:\n  packages: !include_dir_named packages\n", "packages/subdir/light.yaml", "light: !include lights.yaml", true},
		{"merged directory", "homeassistant:\n  packages: !include_dir_merge_named packages\n", "packages/meter.yaml", "meter:\n  sensor: []", true},
		{"named directory with OIDC", "homeassistant:\n  packages: !include_dir_named packages\n", "packages/user.yaml", "auth_oidc user: !include my-oidc.yaml", false},
		{"merged directory with OIDC", "homeassistant:\n  packages: !include_dir_merge_named packages\n", "packages/one.yaml", "my_package:\n  'auth_oidc': {}", false},
		{"single file at homeassistant level", "homeassistant: !include my/home.yaml\n", "my/home.yaml", "packages: {test: {light: []}}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.write("configuration.yaml", tc.config)
			if tc.file != "" {
				f.write(tc.file, tc.value)
			}
			f.run()
			if tc.install {
				f.expectCalls("info --raw-json", "core check", "core restart")
			} else {
				f.expectCalls()
				if f.read("configuration.yaml") != tc.config {
					t.Fatal("overwrote package")
				}
			}
		})
	}
}

func TestUnresolvedPackagesAreDiagnosedWhenActionIsNeeded(t *testing.T) {
	f := newFixture(t)
	f.write("configuration.yaml", "homeassistant:\n  packages: !include absent.yaml\n")
	if f.sync() == nil {
		t.Fatal("missing package was silently skipped")
	}
	f.expectCalls()
	f.write(".storage/core.config_entries", `{"data":{"entries":[{"domain":"auth_oidc"}]}}`)
	f.run()
	f.expectCalls()
}

func TestRestartTimeoutAfterCoreWasRestartedDoesNotRestartAgain(t *testing.T) {
	f := newFixture(t)
	f.fail = "core restart"
	if f.sync() == nil {
		t.Fatal("lost CLI failure")
	}
	f.expectCalls("info --raw-json", "core check", "core restart")
	f.fail = ""
	f.now = f.now.Add(10 * time.Minute)
	f.started = "2026-11-26T01:02:03Z"
	f.run()
	f.expectCalls("info --raw-json")
	f.run()
	f.expectCalls()
}

func TestOnboardedInstanceWithoutUIEntriesCanBeConfigured(t *testing.T) {
	f := newFixture(t)
	if err := f.root.Remove(".storage/core.config_entries"); err != nil {
		t.Fatal(err)
	}
	f.run()
	f.expectCalls("info --raw-json", "core check", "core restart")
}
func TestUnmanagedUIEntryTakesPrecedenceOverUnsupportedYAML(t *testing.T) {
	f := newFixture(t)
	f.write("configuration.yaml", "!include custom-root.yml")
	f.write(".storage/core.config_entries", `{"data":{"entries":[{"domain":"auth_oidc"}]}}`)
	f.run()
	f.expectCalls()
}

func TestFailedCheckRestoresFirstInstallAndRotatedCredentials(t *testing.T) {
	f := newFixture(t)
	f.fail = "core check"
	if f.sync() == nil {
		t.Fatal("expected rejection")
	}
	if f.read("configuration.yaml") != defaults {
		t.Fatal("rejected main configuration is still active")
	}
	for _, name := range []string{bootstrapMarker, configDir, component} {
		if _, err := f.root.Lstat(name); !os.IsNotExist(err) {
			t.Fatal("rejected install left", name)
		}
	}
	f.now = f.now.Add(10 * time.Minute)
	f.fail = ""
	f.run()
	oldMain, oldPrivate, oldCode := f.read("configuration.yaml"), f.read(privateConfig), f.read("custom_components/auth_oidc/__init__.py")
	f.input("config.json", strings.Replace(desired, "app", "rotated", 1))
	f.release("1.4.0")
	f.fail = "core check"
	if f.sync() == nil {
		t.Fatal("expected rejection")
	}
	if f.read("configuration.yaml") != oldMain || f.read(privateConfig) != oldPrivate || f.read("custom_components/auth_oidc/__init__.py") != oldCode {
		t.Fatal("failed rotation did not preserve working credentials and component")
	}
}

func TestPreDockerRecoveryRestoresInterruptedCredentialRotation(t *testing.T) {
	f := newFixture(t)
	f.run()
	oldPrivate := f.read(privateConfig)
	f.release("1.4.1")
	f.input("config.json", strings.Replace(desired, "app", "rotated", 1))
	f.interruptCheck()
	if err := recoverTransaction(f.root); err != nil {
		t.Fatal(err)
	}
	if f.read("custom_components/auth_oidc/__init__.py") != "# plugin 1.3.0" || f.read(privateConfig) != oldPrivate {
		t.Fatal("pre-Docker recovery did not restore credentials and preserve the installed plugin")
	}
	if err := recoverTransaction(f.root); err != nil {
		t.Fatal(err)
	}
}

func TestFailedCheckKeepsConcurrentUserEdit(t *testing.T) {
	f := newFixture(t)
	_, err := reconcile(f.root, f.share, f.pending, f.now, coreControl{
		started: func() (string, error) { return f.started, nil },
		ha: func(args ...string) ([]byte, error) {
			if strings.Join(args, " ") == "core check" {
				conf := f.read("configuration.yaml")
				f.write("configuration.yaml", strings.Replace(conf, "default_config:", "default_config:\r\n# saved by editor", 1))
				return nil, fmt.Errorf("invalid configuration")
			}
			return []byte(`{"result":"ok","data":{"state":"running"}}`), nil
		},
	})
	if err == nil {
		t.Fatal("check failure hidden")
	}
	want := strings.Replace(defaults, "default_config:", "default_config:\r\n# saved by editor", 1)
	if conf := f.read("configuration.yaml"); conf != want {
		t.Fatal(conf)
	}
}

func TestInterruptedFirstInstallKeepsReplacementPlugin(t *testing.T) {
	f := newFixture(t)
	f.interruptCheck()
	if err := f.root.RemoveAll(component); err != nil {
		t.Fatal(err)
	}
	f.write(component+"/manifest.json", `{"domain":"auth_oidc","version":"9.0.0"}`)
	if err := recoverTransaction(f.root); err != nil {
		t.Fatal(err)
	}
	if f.read(mainConfig) != defaults || !strings.Contains(f.read(component+"/manifest.json"), "9.0.0") {
		t.Fatal("recovery failed to restore HA configuration or keep the replacement plugin")
	}
	for _, name := range []string{bootstrapMarker, configDir, transactionDir} {
		if _, err := f.root.Lstat(name); !os.IsNotExist(err) {
			t.Fatal("interrupted installation left", name)
		}
	}
}

func TestHACSUpdatesAndUninstallAfterBootstrap(t *testing.T) {
	f := newFixture(t)
	f.run()
	f.expectCalls("info --raw-json", "core check", "core restart")
	if err := f.root.RemoveAll(component); err != nil {
		t.Fatal(err)
	}
	f.run() // Also covers HACS temporarily removing the directory during an update.
	f.expectCalls()
	if _, err := f.root.Lstat(component); !os.IsNotExist(err) {
		t.Fatal("removed integration was reinstalled")
	}

	f.write(component+"/__init__.py", "# HACS version")
	f.write(component+"/manifest.json", `{"domain":"auth_oidc","version":"9.0.0"}`)
	f.run()
	f.expectCalls()

	f.release("1.4.0")
	f.run()
	f.expectCalls()
	if err := os.Remove(filepath.Join(f.share, "hass-oidc-auth.zip.sha256")); err != nil {
		t.Fatal(err)
	}
	userConfig := f.read(providerConfig) + "\nadditional_scopes: [email]\n"
	f.write(providerConfig, userConfig)
	userMain := strings.Replace(f.read(mainConfig), " "+includeMarker, "", 1)
	f.write(mainConfig, userMain)
	f.input("config.json", strings.Replace(desired, `a: #\"b`, "rotated", 1))
	f.run()
	f.expectCalls("info --raw-json", "core check", "core restart")
	if !strings.Contains(f.read(privateConfig), "rotated") ||
		f.read(component+"/__init__.py") != "# HACS version" || f.read(providerConfig) != userConfig || f.read(mainConfig) != userMain {
		t.Fatal("credential rotation changed HA-managed code or settings")
	}

	f.write(mainConfig, defaults)
	f.input("config.json", desired)
	f.run()
	f.expectCalls()
	if f.read(mainConfig) != defaults || !strings.Contains(f.read(privateConfig), "rotated") {
		t.Fatal("continued to manage OIDC after the user removed the include")
	}
	if err := f.root.RemoveAll(component); err != nil {
		t.Fatal(err)
	}
	if err := f.root.RemoveAll(configDir); err != nil {
		t.Fatal(err)
	}
	f.run()
	f.expectCalls()
	if f.read(mainConfig) != defaults {
		t.Fatal("re-enabled a removed integration")
	}
}
