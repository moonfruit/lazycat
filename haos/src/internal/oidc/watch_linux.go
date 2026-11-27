package oidc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

type credentialWatch struct {
	file  *os.File
	names map[int32]string // Watch descriptor to the one child we care about.
}

func openCredentialWatch(base string) (*credentialWatch, error) {
	parent, err := os.Open(base)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return nil, err
	}
	w := &credentialWatch{file: os.NewFile(uintptr(fd), "oidc-inotify"), names: make(map[int32]string)}
	// Register each parent before looking up its child. A restore can rename
	// a fully populated directory into place without writing secrets.yaml.
	for _, child := range []string{"homeassistant", configDir, "secrets.yaml"} {
		mask := uint32(unix.IN_DELETE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO)
		if child == "secrets.yaml" {
			mask |= unix.IN_CLOSE_WRITE | unix.IN_ATTRIB
		} else {
			mask |= unix.IN_CREATE
		}
		wd, err := unix.InotifyAddWatch(fd, fmt.Sprintf("/proc/self/fd/%d", parent.Fd()), mask)
		if err != nil {
			w.file.Close()
			return nil, err
		}
		w.names[int32(wd)] = child
		if child == "secrets.yaml" {
			break
		}
		next, err := unix.Openat(int(parent.Fd()), child, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR) {
			break // The registered parent will report creation or replacement.
		}
		if err != nil {
			w.file.Close()
			return nil, err
		}
		parent = os.NewFile(uintptr(next), child)
		defer parent.Close()
	}
	return w, nil
}

// wait returns true for a relevant change and false when a scheduled retry is due.
// A zero delay leaves the descriptor blocked indefinitely.
func (w *credentialWatch) wait(delay time.Duration) (bool, error) {
	if delay > 0 {
		if err := w.file.SetReadDeadline(time.Now().Add(delay)); err != nil {
			return false, err
		}
	}
	var buf [4096]byte
	for {
		n, err := w.file.Read(buf[:])
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		for events := buf[:n]; len(events) > 0; {
			var event unix.InotifyEvent
			size, err := binary.Decode(events, binary.NativeEndian, &event)
			if err != nil || int(event.Len) > len(events)-size {
				return false, errors.New("invalid inotify event")
			}
			name := bytes.TrimRight(events[size:size+int(event.Len)], "\x00")
			if event.Mask&(unix.IN_Q_OVERFLOW|unix.IN_IGNORED|unix.IN_UNMOUNT) != 0 || string(name) == w.names[event.Wd] {
				return true, nil
			}
			events = events[size+int(event.Len):]
		}
	}
}

// Watch checks once at boot, then waits for readiness, a retry, or a restore.
func Watch() error {
	for {
		w, err := openCredentialWatch(supervisorDir)
		if err != nil {
			return err
		}
		// Install watches before reading the config so a concurrent restore
		// queues an event even if it happens during a slow Core check/restart.
		// Supervisor may still be restoring the rest of the tree after the
		// secret appears. Wait for it even when the partial tree looks unmanaged.
		delay := readinessInterval
		ready, err := haCore.supervisorRunning()
		if err == nil && ready {
			delay, err = runOnce(false)
		}
		if err != nil {
			log.Printf("OIDC provisioning: %v", err)
			if delay == 0 {
				// Keep our own rollback writes from triggering an immediate retry.
				w.file.Close()
				time.Sleep(retryInterval)
				continue
			}
		}
		_, err = w.wait(delay)
		w.file.Close()
		if err != nil {
			return err
		}
	}
}
