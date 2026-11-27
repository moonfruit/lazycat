package oidc

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"

	"go.yaml.in/yaml/v3"
)

// Keep original configuration on the same filesystem until Core validates it.
const transactionDir = ".haos-lpk-oidc.transaction"
const seedManifest = transactionDir + "/seed-manifest"

type rollbackRecord struct {
	Format                              int
	MainHash, ProviderHash, PrivateHash string
	HadPrivate, Initial                 bool
}

func contentHash(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }

// Publish the record only after the original files are available for recovery.
// Hard links keep the originals' owner, mode and bytes.
func (r rollbackRecord) begin(root *os.Root) error {
	stage := tempName(".")
	if err := root.Mkdir(stage, 0700); err != nil {
		return err
	}
	defer root.RemoveAll(stage)
	for _, file := range []struct {
		name string
		keep bool
	}{
		{mainConfig, r.MainHash != ""}, {privateConfig, r.PrivateHash != "" && r.HadPrivate},
	} {
		if file.keep {
			if err := root.Link(file.name, path.Join(stage, path.Base(file.name))); err != nil {
				return err
			}
		}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := root.WriteFile(path.Join(stage, "record"), data, 0600); err != nil {
		return err
	}
	if _, err := root.Lstat(transactionDir); !errors.Is(err, fs.ErrNotExist) {
		return errors.New("OIDC rollback directory is already in use")
	}
	return root.Rename(stage, transactionDir)
}

// Detach the completed transaction before removing its contents. Interruption
// during cleanup cannot turn a committed change back into a pending rollback.
func closeTransaction(root *os.Root) error {
	stale := tempName(".")
	if err := root.Rename(transactionDir, stale); err != nil {
		return err
	}
	return root.RemoveAll(stale)
}

func recoverTransaction(root *os.Root) error {
	info, err := root.Lstat(transactionDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("invalid OIDC rollback directory")
	}
	data, _, err := regularFile(root, transactionDir+"/record")
	if err != nil {
		return err
	}
	var r rollbackRecord
	if json.Unmarshal(data, &r) != nil || r.Format != 1 {
		return errors.New("unrecognized OIDC rollback record")
	}
	if r.Initial {
		if _, err := root.Lstat(bootstrapMarker); err == nil {
			// The first configuration check succeeded before the interruption.
			return closeTransaction(root)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if r.MainHash != "" {
		if err := r.restoreFile(root, mainConfig, r.MainHash, true); err != nil {
			return err
		}
	}
	if r.PrivateHash != "" {
		if err := r.restoreFile(root, privateConfig, r.PrivateHash, r.HadPrivate); err != nil {
			return err
		}
	}
	if r.Initial {
		if err := r.restoreFile(root, providerConfig, r.ProviderHash, false); err != nil {
			return err
		}
		if err := root.Remove(configDir); err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) {
			return err
		}
		if err := removeSeed(root); err != nil {
			return err
		}
	}
	return closeTransaction(root)
}

func (r rollbackRecord) restoreFile(root *os.Root, name, after string, had bool) error {
	current, info, err := regularFile(root, name)
	if err != nil || info == nil {
		return err
	}
	if contentHash(current) != after {
		// Someone edited the main config during a check. Remove only the
		// precise top-level line we inserted, retaining all concurrent edits.
		if name == mainConfig {
			if next := removeManagedInclude(current); next != nil {
				return replaceFile(root, name, current, next, info.Mode().Perm())
			}
		}
		return nil
	}
	if !had {
		return root.Remove(name)
	}
	err = root.Rename(path.Join(transactionDir, path.Base(name)), name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} // already restored
	return err
}

func removeManagedInclude(data []byte) []byte {
	var doc yaml.Node
	if yaml.Unmarshal(data, &doc) != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	keys := doc.Content[0].Content
	lines := bytes.SplitAfter(data, []byte("\n"))
	for i := 0; i < len(keys); i += 2 {
		key, val := keys[i], keys[i+1]
		n := key.Line - 1
		if n >= 0 && n < len(lines) && key.Value == "auth_oidc" && val.Tag == "!include" && val.Value == providerConfig && val.LineComment == includeMarker &&
			strings.TrimSpace(string(lines[n])) == "auth_oidc: !include "+providerConfig+" "+includeMarker {
			return bytes.Join(append(lines[:n], lines[n+1:]...), nil)
		}
	}
	return nil
}

func removeSeed(root *os.Root) error {
	seed, err := root.Lstat(seedManifest)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	current, err := root.Lstat(component + "/manifest.json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil || !os.SameFile(seed, current) {
		return err
	}
	return root.Rename(component, transactionDir+"/rejected-component")
}
