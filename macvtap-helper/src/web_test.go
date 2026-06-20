package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebStatusPageRenders(t *testing.T) {
	h := webHandler(func(req Request) (Response, error) {
		return Response{OK: true, MacvtapLoaded: true, InstanceStatus: 8, Message: "ok"}, nil
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"macvtap", "load-macvtap", "restart-lightos"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status page missing %q", want)
		}
	}
}

func TestWebButtonForwardsToAgent(t *testing.T) {
	var gotAction string
	h := webHandler(func(req Request) (Response, error) {
		gotAction = req.Action
		return Response{OK: true, Message: "done"}, nil
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/restart-lightos", nil))
	if gotAction != "restart-lightos" {
		t.Fatalf("forwarded action=%q want restart-lightos", gotAction)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, code=%d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "msg=") {
		t.Fatalf("redirect Location=%q does not contain msg", loc)
	}
}
