package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"syscall"
	"time"
)

type monitorReply struct {
	QMP    json.RawMessage
	Return json.RawMessage
	Error  json.RawMessage
	Event  json.RawMessage
}

func qmpPowerdown(conn net.Conn) error {
	dec, enc := json.NewDecoder(conn), json.NewEncoder(conn)
	var greeting monitorReply
	if err := dec.Decode(&greeting); err != nil {
		return err
	}
	if greeting.QMP == nil {
		return errors.New("invalid QMP greeting")
	}
	for _, command := range []string{"qmp_capabilities", "system_powerdown"} {
		if err := enc.Encode(map[string]string{"execute": command}); err != nil {
			return err
		}
		for {
			var reply monitorReply
			if err := dec.Decode(&reply); err != nil {
				return err
			}
			if reply.Error != nil {
				return fmt.Errorf("QMP %s: %s", command, reply.Error)
			}
			if reply.Return != nil {
				break
			}
			if reply.Event == nil {
				return fmt.Errorf("unexpected QMP reply to %s", command)
			}
		}
	}
	return nil
}

func guestPowerdown(conn net.Conn) error {
	dec := json.NewDecoder(conn)
	if _, err := io.WriteString(conn, "{\"execute\":\"guest-ping\"}\n"); err != nil {
		return err
	}
	var ping monitorReply
	if err := dec.Decode(&ping); err != nil {
		return err
	}
	if ping.Return == nil {
		return errors.New("guest agent did not acknowledge ping")
	}
	if _, err := io.WriteString(conn, "{\"execute\":\"guest-shutdown\",\"arguments\":{\"mode\":\"powerdown\"}}\n"); err != nil {
		return err
	}
	// Successful guest-shutdown sends no reply. An explicit error means
	// HAOS is not ready yet and the supervisor should retry.
	var reply monitorReply
	err := dec.Decode(&reply)
	if errors.Is(err, io.EOF) || errors.Is(err, os.ErrDeadlineExceeded) {
		return nil
	}
	if err != nil {
		return err
	}
	if reply.Error != nil {
		return fmt.Errorf("guest-shutdown: %s", reply.Error)
	}
	return nil
}

func monitorRequest(path string, timeout time.Duration, deadline time.Time, request func(net.Conn) error) error {
	end := time.Now().Add(timeout)
	if end.After(deadline) {
		end = deadline
	}
	ctx, cancel := context.WithDeadline(context.Background(), end)
	defer cancel()
	conn, err := new(net.Dialer).DialContext(ctx, "unix", path)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(end); err != nil {
		return err
	}
	return request(conn)
}

func shutdownVM(process *os.Process, done <-chan struct{}) {
	const grace = 460 * time.Second
	log.Printf("requesting guest shutdown, allowing %s", grace)
	deadline := time.Now().Add(grace)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	retry := time.NewTicker(time.Second)
	defer retry.Stop()
	var acpiSent, agentSent bool
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return
		default:
		}
		if !acpiSent {
			acpiSent = monitorRequest(qmpSocket, 2*time.Second, deadline, qmpPowerdown) == nil
			if acpiSent {
				log.Printf("requested ACPI poweroff through QMP")
			}
		}
		if !agentSent {
			agentSent = monitorRequest(agentSocket, time.Second, deadline, guestPowerdown) == nil
			if agentSent {
				log.Printf("requested poweroff through the HAOS guest agent")
			}
		}
		if agentSent && acpiSent {
			select {
			case <-done:
				return
			case <-timer.C:
			}
			break
		}
		select {
		case <-done:
			return
		case <-timer.C:
		case <-retry.C:
		}
	}
	log.Printf("guest shutdown timed out, terminating QEMU")
	process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		log.Printf("QEMU did not terminate, killing it")
		process.Kill()
		<-done
	}
}
