package vm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Run starts QEMU and returns its exit status after shutdown.
func Run(ctx context.Context) (int, error) {
	syscall.Umask(0077)
	c, err := readConfig()
	if err != nil {
		return 0, err
	}
	lan, bridge, err := networkInterfaces(ctx)
	if err != nil {
		return 0, setupError(ctx, err)
	}
	c.lan, c.bridge = lan.Name, bridge
	if !c.host.IsValid() {
		c.host, err = parentIPv4(lan)
		if err != nil {
			log.Printf("cannot detect HostIP (%v); continuing without a host route. Set HostIP to override", err)
		} else {
			log.Printf("detected HostIP %s on the macvlan parent", c.host)
		}
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return 0, err
	}
	lock, err := os.OpenFile(filepath.Join(stateDir, "instance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return 0, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return 0, fmt.Errorf("another HAOS runtime holds %s", lock.Name())
		}
		return 0, err
	}
	owner := os.Getenv("HAOS_OWNER")
	exports, err := files.openDocumentExports(owner)
	if err != nil {
		return 0, err
	}
	for _, dir := range exports {
		defer dir.Close()
	}
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return 0, err
	}
	for _, path := range []string{qmpSocket, agentSocket, serialSocket} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}
	if c.disk, err = prepareDisk(ctx, owner); err != nil {
		return 0, setupError(ctx, err)
	}
	if c.code, c.variables, err = prepareFirmware(); err != nil {
		return 0, err
	}
	if c.lanMAC, c.managementMAC, err = persistentMACs(); err != nil {
		return 0, err
	}
	if err := configureLAN(ctx, c.lan, c.lanMAC); err != nil {
		return 0, setupError(ctx, err)
	}
	if c.oidc != nil {
		defer os.RemoveAll(oidcExportDir)
		if err := c.prepareOIDCExport(oidcExportDir, "/lzcapp/pkg/content"); err != nil {
			log.Printf("could not prepare OIDC guest files, continuing with local authentication: %v", err)
			c.oidc = nil
		}
	}
	if ctx.Err() != nil {
		return 0, nil
	}
	log.Printf("LAN %s %s, management enp0s2 %s, hostfwd %s:8123 -> %s15:80, trusted proxy network %s", c.lan, c.lanMAC, c.managementMAC, c.bridge.Addr(), c.managementPrefix(), c.bridge.Masked())
	// QEMU's 9p server creates files for the document owner. All private
	// state files have already been prepared under the restrictive umask.
	syscall.Umask(0022)
	args, err := c.qemuArgs()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command("qemu-system-x86_64", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	// os/exec passes ExtraFiles[0] as fd 3 even when the parent's fd differs.
	cmd.ExtraFiles = exports
	return supervise(ctx, cmd)
}

func setupError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func supervise(ctx context.Context, cmd *exec.Cmd) (int, error) {
	if ctx.Err() != nil {
		return 0, nil
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	vmDone := make(chan struct{})
	var waitErr error
	go func() { waitErr = cmd.Wait(); close(vmDone) }()

	select {
	case <-vmDone:
		if state := cmd.ProcessState; state != nil {
			status := state.ExitCode()
			if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				status = 128 + int(ws.Signal())
			}
			log.Printf("QEMU exited with status %d", status)
			return status, nil
		}
		return 0, waitErr
	case <-ctx.Done():
	}
	shutdownVM(cmd.Process, vmDone)
	log.Printf("QEMU exited after shutdown: %v", cmd.ProcessState)
	return 0, nil
}
