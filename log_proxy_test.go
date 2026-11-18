package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDockerLogProxyAllowsOnlyConfiguredContainerLogs(t *testing.T) {
	calls := 0
	handler := dockerLogProxyHandler(func(_ context.Context, name string, tail int) (string, error) {
		calls++
		if name != "pool" || tail != 160 {
			t.Fatalf("unexpected request: %s %d", name, tail)
		}
		return "pool log", nil
	}, map[string]bool{"pool": true})
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/containers/pool/logs?tail=160", http.StatusOK},
		{"POST", "/containers/pool/logs?tail=160", http.StatusNotFound},
		{"GET", "/containers/other/logs?tail=160", http.StatusNotFound},
		{"GET", "/containers/json?tail=160", http.StatusNotFound},
		{"GET", "/containers/pool/logs?tail=5001", http.StatusBadRequest},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Errorf("%s %s returned %d, want %d", tc.method, tc.path, w.Code, tc.status)
		}
	}
	if calls != 1 {
		t.Fatalf("Docker called %d times, want 1", calls)
	}
}

func TestDockerLogProxyReadsLogsThroughUnixSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "lp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "d.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/containers/pool/logs" || r.URL.Query().Get("tail") != "160" {
			t.Errorf("unexpected Docker request: %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte("pool log\n"))
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	handler := dockerLogProxyHandler(func(ctx context.Context, name string, tail int) (string, error) {
		return dockerContainerLogs(ctx, socket, name, tail)
	}, map[string]bool{"pool": true})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/containers/pool/logs?tail=160", nil))
	if w.Code != http.StatusOK || w.Body.String() != "pool log\n" {
		t.Fatalf("proxy response = %d %q", w.Code, w.Body.String())
	}
}
