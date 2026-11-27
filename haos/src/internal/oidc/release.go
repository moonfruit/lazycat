package oidc

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const component = "custom_components/auth_oidc"

func validDigest(digest string) bool {
	b, err := hex.DecodeString(digest)
	return err == nil && len(b) == sha256.Size
}

func releaseDigest(share string) (string, error) {
	b, err := os.ReadFile(filepath.Join(share, "hass-oidc-auth.zip.sha256"))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 || !validDigest(fields[0]) {
		return "", errors.New("invalid shared release digest")
	}
	return strings.ToLower(fields[0]), nil
}

func unpackRelease(root *os.Root, share, digest, stage string) error {
	archive, err := os.ReadFile(filepath.Join(share, "hass-oidc-auth.zip"))
	if err != nil {
		return err
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != digest {
		return errors.New("shared release checksum mismatch")
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	if err := root.Mkdir(stage, 0755); err != nil {
		return err
	}
	dest, err := root.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer dest.Close()
	for _, entry := range reader.File {
		name := strings.TrimSuffix(entry.Name, "/")
		if !fs.ValidPath(name) || name == "." {
			return errors.New("invalid path in OIDC release")
		}
		if entry.FileInfo().IsDir() {
			if err := dest.MkdirAll(name, 0755); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return errors.New("non-regular file in OIDC release")
		}
		if err := dest.MkdirAll(path.Dir(name), 0755); err != nil {
			return err
		}
		if err := unpackFile(dest, entry); err != nil {
			return err
		}
	}
	manifest, err := dest.ReadFile("manifest.json")
	if err != nil {
		return err
	}
	var meta struct{ Domain, Version string }
	if json.Unmarshal(manifest, &meta) != nil || meta.Domain != "auth_oidc" || meta.Version == "" {
		return errors.New("invalid OIDC release manifest")
	}
	if _, err := dest.Stat("__init__.py"); err != nil {
		return err
	}
	return nil
}

func unpackFile(dest *os.Root, entry *zip.File) error {
	in, err := entry.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := dest.OpenFile(entry.Name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	return errors.Join(err, out.Close())
}

func installRelease(root *os.Root, share, digest string) error {
	stage := transactionDir + "/component"
	if err := unpackRelease(root, share, digest, stage); err != nil {
		return err
	}
	if err := root.MkdirAll("custom_components", 0755); err != nil {
		return err
	}
	dir, err := root.Lstat("custom_components")
	if err != nil {
		return err
	}
	if !dir.IsDir() {
		return errors.New("custom_components must be a directory")
	}
	if _, err := root.Lstat(component); !errors.Is(err, fs.ErrNotExist) {
		return errors.New("OIDC component path is already in use")
	}
	// Keep a reference to our own manifest so rollback can identify the seed
	// even if HACS concurrently replaces the component directory.
	if err := root.Link(path.Join(stage, "manifest.json"), seedManifest); err != nil {
		return err
	}
	return root.Rename(stage, component)
}
