package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPeerListUsesGetPeerForksAndRendersAllTopLevelFields(t *testing.T) {
	resetPeerListCacheForTest()
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
			ID     any    `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		methods = append(methods, request.Method)
		if request.Method != "getPeerForks" {
			http.Error(w, "unexpected method", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "1.0",
			"id":      request.ID,
			"result": []map[string]any{
				{
					"id":          "peer-a",
					"addr":        "/ip4/127.0.0.1/tcp/8150",
					"active":      true,
					"version":     70001,
					"fork_status": "same_main_chain",
					"graphstate":  map[string]any{"mainorder": 123},
				},
			},
		})
	}))
	defer server.Close()
	t.Setenv("BDAG_NODE_RPC_URL", server.URL)
	t.Setenv("NODE_RPC_USER", "")
	t.Setenv("NODE_RPC_PASS", "")

	view := (&App{}).peerList(context.Background())

	if len(methods) != 1 || methods[0] != "getPeerForks" {
		t.Fatalf("methods = %#v, want only getPeerForks", methods)
	}
	for _, column := range []string{"id", "addr", "active", "fork_status", "version", "graphstate"} {
		if !containsString(view.Columns, column) {
			t.Fatalf("columns = %#v, missing %q", view.Columns, column)
		}
	}
	if view.PeerCount != 1 || len(view.Rows) != 1 {
		t.Fatalf("peer rows = %d/%d, want 1", view.PeerCount, len(view.Rows))
	}
	if got := view.Rows[0]["graphstate"]; got != `{"mainorder":123}` {
		t.Fatalf("graphstate cell = %q", got)
	}
}

func TestPeerListCachesGetPeerListForRefreshInterval(t *testing.T) {
	resetPeerListCacheForTest()
	var calls atomic.Int64
	server := nativeRPCTestServerFunc(t, func(method string) (any, int) {
		if method != "getPeerForks" {
			return "unexpected method", http.StatusNotFound
		}
		calls.Add(1)
		return []map[string]any{{"id": "peer-a"}}, http.StatusOK
	})
	defer server.Close()
	t.Setenv("BDAG_NODE_RPC_URL", server.URL)
	t.Setenv("NODE_RPC_USER", "")
	t.Setenv("NODE_RPC_PASS", "")

	app := &App{}
	_ = app.peerList(context.Background())
	_ = app.peerList(context.Background())

	if got := calls.Load(); got != 1 {
		t.Fatalf("getPeerForks calls = %d, want 1", got)
	}
}

func TestPeerListFallsBackToGetPeerInfoWhenGetPeerListIsUnavailable(t *testing.T) {
	resetPeerListCacheForTest()
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
			ID     any    `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		methods = append(methods, request.Method)
		if request.Method == "getPeerForks" || request.Method == "getPeerList" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"error": map[string]any{
					"code":    -32601,
					"message": "The method " + request.Method + " does not exist/is not available",
				},
			})
			return
		}
		if request.Method != "getPeerInfo" {
			http.Error(w, "unexpected method", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result": []map[string]any{
				{"id": "peer-from-info", "address": "/ip4/127.0.0.1/tcp/8150", "latency_ms": 12},
			},
		})
	}))
	defer server.Close()
	t.Setenv("BDAG_NODE_RPC_URL", server.URL)
	t.Setenv("NODE_RPC_USER", "")
	t.Setenv("NODE_RPC_PASS", "")

	view := (&App{}).peerList(context.Background())

	if strings.Join(methods, ",") != "getPeerForks,getPeerList,getPeerInfo" {
		t.Fatalf("methods = %#v, want getPeerForks then getPeerList then getPeerInfo", methods)
	}
	if view.Method != "getPeerInfo" {
		t.Fatalf("method = %q, want getPeerInfo fallback", view.Method)
	}
	if view.PeerCount != 1 || view.Rows[0]["id"] != "peer-from-info" {
		t.Fatalf("view = %#v, want peer row from getPeerInfo", view)
	}
}

func TestPeersTabAndLiveEndpointRenderPeerRows(t *testing.T) {
	view := PeerListView{
		State:        "1 peers",
		StateTone:    "ok",
		UpdatedAt:    "2026-07-07 07:00:00 UTC",
		RefreshEvery: "10m",
		Method:       "getPeerInfo",
		PeerCount:    1,
		Columns:      []string{"id", "addr", "fork_status", "tip_hash"},
		Rows:         []map[string]string{{"id": "peer-a", "addr": "/ip4/127.0.0.1/tcp/8150", "fork_status": "same_main_chain", "tip_hash": "0123456789abcdef0123456789abcdef"}},
	}
	app := &App{
		templates: parseTemplates(),
		peerListProvider: func(context.Context) (PeerListView, error) {
			return view, nil
		},
	}

	tabReq := httptest.NewRequest(http.MethodGet, "/tabs/peers", nil)
	tabRec := httptest.NewRecorder()
	app.handleTab(tabRec, tabReq)
	if tabRec.Code != http.StatusOK {
		t.Fatalf("peers tab status = %d, want 200", tabRec.Code)
	}
	tabHTML := tabRec.Body.String()
	for _, expected := range []string{`data-live-tab="peers"`, "getPeerInfo", "peer-a", "/ip4/127.0.0.1/tcp/8150", "fork_status", "has-tooltip", `data-tooltip="Native DAG Main-chain classification.`, "tip_hash", "hash-cell-text"} {
		if !strings.Contains(tabHTML, expected) {
			t.Fatalf("peers tab missing %q: %s", expected, tabHTML)
		}
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/live/peers", nil)
	apiRec := httptest.NewRecorder()
	app.handleLivePeers(apiRec, apiReq)
	if apiRec.Code != http.StatusOK {
		t.Fatalf("live peers status = %d, want 200", apiRec.Code)
	}
	if !strings.Contains(apiRec.Body.String(), `"peer_count":1`) {
		t.Fatalf("live peers body missing peer count: %s", apiRec.Body.String())
	}
}

func TestPeersTemplateUsesTenMinuteLiveRefreshWithoutHTMXPolling(t *testing.T) {
	templates := parseTemplates()
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "index", map[string]any{
		"Title": "Dashboard",
		"Tab":   TabView{},
	}); err != nil {
		t.Fatalf("render index: %v", err)
	}
	html := buf.String()
	for _, expected := range []string{
		`hx-get="/tabs/peers"`,
		`peers: 600000`,
		`/api/live/peers`,
		`const tooltipAttr = tooltip ? ' data-tooltip="'`,
		`max-width: 20ch`,
		`if (tab === "peers")`,
		`kind === "peers" && activeLiveTab() === "peers"`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("index missing peers refresh marker %q", expected)
		}
	}
	if strings.Contains(html, `hx-trigger="every 10m"`) {
		t.Fatalf("peers tab should use centralized live refresh, not htmx polling")
	}
}
