package vm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

func fixtureStorage(t *testing.T, layout string) vmStorage {
	t.Helper()
	base := t.TempDir()
	s := vmStorage{
		state: filepath.Join(base, "state"), private: filepath.Join(base, "documents"),
		legacy:    filepath.Join(base, "document"),
		mountInfo: filepath.Join(base, "mountinfo"),
	}
	for _, dir := range []string{s.state, filepath.Join(s.private, "alice"), filepath.Join(s.legacy, "alice")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	mount := map[string]string{"private": s.private, "legacy": filepath.Join(s.legacy, "alice")}[layout]
	writeFixture(t, s.mountInfo, []byte("1 2 0:0 / "+mount+" rw - tmpfs tmpfs rw\n"))
	return s
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestBackupPinsLayoutAndDirectory(t *testing.T) {
	for _, layout := range []string{"private", "legacy"} {
		t.Run(layout, func(t *testing.T) {
			s := fixtureStorage(t, layout)
			fd, err := s.documentDirectory("alice", true)
			if err != nil {
				t.Fatal(err)
			}
			defer fd.Close()
			parent := filepath.Join(s.private, "alice")
			if layout == "legacy" {
				parent = filepath.Join(s.legacy, "alice", "HAOS")
			}
			path := filepath.Join(parent, "Backups")
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(path, path+"-moved"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), path); err != nil {
				t.Fatal(err)
			}
			after, err := fd.Stat()
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("directory handle moved: %v", err)
			}
			if _, err := s.documentDirectory("alice", true); err == nil {
				t.Fatal("followed replacement symlink")
			}
			writeFixture(t, s.mountInfo, []byte("1 2 0:0 / / rw - tmpfs tmpfs rw\n"))
			if _, err := s.documentDirectory("alice", true); err == nil {
				t.Fatal("accepted missing bind mount")
			}
		})
	}
}

func TestStorageRejectsUnsafeOwnersAndArchives(t *testing.T) {
	s := fixtureStorage(t, "private")
	for _, owner := range []string{"", "..", "../alice", "/alice"} {
		if _, err := s.documentDirectory(owner, true); err == nil {
			t.Errorf("accepted owner %q", owner)
		}
	}
	images := filepath.Join(s.private, "alice", "Images")
	if err := os.Mkdir(images, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(images, imageName)
	if err := os.Symlink("/nonexistent", target); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"symlink", "fifo"} {
		if kind == "fifo" {
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(target, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.prepareDisk(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), target) {
			t.Fatalf("expected rejection of %s at %s, got %v", kind, target, err)
		}
	}
}

func TestDiskImportAndReuse(t *testing.T) {
	for _, tool := range []string{"qemu-img", "xz"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("disk import requires %s", tool)
		}
	}
	raw := filepath.Join(t.TempDir(), "tiny.qcow2")
	if out, err := exec.Command("qemu-img", "create", "-f", "qcow2", raw, "1M").CombinedOutput(); err != nil {
		t.Fatalf("create qcow2: %s %v", out, err)
	}
	archive, err := exec.Command("xz", "-c", raw).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"private", "legacy", "download"} {
		t.Run(source, func(t *testing.T) {
			layout := "private"
			if source == "legacy" {
				layout = "legacy"
			}
			s := fixtureStorage(t, layout)
			fd, err := s.documentDirectory("alice", true)
			if err != nil {
				t.Fatal(err)
			}
			fd.Close()
			var expectedRequests int32
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Write(archive)
			}))
			defer server.Close()
			s.imageURL = server.URL
			s.digest = fmt.Sprintf("%x", sha256.Sum256(archive))
			parent := filepath.Join(s.private, "alice")
			if source == "legacy" {
				parent = filepath.Join(s.legacy, "alice", "HAOS")
			}
			images := filepath.Join(parent, "Images")
			provided := filepath.Join(images, imageName)
			if source == "download" {
				expectedRequests = 1
				writeFixture(t, filepath.Join(parent, imageName), []byte("old location"))
			} else {
				if err := os.Mkdir(images, 0700); err != nil {
					t.Fatal(err)
				}
				writeFixture(t, provided, archive)
			}
			disk, err := s.prepareDisk(context.Background(), "alice")
			if err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command("qemu-img", "compare", "-f", "qcow2", "-F", "qcow2", raw, disk).CombinedOutput(); err != nil {
				t.Fatalf("imported disk differs: %v %s", err, out)
			}
			got, err := os.ReadFile(provided)
			if err != nil || !bytes.Equal(got, archive) {
				t.Fatalf("document image unavailable for reuse: %v", err)
			}
			if err := os.Remove(disk); err != nil {
				t.Fatal(err)
			}
			if _, err := s.prepareDisk(context.Background(), "alice"); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != expectedRequests {
				t.Fatalf("HTTP requests after reinitialization: %d, want %d", requests.Load(), expectedRequests)
			}
			writeFixture(t, provided, []byte("damaged user archive"))
			if reused, err := s.prepareDisk(context.Background(), "alice"); err != nil || reused != disk {
				t.Fatalf("did not reuse persistent disk: %v %s", err, reused)
			}
			if err := os.Remove(disk); err != nil {
				t.Fatal(err)
			}
			if _, err := s.prepareDisk(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), "SHA-256") || !strings.Contains(err.Error(), provided) {
				t.Fatalf("should reject invalid user archive: %v", err)
			}
			if contents, err := os.ReadFile(provided); err != nil || string(contents) != "damaged user archive" {
				t.Fatalf("user archive modified: %v", err)
			}
			if _, err := os.Stat(disk); !os.IsNotExist(err) || requests.Load() != expectedRequests {
				t.Fatalf("disk stat %v, HTTP requests %d", err, requests.Load())
			}
		})
	}
}

func TestIncompleteDownloadLeavesImagesEmpty(t *testing.T) {
	for _, failure := range []string{"checksum", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			s := fixtureStorage(t, "private")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, "partial archive")
				if failure == "cancel" {
					w.(http.Flusher).Flush()
					cancel()
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			s.imageURL = server.URL
			_, err := s.prepareDisk(ctx, "alice")
			if failure == "checksum" && (err == nil || !strings.Contains(err.Error(), "SHA-256")) {
				t.Fatalf("expected checksum error, got %v", err)
			}
			if failure == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("expected interrupted download, got %v", err)
			}
			for _, dir := range []string{s.state, filepath.Join(s.private, "alice", "Images")} {
				if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
					t.Fatalf("expected empty %s, got %v, %v", dir, entries, err)
				}
			}
		})
	}
}

func TestDownloadPreservesConcurrentUpload(t *testing.T) {
	s := fixtureStorage(t, "private")
	name := filepath.Join(s.private, "alice", "Images", imageName)
	const userFile = "concurrent user upload"
	const officialFile = "complete release"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(name, []byte(userFile), 0600); err != nil {
			t.Error(err)
		}
		io.WriteString(w, officialFile)
	}))
	defer server.Close()
	s.imageURL = server.URL
	s.digest = fmt.Sprintf("%x", sha256.Sum256([]byte(officialFile)))
	if _, err := s.prepareDisk(context.Background(), "alice"); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch for "+name) {
		t.Fatalf("expected check of concurrently uploaded file, got %v", err)
	}
	if contents, err := os.ReadFile(name); err != nil || string(contents) != userFile {
		t.Fatalf("concurrent upload changed: %q, %v", contents, err)
	}
}

func TestExistingVMIdentityAndFirmware(t *testing.T) {
	s := fixtureStorage(t, "private")
	macs := filepath.Join(s.state, "macs.json")
	lan, management, err := storedMACs(macs)
	if err != nil || !strings.HasPrefix(lan, "02:") || !strings.HasPrefix(management, "06:") {
		t.Fatalf("failed to persist newly generated MACs: %s %s %v", lan, management, err)
	}
	lanAgain, managementAgain, err := storedMACs(macs)
	if err != nil || lanAgain != lan || managementAgain != management {
		t.Fatalf("MACs changed across boots: %s %s %v", lanAgain, managementAgain, err)
	}
	original := []byte("{\"lan\":\"02:BE:00:01:00:10\", \"management\": \"06:10:ff:20:00:02\"}\n")
	writeFixture(t, macs, original)
	lan, management, err = storedMACs(macs)
	if err != nil || lan != "02:BE:00:01:00:10" || management != "06:10:ff:20:00:02" {
		t.Fatalf("did not reuse original Python MACs: %s %s %v", lan, management, err)
	}
	firmware := t.TempDir()
	writeFixture(t, filepath.Join(firmware, "OVMF_CODE_4M.fd"), []byte("code"))
	writeFixture(t, filepath.Join(firmware, "OVMF_VARS_4M.fd"), []byte("factory vars"))
	code, vars, err := firmwarePaths(firmware, s.state)
	if err != nil || code != filepath.Join(firmware, "OVMF_CODE_4M.fd") || vars != filepath.Join(s.state, "OVMF_VARS_4M.fd") {
		t.Fatalf("initial firmware: %s %s %v", code, vars, err)
	}
	if got, err := os.ReadFile(vars); err != nil || string(got) != "factory vars" {
		t.Fatalf("initial variables: %q %v", got, err)
	}
	writeFixture(t, vars, []byte("guest state"))
	if _, _, err := firmwarePaths(firmware, s.state); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(vars); err != nil || string(got) != "guest state" {
		t.Fatalf("persisted guest variables: %q %v", got, err)
	}
}

func TestStateRecordsRejectSymlinks(t *testing.T) {
	s := fixtureStorage(t, "private")
	data := filepath.Join(t.TempDir(), "record.json")
	writeFixture(t, data, []byte(`{"lan":"02:11:22:33:44:55","management":"06:11:22:33:44:55"}`))
	macs := filepath.Join(s.state, "macs.json")
	if err := os.Symlink(data, macs); err != nil {
		t.Fatal(err)
	}
	if _, _, err := storedMACs(macs); err == nil {
		t.Fatal("followed a MAC state symlink")
	}
	layouts := filepath.Join(s.state, "backup-layouts.json")
	writeFixture(t, data, []byte(`{}`))
	if err := os.Symlink(data, layouts); err != nil {
		t.Fatal(err)
	}
	if fd, err := s.documentDirectory("alice", true); err == nil {
		fd.Close()
		t.Fatal("followed a backup layout state symlink")
	}
}

func TestCancelledImportLeavesDiskUninitialized(t *testing.T) {
	s := fixtureStorage(t, "private")
	s.imageURL = "http://127.0.0.1:1"
	images := filepath.Join(s.private, "alice", "Images")
	if err := os.Mkdir(images, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(images, imageName)
	writeFixture(t, path, []byte("provided image"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.prepareDisk(ctx, "alice"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled import: %v", err)
	}
	for _, name := range []string{"haos.qcow2", "haos.qcow2.part", "download.part"} {
		if _, err := os.Lstat(filepath.Join(s.state, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected %s after cancel: %v", name, err)
		}
	}
}

func TestBackupRequiresActualDocumentMount(t *testing.T) {
	s := fixtureStorage(t, "private")
	s.mountInfo = "/proc/self/mountinfo"
	if dir, err := s.documentDirectory("alice", true); err == nil {
		dir.Close()
		t.Fatal("used an ordinary container directory as a document mount")
	}
	s.private = "/dev/shm" // A real, isolated tmpfs mount in the runner container.
	owner, err := os.MkdirTemp(s.private, "haos-documents-")
	if err != nil {
		t.Skipf("no writable /dev/shm mount: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(owner) })
	dir, err := s.documentDirectory(filepath.Base(owner), true)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if dir.Name() != filepath.Join(owner, "Backups") {
		t.Fatalf("unexpected backup directory %q", dir.Name())
	}
}

func TestSavedLegacyLayoutSurvivesNewPrivateMount(t *testing.T) {
	s := fixtureStorage(t, "private")
	writeFixture(t, filepath.Join(s.state, "backup-layouts.json"), []byte("{\"alice\": \"legacy\"}\n"))
	if dir, err := s.documentDirectory("alice", true); err == nil {
		dir.Close()
		t.Fatal("quietly switched an existing user to private documents")
	}
	if _, err := os.Stat(filepath.Join(s.private, "alice", "Backups")); !os.IsNotExist(err) {
		t.Fatalf("unexpected replacement backup directory: %v", err)
	}
}

func TestDownloadRetriesFromZero(t *testing.T) {
	const image = "complete release image"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(image)))
		if requests.Add(1) == 1 {
			io.WriteString(w, "cut")
			return
		}
		io.WriteString(w, image)
	}))
	defer server.Close()
	dst, err := os.CreateTemp(t.TempDir(), "download")
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	sum := sha256.New()
	if err := downloadSeed(context.Background(), server.URL, dst, sum); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(image))
	got, err := os.ReadFile(dst.Name())
	if err != nil || string(got) != image || !bytes.Equal(sum.Sum(nil), want[:]) || requests.Load() != 2 {
		t.Fatalf("partial download was not replaced: %q %x, requests %d, error %v", got, sum.Sum(nil), requests.Load(), err)
	}
}

func TestAppDocumentExports(t *testing.T) {
	s := fixtureStorage(t, "private")
	docs := filepath.Join(s.private, "alice")
	if err := os.Mkdir(filepath.Join(docs, "Media"), 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(docs, "Media", "song"), []byte("pre-existing"))

	args, err := (vmConfig{}).qemuArgs()
	if err != nil {
		t.Fatal(err)
	}
	var fstab string
	for _, arg := range args {
		if encoded, ok := strings.CutPrefix(arg, "type=11,value=io.systemd.credential.binary:fstab.extra="); ok {
			buf, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			fstab = string(buf)
		}
	}
	want := []struct{ name, tag, target string }{
		{"Backups", "haosbackup", "/mnt/data/supervisor/backup"},
		{"Media", "haosmedia", "/mnt/data/supervisor/media/Host"},
		{"Share", "haosshare", "/mnt/data/supervisor/share/Host"},
	}
	if strings.Count(fstab, "\n") != len(want) {
		t.Fatalf("unexpected document mounts: %s", fstab)
	}
	for boot := 0; boot < 2; boot++ {
		dirs, err := s.openDocumentExports("alice")
		if err != nil {
			t.Fatal(err)
		}
		if len(dirs) != len(want) {
			t.Fatalf("got %d directories", len(dirs))
		}
		for i, mount := range want {
			dir := dirs[i]
			if dir.Name() != filepath.Join(docs, mount.name) {
				t.Fatalf("%s: got descriptor for %s", mount.tag, dir.Name())
			}
			if !strings.Contains(fstab, mount.tag+" "+mount.target+" 9p rw,") {
				t.Errorf("wrong mount point or mode for %s: %s", mount.tag, fstab)
			}
			wantFS := fmt.Sprintf("local,id=%s,path=/proc/self/fd/%d,security_model=mapped-xattr,fmode=0644,dmode=0755", mount.tag, i+3)
			if !slices.Contains(args, wantFS) {
				t.Errorf("missing QEMU export %s", wantFS)
			}
			marker := filepath.Join(dir.Name(), "marker")
			if boot == 0 {
				writeFixture(t, marker, []byte(mount.name))
			} else if got, err := os.ReadFile(marker); err != nil || string(got) != mount.name {
				t.Errorf("%s lost contents across restart: %q %v", mount.name, got, err)
			}
			dir.Close()
		}
	}
	if got, err := os.ReadFile(filepath.Join(docs, "Media", "song")); err != nil || string(got) != "pre-existing" {
		t.Errorf("pre-existing media file: %q %v", got, err)
	}
}

func TestAppDocumentExportsRequirePrivateMount(t *testing.T) {
	s := fixtureStorage(t, "legacy")
	if dirs, err := s.openDocumentExports("alice"); err == nil || len(dirs) != 0 {
		t.Fatalf("used user documents for fixed application shares: %v %v", dirs, err)
	}
}

func TestAppDocumentExportsRejectSymlinks(t *testing.T) {
	for _, name := range []string{"Media", "Share"} {
		t.Run(name, func(t *testing.T) {
			s := fixtureStorage(t, "private")
			docs := filepath.Join(s.private, "alice")
			if err := os.Symlink(filepath.Join(docs, "Backups"), filepath.Join(docs, name)); err != nil {
				t.Fatal(err)
			}
			if dirs, err := s.openDocumentExports("alice"); err == nil || len(dirs) != 0 {
				t.Fatalf("accepted %s symlink to Backups: %v %v", name, dirs, err)
			}
		})
	}
}
