package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

func TestQMPPowerdownAcceptsInterleavedEvents(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		server.SetDeadline(time.Now().Add(3 * time.Second))
		enc, dec := json.NewEncoder(server), json.NewDecoder(server)
		enc.Encode(map[string]any{"QMP": map[string]any{}})
		for _, want := range []string{"qmp_capabilities", "system_powerdown"} {
			var req struct{ Execute string }
			if err := dec.Decode(&req); err != nil || req.Execute != want {
				t.Errorf("got %q (%v), want %q", req.Execute, err, want)
				return
			}
			enc.Encode(map[string]string{"event": "POWERDOWN"})
			enc.Encode(map[string]any{"return": map[string]any{}})
		}
	}()
	if err := qmpPowerdown(client); err != nil {
		t.Fatal(err)
	}
}

func TestGuestShutdownIsAcknowledgedByDisconnect(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	go func() {
		defer server.Close()
		server.SetDeadline(time.Now().Add(3 * time.Second))
		reader := bufio.NewReader(server)
		for i, want := range []string{"guest-ping", "guest-shutdown"} {
			line, err := reader.ReadString('\n')
			var req struct {
				Execute   string
				Arguments struct{ Mode string }
			}
			if err != nil || json.Unmarshal([]byte(line), &req) != nil || req.Execute != want {
				t.Errorf("unexpected guest request %q, %v", line, err)
				return
			}
			if i == 0 {
				io.WriteString(server, "{\"return\":{}}\n")
			} else if req.Arguments.Mode != "powerdown" {
				t.Errorf("bad shutdown mode %q", req.Arguments.Mode)
			}
		}
	}()
	if err := guestPowerdown(client); err != nil {
		t.Fatal(err)
	}
}
