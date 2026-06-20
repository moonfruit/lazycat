package main

import (
	"path/filepath"
	"testing"
)

func TestIPCRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "agent.sock")
	srv, err := ServeIPC(sock, func(req Request) Response {
		if req.Action == "status" {
			return Response{OK: true, MacvtapLoaded: true, InstanceStatus: 8, Message: "ok"}
		}
		return Response{OK: false, Message: "unknown"}
	})
	if err != nil {
		t.Fatalf("ServeIPC: %v", err)
	}
	defer srv.Close()

	resp, err := CallIPC(sock, Request{Action: "status"})
	if err != nil {
		t.Fatalf("CallIPC: %v", err)
	}
	if !resp.OK || !resp.MacvtapLoaded || resp.InstanceStatus != 8 {
		t.Fatalf("unexpected resp: %+v", resp)
	}

	resp2, err := CallIPC(sock, Request{Action: "bogus"})
	if err != nil {
		t.Fatalf("CallIPC2: %v", err)
	}
	if resp2.OK {
		t.Fatalf("expected not-ok for bogus action")
	}
}
