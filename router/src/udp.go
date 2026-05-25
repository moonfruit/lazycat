package main

import (
	"net"
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
