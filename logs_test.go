package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStripDockerLogFrames(t *testing.T) {
	raw := []byte{
		1, 0, 0, 0, 0, 0, 0, 6,
		'h', 'e', 'l', 'l', 'o', '\n',
		2, 0, 0, 0, 0, 0, 0, 6,
		'w', 'a', 'r', 'n', '!', '\n',
	}
	if got := stripDockerLogFrames(raw); got != "hello\nwarn!\n" {
		t.Fatalf("stripDockerLogFrames = %q", got)
	}
	if got := stripDockerLogFrames([]byte("plain\n")); got != "plain\n" {
		t.Fatalf("plain logs changed to %q", got)
	}
}

func TestLogsViewDefersPoolLogFetch(t *testing.T) {
	nodeFetches := 0
	poolFetches := 0
	dashboardFetches := 0
	app := &App{
		nodeLogProvider: func(context.Context, int) string {
			nodeFetches++
			return "node line"
		},
		poolLogProvider: func(context.Context, int) string {
			poolFetches++
			return "pool line"
		},
		dashboardLogProvider: func(context.Context, int) string {
			dashboardFetches++
			return "dashboard line"
		},
	}

	view := app.logsView(context.Background())

	if nodeFetches != 1 {
		t.Fatalf("node fetches = %d, want 1", nodeFetches)
	}
	if poolFetches != 0 {
		t.Fatalf("pool fetches = %d, want deferred pool fetch", poolFetches)
	}
	if dashboardFetches != 0 {
		t.Fatalf("dashboard fetches = %d, want deferred dashboard fetch", dashboardFetches)
	}
	if view.Node.Content != "node line" {
		t.Fatalf("node content = %q, want node line", view.Node.Content)
	}
	if view.Pool.Content != "" || view.Pool.ContentEndpoint != "/logs/pool/content" {
		t.Fatalf("pool metadata = %#v, want endpoint without content", view.Pool)
	}
	if view.Dashboard.Content != "" || view.Dashboard.ContentEndpoint != "/logs/dashboard/content" {
		t.Fatalf("dashboard metadata = %#v, want endpoint without content", view.Dashboard)
	}
}

func TestLogContentEndpointReturnsPlainText(t *testing.T) {
	app := &App{
		nodeLogProvider: func(context.Context, int) string {
			return "<node>\n"
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/logs/node/content", nil)
	rec := httptest.NewRecorder()

	app.handleLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("content type = %q, want text/plain", contentType)
	}
	if got := rec.Body.String(); got != "<node>\n" {
		t.Fatalf("body = %q, want raw log text", got)
	}
}

func TestDashboardLogContentEndpointReturnsPlainText(t *testing.T) {
	app := &App{
		dashboardLogProvider: func(context.Context, int) string {
			return "dashboard line\n"
		},
	}

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/logs/dashboard/content", "dashboard line\n"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()

		app.handleLogs(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.path, rec.Code)
		}
		if got := rec.Body.String(); got != tc.want {
			t.Fatalf("%s body = %q, want %q", tc.path, got, tc.want)
		}
	}
}
