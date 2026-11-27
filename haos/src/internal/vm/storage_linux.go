package vm

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const imageVersion = "18.4"
const imageName = "haos_ova-" + imageVersion + ".qcow2.xz"

type vmStorage struct {
	state, private, legacy, mountInfo string
	imageURL, digest                  string
}

var files = vmStorage{
	state: stateDir, private: "/lzcapp/documents", legacy: "/lzcapp/document",
	mountInfo: "/proc/self/mountinfo",
	imageURL:  "https://github.com/home-assistant/operating-system/releases/download/" + imageVersion + "/" + imageName,
	digest:    "756ec485b1b153099ca53e88a5eebd63a801d56b791afe5c6d63ef94a35dc02d",
}

var ownerPattern = regexp.MustCompile(`^[\pL\pN_.@+-]+$`)

const dirFlags = syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW

func openAt(parent *os.File, name string, flags int) (*os.File, error) {
	path := filepath.Join(parent.Name(), name)
	fd, err := syscall.Openat(int(parent.Fd()), name, flags|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func openMountedOwner(root, owner string, mounted map[string]bool) (*os.File, error) {
	if !ownerPattern.MatchString(owner) || owner == "." || owner == ".." {
		return nil, errors.New("HAOS_OWNER must identify a single LazyCat document owner")
	}
	if !mounted[root] && !mounted[filepath.Join(root, owner)] {
		return nil, &os.PathError{Op: "open mounted documents", Path: root, Err: os.ErrNotExist}
	}
	dir, err := os.OpenFile(root, dirFlags, 0)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return openAt(dir, owner, dirFlags)
}

// With a saved layout, missing mounts and missing subdirectories must stop the
// VM. Creating a replacement would silently send backups to a different disk.
func (s vmStorage) documentDirectory(owner string, backup bool) (*os.File, error) {
	record := filepath.Join(s.state, "backup-layouts.json")
	layouts := map[string]string{}
	data, err := readRecord(record)
	if err == nil {
		if err := json.Unmarshal(data, &layouts); err != nil || layouts == nil {
			return nil, fmt.Errorf("invalid backup layouts in %s", record)
		}
		for _, name := range layouts {
			if name != "private" && name != "legacy" {
				return nil, fmt.Errorf("unknown backup layout %q in %s", name, record)
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	mounted, err := s.mountPoints()
	if err != nil {
		return nil, err
	}

	saved := layouts[owner]
	for _, layout := range []string{"private", "legacy"} {
		root := s.private
		var parts []string
		if layout == "legacy" {
			root = s.legacy
			parts = append(parts, "HAOS")
		}
		if saved != "" && saved != layout {
			continue
		}
		dir, err := openMountedOwner(root, owner, mounted)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if backup {
			parts = append(parts, "Backups")
		}
		child, err := openDocumentChildren(dir, parts, saved == "" && backup)
		dir.Close()
		if err != nil {
			return nil, err
		}
		if backup && saved == "" {
			layouts[owner] = layout
			if err := writeJSON(record, layouts); err != nil {
				child.Close()
				return nil, err
			}
		}
		if backup {
			log.Printf("backup directory: %s", child.Name())
		}
		return child, nil
	}
	if saved != "" {
		return nil, fmt.Errorf("saved %s document mount unavailable for %s", saved, owner)
	}
	return nil, fmt.Errorf("no mounted LazyCat documents directory for %s", owner)
}

func (s vmStorage) mountPoints() (map[string]bool, error) {
	info, err := os.ReadFile(s.mountInfo)
	if err != nil {
		return nil, err
	}
	mounted := make(map[string]bool)
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for line := range strings.SplitSeq(string(info), "\n") {
		if fields := strings.Fields(line); len(fields) >= 5 {
			mounted[unescape.Replace(fields[4])] = true
		}
	}
	return mounted, nil
}

func (s vmStorage) openDocumentExports(owner string) ([]*os.File, error) {
	mounted, err := s.mountPoints()
	if err != nil {
		return nil, err
	}
	docs, err := openMountedOwner(s.private, owner, mounted)
	if err != nil {
		return nil, fmt.Errorf("open application documents: %w", err)
	}
	defer docs.Close()

	var exports []*os.File
	for _, mount := range documentMounts {
		var dir *os.File
		if mount.directory == "Backups" {
			// Use the backup directory already recorded for this owner.
			dir, err = s.documentDirectory(owner, true)
		} else {
			dir, err = openDocumentChildren(docs, []string{mount.directory}, true)
		}
		if err == nil {
			exports = append(exports, dir)
			err = syscall.Access(fmt.Sprintf("/proc/self/fd/%d", dir.Fd()), 2)
		}
		if err != nil {
			for _, opened := range exports {
				opened.Close()
			}
			return nil, fmt.Errorf("open writable %s directory: %w", mount.directory, err)
		}
	}
	return exports, nil
}

// Return a separately owned descriptor, pinning each path component in turn.
func openDocumentChildren(parent *os.File, parts []string, create bool) (*os.File, error) {
	dir, err := openAt(parent, ".", dirFlags)
	if err != nil {
		return nil, err
	}
	var owner syscall.Stat_t
	if err := syscall.Fstat(int(dir.Fd()), &owner); err != nil {
		dir.Close()
		return nil, err
	}
	for _, part := range parts {
		created := false
		if create {
			err := syscall.Mkdirat(int(dir.Fd()), part, 0700)
			if err != nil && !errors.Is(err, syscall.EEXIST) {
				dir.Close()
				return nil, &os.PathError{Op: "mkdirat", Path: filepath.Join(dir.Name(), part), Err: err}
			}
			created = err == nil
		}
		child, err := openAt(dir, part, dirFlags)
		dir.Close()
		if err != nil {
			return nil, err
		}
		if created {
			if err := child.Chown(int(owner.Uid), int(owner.Gid)); err != nil {
				child.Close()
				return nil, err
			}
		}
		dir = child
	}
	return dir, nil
}

// O_PATH inspects the entry without ever opening a FIFO or device for I/O.
// Reopening the pinned regular inode also survives a rename in File Manager.
func openRegularAt(dir *os.File, name string) (*os.File, error) {
	const oPath = 0x200000 // Linux O_PATH is absent from syscall on amd64.
	entry, err := openAt(dir, name, oPath|syscall.O_NOFOLLOW)
	if err != nil {
		return nil, err
	}
	defer entry.Close()
	info, err := entry.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected a regular file: %s", entry.Name())
	}
	return os.Open(fmt.Sprintf("/proc/self/fd/%d", entry.Fd()))
}

func readRecord(path string) ([]byte, error) {
	dir, err := os.OpenFile(filepath.Dir(path), dirFlags, 0)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	f, err := openRegularAt(dir, filepath.Base(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func prepareDisk(ctx context.Context, owner string) (string, error) {
	return files.prepareDisk(ctx, owner)
}

func (s vmStorage) prepareDisk(ctx context.Context, owner string) (string, error) {
	disk := filepath.Join(s.state, "haos.qcow2")
	if exists, err := regularFileExists(disk); err != nil || exists {
		return disk, err
	}
	// Verify a private snapshot so changes in the user's documents cannot
	// alter the bytes passed to xz. The runtime holds instance.lock.
	archive, err := newStage(filepath.Join(s.state, "download.part"))
	if err != nil {
		return "", err
	}
	defer discardStage(archive)
	sum := sha256.New()
	name, err := s.copySeed(ctx, owner, archive, sum)
	if err != nil {
		return "", err
	}
	if fmt.Sprintf("%x", sum.Sum(nil)) != s.digest {
		return "", fmt.Errorf("SHA-256 mismatch for %s", name)
	}
	log.Printf("verified %s, unpacking disk", name)
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	unpacked, err := newStage(disk + ".part")
	if err != nil {
		return "", err
	}
	defer discardStage(unpacked)
	unzip := exec.CommandContext(ctx, "xz", "-dc")
	unzip.Stdin, unzip.Stdout, unzip.Stderr = archive, unpacked, os.Stderr
	if err := unzip.Run(); err != nil {
		return "", fmt.Errorf("unpacking %s: %w", name, err)
	}
	check := exec.CommandContext(ctx, "qemu-img", "check", "-q", "-f", "qcow2", unpacked.Name())
	check.Stderr = os.Stderr
	if err := check.Run(); err != nil {
		return "", fmt.Errorf("checking imported disk: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := unpacked.Sync(); err != nil {
		return "", err
	}
	if err := os.Link(unpacked.Name(), disk); err != nil {
		return "", err
	}
	return disk, syncDirectory(s.state)
}

type cancelReader struct {
	ctx context.Context
	src io.Reader
}

func (r cancelReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.src.Read(p)
}

func (s vmStorage) copySeed(ctx context.Context, owner string, dst *os.File, sum hash.Hash) (string, error) {
	docs, err := s.documentDirectory(owner, false)
	if err != nil {
		return "", err
	}
	defer docs.Close()
	images, err := openDocumentChildren(docs, []string{"Images"}, true)
	if err != nil {
		return "", err
	}
	defer images.Close()
	src, err := openRegularAt(images, imageName)
	if os.IsNotExist(err) {
		if err := s.downloadDocumentImage(ctx, images); err != nil {
			return "", err
		}
		src, err = openRegularAt(images, imageName)
	}
	if err != nil {
		return "", err
	}
	defer src.Close()
	name := filepath.Join(images.Name(), imageName)
	log.Printf("reading %s", name)
	_, err = io.Copy(io.MultiWriter(dst, sum), cancelReader{ctx, src})
	return name, err
}

func (s vmStorage) downloadDocumentImage(ctx context.Context, images *os.File) error {
	name := filepath.Join(images.Name(), imageName)
	log.Printf("downloading official image to %s", name)
	// Keep publication on the pinned document filesystem even if Images is renamed.
	dir := fmt.Sprintf("/proc/self/fd/%d", images.Fd())
	stage, err := os.CreateTemp(dir, "."+imageName+"-*.part")
	if err != nil {
		return err
	}
	defer discardStage(stage)
	sum := sha256.New()
	if err := downloadSeed(ctx, s.imageURL, stage, sum); err != nil {
		return err
	}
	if fmt.Sprintf("%x", sum.Sum(nil)) != s.digest {
		return fmt.Errorf("SHA-256 mismatch for download from %s", s.imageURL)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var owner syscall.Stat_t
	if err := syscall.Fstat(int(images.Fd()), &owner); err != nil {
		return err
	}
	if err := stage.Chown(int(owner.Uid), int(owner.Gid)); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	// Never replace an archive the user uploaded while the download was running.
	if err := os.Link(stage.Name(), filepath.Join(dir, imageName)); err != nil && !os.IsExist(err) {
		return fmt.Errorf("publishing %s: %w", name, err)
	}
	return images.Sync()
}

func downloadSeed(ctx context.Context, url string, dst *os.File, sum hash.Hash) error {
	client := http.Client{
		Timeout: 1 * time.Hour,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || len(via) >= 10 {
				return errors.New("invalid HTTPS redirect for HAOS download")
			}
			return nil
		},
	}
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(request)
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				_, err = io.Copy(io.MultiWriter(dst, sum), resp.Body)
			} else {
				err = fmt.Errorf("HAOS download: %s", resp.Status)
			}
			resp.Body.Close()
			if resp.StatusCode < 500 && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests {
				return err
			}
		}
		if err == nil || attempt == 3 || ctx.Err() != nil {
			return err
		}
		log.Printf("HAOS download failed, retrying: %v", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second << attempt):
		}
		if err := dst.Truncate(0); err != nil {
			return err
		}
		if _, err := dst.Seek(0, io.SeekStart); err != nil {
			return err
		}
		sum.Reset()
	}
}

func regularFileExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("expected a regular file: %s", path)
	}
	return true, nil
}

func prepareFirmware() (code, vars string, err error) {
	return firmwarePaths("/usr/share/OVMF", files.state)
}

func firmwarePaths(firmware, state string) (string, string, error) {
	code := filepath.Join(firmware, "OVMF_CODE_4M.fd")
	if info, err := os.Stat(code); err != nil || !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("missing OVMF firmware: %s", code)
	}
	const name = "OVMF_VARS_4M.fd"
	vars := filepath.Join(state, name)
	exists, err := regularFileExists(vars)
	if err != nil || exists {
		return code, vars, err
	}
	template, err := os.Open(filepath.Join(firmware, name))
	if err != nil {
		return "", "", err
	}
	defer template.Close()
	if err := replaceStateFile(vars, template); err != nil {
		return "", "", err
	}
	return code, vars, nil
}

func persistentMACs() (lan, management string, err error) {
	return storedMACs(filepath.Join(files.state, "macs.json"))
}

func storedMACs(path string) (lan, management string, err error) {
	type addresses struct {
		LAN        string `json:"lan"`
		Management string `json:"management"`
	}
	var saved addresses
	data, err := readRecord(path)
	switch {
	case os.IsNotExist(err):
		newMAC := func(prefix byte) string {
			raw := net.HardwareAddr{prefix, 0, 0, 0, 0, 0}
			rand.Read(raw[1:])
			return raw.String()
		}
		saved = addresses{newMAC(0x02), newMAC(0x06)}
		if err := writeJSON(path, saved); err != nil {
			return "", "", err
		}
	case err != nil:
		return "", "", err
	default:
		if err := json.Unmarshal(data, &saved); err != nil {
			return "", "", err
		}
	}
	for _, value := range []string{saved.LAN, saved.Management} {
		addr, err := net.ParseMAC(value)
		if err != nil || len(addr) != 6 || addr[0]&1 != 0 || !strings.EqualFold(addr.String(), value) {
			return "", "", fmt.Errorf("invalid unicast MAC in %s", path)
		}
	}
	if strings.EqualFold(saved.LAN, saved.Management) {
		return "", "", fmt.Errorf("duplicate MACs in %s", path)
	}
	return saved.LAN, saved.Management, nil
}

// The caller holds the VM instance lock for all state-file writes.
func newStage(path string) (*os.File, error) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
}

func discardStage(stage *os.File) {
	stage.Close()
	os.Remove(stage.Name())
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return replaceStateFile(path, bytes.NewReader(append(data, '\n')))
}

func replaceStateFile(path string, contents io.Reader) error {
	stage, err := newStage(path + ".part")
	if err != nil {
		return err
	}
	defer discardStage(stage)
	if _, err := io.Copy(stage, contents); err != nil {
		return err
	}
	if err := stage.Sync(); err != nil {
		return err
	}
	if err := os.Rename(stage.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
