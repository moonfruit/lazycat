package main

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	udpIdleTTL      = 60 * time.Second
	udpMaxSessions  = 4096
	udpScanInterval = 10 * time.Second
	udpReadBufBytes = 64 * 1024
)

type sessionKey struct {
	client      string
	publishPort uint16
}

type udpSession struct {
	upstream     *net.UDPConn
	lastActivity time.Time
}

type udpSessionTable struct {
	mu       sync.Mutex
	sessions map[sessionKey]*udpSession
	nowFunc  func() time.Time
	maxSize  int
}

func newUDPSessionTable() *udpSessionTable {
	return &udpSessionTable{
		sessions: make(map[sessionKey]*udpSession),
		nowFunc:  time.Now,
		maxSize:  udpMaxSessions,
	}
}

func (t *udpSessionTable) get(k sessionKey) (*udpSession, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[k]
	if ok {
		s.lastActivity = t.nowFunc()
	}
	return s, ok
}

func (t *udpSessionTable) touch(k sessionKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.sessions[k]; ok {
		s.lastActivity = t.nowFunc()
	}
}

func (t *udpSessionTable) put(k sessionKey, s *udpSession) (evictedKey sessionKey, evicted *udpSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s.lastActivity = t.nowFunc()
	if len(t.sessions) >= t.maxSize {
		for kk, ss := range t.sessions {
			if evicted == nil || ss.lastActivity.Before(evicted.lastActivity) {
				evicted = ss
				evictedKey = kk
			}
		}
		if evicted != nil {
			delete(t.sessions, evictedKey)
		}
	}
	t.sessions[k] = s
	return
}

func (t *udpSessionTable) remove(k sessionKey) *udpSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[k]
	if !ok {
		return nil
	}
	delete(t.sessions, k)
	return s
}

func (t *udpSessionTable) reapExpired(ttl time.Duration) []*udpSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.nowFunc()
	var removed []*udpSession
	for k, s := range t.sessions {
		if now.Sub(s.lastActivity) > ttl {
			removed = append(removed, s)
			delete(t.sessions, k)
		}
	}
	return removed
}

func (t *udpSessionTable) size() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}

func runUDP(listenPort int, targetIP string) error {
	pc, err := net.ListenPacket("udp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(listenPort)))
	if err != nil {
		return fmt.Errorf("udp listen :%d failed: %w", listenPort, err)
	}
	defer pc.Close()
	fmt.Printf("UDP TARGET IP IS %v LISTEN PORT IS:%d\n", targetIP, listenPort)

	tbl := newUDPSessionTable()
	go udpReaper(tbl)

	buf := make([]byte, udpReadBufBytes)
	for {
		n, clientAddr, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			fmt.Printf("udp readfrom error: %v\n", err)
			continue
		}
		ua, ok := clientAddr.(*net.UDPAddr)
		if !ok || ua.Port == 0 {
			continue
		}
		payload := make([]byte, n)
		copy(payload, buf[:n])
		handleUDPDatagram(pc, tbl, ua, payload, targetIP)
	}
}

func udpReaper(tbl *udpSessionTable) {
	ticker := time.NewTicker(udpScanInterval)
	defer ticker.Stop()
	for range ticker.C {
		for _, s := range tbl.reapExpired(udpIdleTTL) {
			s.upstream.Close()
		}
	}
}

func handleUDPDatagram(pc net.PacketConn, tbl *udpSessionTable, clientAddr *net.UDPAddr, payload []byte, targetIP string) {
	publishPort := uint16(clientAddr.Port)
	k := sessionKey{client: clientAddr.IP.String(), publishPort: publishPort}
	s, ok := tbl.get(k)
	if !ok {
		conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP(targetIP), Port: int(publishPort)})
		if err != nil {
			fmt.Printf("udp dial %s:%d failed: %v\n", targetIP, publishPort, err)
			return
		}
		s = &udpSession{upstream: conn}
		if _, evicted := tbl.put(k, s); evicted != nil {
			evicted.upstream.Close()
		}
		go reverseUDPLoop(pc, tbl, conn, clientAddr, k)
		fmt.Printf("BEGIN UDP FORWARD %v -> %s:%d\n", clientAddr, targetIP, publishPort)
	}
	if _, err := s.upstream.Write(payload); err != nil {
		fmt.Printf("udp upstream write %v failed: %v\n", k, err)
		if removed := tbl.remove(k); removed != nil {
			removed.upstream.Close()
		}
	}
}

func reverseUDPLoop(pc net.PacketConn, tbl *udpSessionTable, upstream *net.UDPConn, clientAddr *net.UDPAddr, k sessionKey) {
	rbuf := make([]byte, udpReadBufBytes)
	for {
		n, _, err := upstream.ReadFromUDP(rbuf)
		if err != nil {
			return
		}
		if _, err := pc.WriteTo(rbuf[:n], clientAddr); err != nil {
			fmt.Printf("udp writeback %v failed: %v\n", k, err)
			if removed := tbl.remove(k); removed != nil {
				removed.upstream.Close()
			}
			return
		}
		tbl.touch(k)
	}
}
