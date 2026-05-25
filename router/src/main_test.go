package main

import (
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func TestSessionTable_PutGet(t *testing.T) {
	tbl := newUDPSessionTable()
	clock := &fakeClock{now: time.Unix(1000, 0)}
	tbl.nowFunc = clock.Now

	k := sessionKey{client: "1.2.3.4", publishPort: 80}
	s := &udpSession{}
	tbl.put(k, s)

	got, ok := tbl.get(k)
	if !ok || got != s {
		t.Fatalf("expected to find session, got %v %v", got, ok)
	}
}

func TestSessionTable_Touch(t *testing.T) {
	tbl := newUDPSessionTable()
	clock := &fakeClock{now: time.Unix(1000, 0)}
	tbl.nowFunc = clock.Now

	k := sessionKey{client: "a", publishPort: 1}
	s := &udpSession{}
	tbl.put(k, s)
	t0 := s.lastActivity

	clock.now = clock.now.Add(5 * time.Second)
	tbl.touch(k)
	if !s.lastActivity.After(t0) {
		t.Errorf("touch did not refresh lastActivity")
	}
}

func TestSessionTable_ReapExpired(t *testing.T) {
	tbl := newUDPSessionTable()
	clock := &fakeClock{now: time.Unix(1000, 0)}
	tbl.nowFunc = clock.Now

	tbl.put(sessionKey{client: "a", publishPort: 1}, &udpSession{})
	clock.now = clock.now.Add(30 * time.Second)
	tbl.put(sessionKey{client: "b", publishPort: 2}, &udpSession{})

	clock.now = clock.now.Add(45 * time.Second)
	removed := tbl.reapExpired(60 * time.Second)
	if len(removed) != 1 {
		t.Errorf("expected 1 expired, got %d", len(removed))
	}
	if tbl.size() != 1 {
		t.Errorf("expected 1 remaining, got %d", tbl.size())
	}
}

func TestSessionTable_LRUEviction(t *testing.T) {
	tbl := newUDPSessionTable()
	clock := &fakeClock{now: time.Unix(1000, 0)}
	tbl.nowFunc = clock.Now
	tbl.maxSize = 3

	for i := 0; i < 3; i++ {
		tbl.put(sessionKey{client: "x", publishPort: uint16(i)}, &udpSession{})
		clock.now = clock.now.Add(time.Second)
	}
	_, evicted := tbl.put(sessionKey{client: "x", publishPort: 99}, &udpSession{})
	if evicted == nil {
		t.Fatal("expected eviction")
	}
	if _, ok := tbl.get(sessionKey{client: "x", publishPort: 0}); ok {
		t.Error("expected port=0 to be evicted")
	}
	if tbl.size() != 3 {
		t.Errorf("size = %d, want 3", tbl.size())
	}
}
