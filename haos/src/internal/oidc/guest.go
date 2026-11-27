package oidc

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type pendingRestart struct {
	Fingerprint string
	After       time.Time
	CoreStarted string
}

func (p pendingRestart) save(name string) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(name), ".haos-oidc-restart-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(data)
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), name)
}

type coreControl struct {
	ha      func(...string) ([]byte, error)
	started func() (string, error)
}

const shareDir = "/run/haos-oidc"
const supervisorDir = "/mnt/data/supervisor"
const readinessInterval = 30 * time.Second
const retryInterval = 5 * time.Minute

func (core coreControl) supervisorRunning() (bool, error) {
	reply, err := core.ha("info", "--raw-json")
	if err != nil {
		return false, fmt.Errorf("ha info: %w", err)
	}
	var info struct {
		Result string
		Data   struct{ State string }
	}
	if json.Unmarshal(reply, &info) != nil || info.Result != "ok" || info.Data.State == "" {
		return false, errors.New("invalid ha info response")
	}
	return info.Data.State == "running", nil
}

func reconcile(root *os.Root, share, pendingPath string, now time.Time, core coreControl) (time.Duration, error) {
	if err := recoverTransaction(root); err != nil {
		return 0, err
	}
	config, info, err := regularFile(root, mainConfig)
	if err != nil {
		return 0, err
	}
	if info == nil {
		return readinessInterval, nil
	}
	marker, err := pathInfo(root, bootstrapMarker)
	if err != nil {
		return 0, err
	}
	initial := marker == nil
	plugin, err := pathInfo(root, component)
	if err != nil {
		return 0, err
	}
	dir, err := pathInfo(root, configDir)
	if err != nil {
		return 0, err
	}
	if initial {
		if plugin != nil || dir != nil {
			return 0, nil
		}
	} else {
		// Once seeded, HA owns the component directory and provider settings.
		if plugin == nil || !plugin.IsDir() || dir == nil || !dir.IsDir() {
			return 0, nil
		}
		provider, err := pathInfo(root, providerConfig)
		if err != nil || provider == nil || !provider.Mode().IsRegular() {
			return 0, err
		}
	}
	if claimed, err := hasUIProvider(root); err != nil || claimed {
		return 0, err
	}
	ready, err := onboardingComplete(root)
	if err != nil {
		return 0, err
	}
	if !ready || len(bytes.TrimSpace(config)) == 0 {
		return readinessInterval, nil
	}
	nextConfig, home, err := withInclude(config)
	if err != nil {
		return 0, err
	}
	if nextConfig == nil || !initial && !bytes.Equal(config, nextConfig) {
		return 0, nil
	}
	oldPrivate, privateInfo, err := regularFile(root, privateConfig)
	if err != nil {
		return 0, err
	}
	if privateInfo != nil && !bytes.HasPrefix(oldPrivate, []byte(managedHeader)) {
		return 0, nil
	}
	raw, err := os.ReadFile(filepath.Join(share, "config.json"))
	if err != nil {
		return 0, err
	}
	nextPrivate, err := privateYAML(raw)
	if err != nil {
		return 0, err
	}
	changePrivate := privateInfo == nil || privateInfo.Mode().Perm() != 0600 || !bytes.Equal(oldPrivate, nextPrivate)

	var pending pendingRestart
	stored, err := os.ReadFile(pendingPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	if stored != nil && json.Unmarshal(stored, &pending) != nil {
		return 0, errors.New("invalid pending Core restart")
	}
	if home != nil {
		conflict, err := usesPackageOIDC(root, home, ".", homeSettings, 0)
		if conflict || err != nil {
			return 0, err
		}
	}
	if !initial && !changePrivate && stored == nil {
		return 0, nil
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(nextPrivate))
	if pending.Fingerprint == fingerprint && now.Before(pending.After) {
		return pending.After.Sub(now), nil
	}
	ready, err = core.supervisorRunning()
	if err != nil || !ready {
		return readinessInterval, err
	}

	// Supervisor may time out after Docker has already restarted Core. Its
	// container start time distinguishes this from a rejected restart.
	started, err := core.started()
	if err != nil {
		return 0, err
	}
	if started == "" {
		return readinessInterval, nil
	}
	if !initial && !changePrivate && pending.Fingerprint == fingerprint && pending.CoreStarted != "" && pending.CoreStarted != started {
		return 0, os.Remove(pendingPath)
	}

	// A crash or a failed CLI invocation must leave a retry pending. On a
	// guest reboot Core will naturally load the files and /run starts clean.
	pending = pendingRestart{Fingerprint: fingerprint, After: now.Add(retryInterval)}
	if err := pending.save(pendingPath); err != nil {
		return 0, err
	}
	record := rollbackRecord{Format: 1, Initial: initial, HadPrivate: privateInfo != nil}
	if initial {
		record.MainHash = contentHash(nextConfig)
		record.ProviderHash = contentHash(defaultProvider)
	}
	if changePrivate {
		record.PrivateHash = contentHash(nextPrivate)
	}
	changed := initial || changePrivate
	if changed {
		if err := record.begin(root); err != nil {
			return 0, err
		}
	}
	apply := func() error {
		if initial {
			sourceDigest, err := releaseDigest(share)
			if err != nil {
				return err
			}
			if err := installRelease(root, share, sourceDigest); err != nil {
				return err
			}
			if err := root.Mkdir(configDir, 0700); err != nil {
				return err
			}
			if err := replaceFile(root, providerConfig, nil, defaultProvider, 0644); err != nil {
				return err
			}
		}
		if changePrivate {
			if err := replaceFile(root, privateConfig, oldPrivate, nextPrivate, 0600); err != nil {
				return err
			}
		}
		if initial {
			if err := replaceFile(root, mainConfig, config, nextConfig, info.Mode().Perm()); err != nil {
				return err
			}
		}
		// CLI output may contain YAML values. Keep it out of the journal.
		if _, err := core.ha("core", "check"); err != nil {
			return fmt.Errorf("ha core check: %w", err)
		}
		if initial {
			return replaceFile(root, bootstrapMarker, nil, []byte("initialized\n"), 0600)
		}
		return nil
	}
	if err := apply(); err != nil {
		return 0, errors.Join(err, recoverTransaction(root))
	}
	if changed {
		if err := closeTransaction(root); err != nil {
			return 0, err
		}
	}
	pending.CoreStarted = started
	if err := pending.save(pendingPath); err != nil {
		return 0, err
	}
	if _, err := core.ha("core", "restart"); err != nil {
		return 0, fmt.Errorf("ha core restart: %w", err)
	}
	return 0, os.Remove(pendingPath)
}

// Run provisions HA when invoked inside the HAOS VM.
func Run(recoverOnly bool) error {
	_, err := runOnce(recoverOnly)
	return err
}

func runOnce(recoverOnly bool) (time.Duration, error) {
	// Constrain all guest configuration operations, including restored symlinks,
	// to Core's configuration directory. Do not follow a symlink for that root.
	supervisor, err := os.OpenRoot(supervisorDir)
	if err != nil {
		return 0, err
	}
	defer supervisor.Close()
	info, err := supervisor.Lstat("homeassistant")
	if errors.Is(err, fs.ErrNotExist) {
		return readinessInterval, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, errors.New("homeassistant configuration root must be a directory")
	}
	root, err := supervisor.OpenRoot("homeassistant")
	if err != nil {
		return 0, err
	}
	defer root.Close()
	if recoverOnly {
		return 0, recoverTransaction(root)
	}
	return reconcile(root, shareDir, "/run/haos-lpk-oidc-restart", time.Now(), haCore)
}

var haCore = coreControl{
	ha: func(args ...string) ([]byte, error) { return exec.Command("/usr/bin/ha", args...).Output() },
	started: func() (string, error) {
		out, err := exec.Command("docker", "inspect", "--format", "{{if .State.Running}}{{.State.StartedAt}}{{end}}", "homeassistant").Output()
		return strings.TrimSpace(string(out)), err
	},
}
