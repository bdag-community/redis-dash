package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOverviewUsesLiveStatusWithoutContainerLogs(t *testing.T) {
	calls := 0
	app := &App{templates: parseTemplates(), statusDataProvider: func(context.Context) (map[string]any, error) {
		calls++
		return map[string]any{
			"source": "redis-live", "mode": "mining", "overall": "ok", "can_accept_shares": true, "can_submit_blocks": true,
			"sync_progress": map[string]any{"status": "synced", "native_is_current": true, "eth_syncing": false, "chain_block_count": 123, "peer_count": 4},
			"miner_health":  map[string]any{"connected_count": 3},
		}, nil
	}}
	w := httptest.NewRecorder()
	app.handleTab(w, httptest.NewRequest("GET", "/tabs/overview", nil))
	if w.Code != 200 {
		t.Fatalf("overview status: %d", w.Code)
	}
	html := w.Body.String()
	for _, want := range []string{"Node Status", "Synced", "Pool Status", "ASIC Performance", "Chain Block Count", "123", `data-live-tab="status"`} {
		if !strings.Contains(html, want) {
			t.Errorf("overview missing %q", want)
		}
	}
	if strings.Contains(html, "waiting for node_status_log") || calls != 1 {
		t.Fatalf("overview did not use one live status sample; calls=%d", calls)
	}
	w = httptest.NewRecorder()
	app.handleLiveStatus(w, httptest.NewRequest("GET", "/api/live/status", nil))
	var view TabView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Node.State != "Synced" {
		t.Fatalf("live node status: %+v %v", view.Node, err)
	}
}

func TestOverviewShowsUnavailableOrUnsyncedNode(t *testing.T) {
	for _, tc := range []struct {
		data map[string]any
		want string
	}{
		{map[string]any{}, "Unknown"},
		{map[string]any{"collector_status_unavailable": true}, "Unavailable"},
		{map[string]any{"sync_progress": map[string]any{"status": "p2p_down"}}, "P2P Down"},
		{map[string]any{"sync_progress": map[string]any{"status": "synced"}}, "Unknown"},
		{map[string]any{"sync_progress": map[string]any{"status": "synced", "native_is_current": false}}, "Catching Up"},
		{map[string]any{"sync_progress": map[string]any{"status": "synced", "eth_syncing": true}}, "EVM Syncing"},
	} {
		if got := liveNodeStatus(tc.data).State; got != tc.want {
			t.Errorf("state=%q want %q", got, tc.want)
		}
	}
	app := &App{statusDataProvider: func(context.Context) (map[string]any, error) { return nil, errors.New("unavailable") }}
	if got := app.nodeStatusOverview(context.Background()).Node.State; got != "Unavailable" {
		t.Fatalf("API failure state=%q", got)
	}
}

func TestCommunityReleaseLabel(t *testing.T) {
	for _, tc := range []struct{ tag, want string }{
		{"stack-dev-v3.0.0-rc.1", "BDAG Community Release v3.0.0-rc.1"},
		{"stack-v3.0.0", "BDAG Community Release v3.0.0"},
		{"jeremy-community-rescue-rc.65.6", "BDAG Community Development (jeremy-community-rescue-rc.65.6)"},
		{"", ""},
	} {
		if got := communityReleaseLabel(tc.tag); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.tag, got, tc.want)
		}
	}
}

func TestBundledHTMXIsPinnedAndServedLocally(t *testing.T) {
	w := httptest.NewRecorder()
	serveHTMX(w, httptest.NewRequest("GET", "/static/htmx-2.0.4.min.js", nil))
	digest := sha256.Sum256(w.Body.Bytes())
	if w.Code != 200 || fmt.Sprintf("%x", digest) != "69caefd0da92269066e725d7fe175e26b9d50c962e3056459c0c477154cdb9d3" {
		t.Fatal("bundled HTMX does not match its verified upstream copy")
	}
	if !strings.Contains(indexTemplate, `src="/static/htmx-2.0.4.min.js"`) || strings.Contains(indexTemplate, "unpkg.com") {
		t.Fatal("dashboard must load its pinned HTMX asset locally")
	}
}
