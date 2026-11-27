package oidc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCredentialWatch(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "homeassistant")
	private := filepath.Join(home, privateConfig)
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(name, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	makeConfig := func() {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(private), 0700); err != nil {
			t.Fatal(err)
		}
		write(private)
	}
	rename := func(from, to string) {
		t.Helper()
		if err := os.Rename(from, to); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(changed bool, operation func()) {
		t.Helper()
		w, err := openCredentialWatch(base)
		if err != nil {
			t.Fatal(err)
		}
		defer w.file.Close()
		operation()
		got, err := w.wait(100 * time.Millisecond)
		if err != nil || got != changed {
			t.Fatalf("change=%v want=%v err=%v", got, changed, err)
		}
	}
	expect(true, makeConfig) // Even when no config directory existed at startup.
	expect(false, func() { write(filepath.Join(home, "home-assistant_v2.db")) })
	expect(true, func() { write(private) })
	expect(true, func() {
		write(private + ".tmp")
		rename(private+".tmp", private)
	})
	expect(true, func() {
		rename(filepath.Dir(private), filepath.Join(home, "old-provider"))
		makeConfig()
	})
	expect(true, func() {
		rename(home, filepath.Join(base, "old-config"))
		makeConfig()
	})
	expect(true, func() { write(private) }) // The replacement is now watched.
}
