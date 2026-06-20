package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPkgmStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/app/instance/status" || r.URL.Query().Get("id") != "x" {
			t.Errorf("unexpected req: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Write([]byte(`{"status":8,"deploy":{"deploy_id":"x"}}`))
	}))
	defer srv.Close()
	p := &Pkgm{Base: srv.URL + "/api/app", HTTP: srv.Client()}
	got, err := p.Status("x")
	if err != nil || got != 8 {
		t.Fatalf("Status=%d err=%v want 8,nil", got, err)
	}
}

func TestPkgmResumeIgnores400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/app/instance/resume" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		http.Error(w, `{"error":"instance not found"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	p := &Pkgm{Base: srv.URL + "/api/app", HTTP: srv.Client()}
	if err := p.Resume("cloud.lazycat.lightos.entry", "moon"); err != nil {
		t.Fatalf("Resume should ignore 400, got err=%v", err)
	}
}

func TestPkgmResumeFailsOn500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := &Pkgm{Base: srv.URL + "/api/app", HTTP: srv.Client()}
	if err := p.Resume("x", "moon"); err == nil {
		t.Fatalf("Resume should fail on 500")
	}
}
