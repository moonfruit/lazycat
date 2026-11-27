package oidc

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path"
	"syscall"
)

func tempName(dir string) string {
	return path.Join(dir, ".haos-oidc-"+rand.Text())
}

// pathInfo distinguishes an absent path from an unreadable one without following its final symlink.
func pathInfo(root *os.Root, name string) (fs.FileInfo, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return info, err
}

func regularFile(root *os.Root, name string) ([]byte, fs.FileInfo, error) {
	info, err := pathInfo(root, name)
	if err != nil || info == nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("expected a regular file: " + name)
	}
	data, err := root.ReadFile(name)
	return data, info, err
}

// Replace a regular file through rename. Check for concurrent changes before
// committing and retain the existing configuration.yaml ownership and mode.
func replaceFile(root *os.Root, name string, old, next []byte, mode fs.FileMode) error {
	tmp := tempName(path.Dir(name))
	file, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	defer file.Close()
	if _, err := file.Write(next); err != nil {
		return err
	}
	current, info, err := regularFile(root, name)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, old) || (info == nil) != (old == nil) {
		return errors.New("configuration changed concurrently: " + name)
	}
	if name == mainConfig && info != nil {
		stat := info.Sys().(*syscall.Stat_t)
		if err := file.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
			return err
		}
		mode = info.Mode().Perm()
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if info == nil {
		// link will fail if another writer has just created the destination.
		return root.Link(tmp, name)
	}
	return root.Rename(tmp, name)
}
