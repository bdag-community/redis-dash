package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const peerListRefreshInterval = 10 * time.Minute

var peerListCache = struct {
	mu        sync.Mutex
	key       string
	view      PeerListView
	fetchedAt time.Time
	expires   time.Time
}{}

type PeerListView struct {
	Title        string              `json:"title"`
	State        string              `json:"state"`
	StateTone    string              `json:"state_tone"`
	UpdatedAt    string              `json:"updated_at"`
	RefreshEvery string              `json:"refresh_every"`
	Source       string              `json:"source"`
	Method       string              `json:"method"`
	PeerCount    int                 `json:"peer_count"`
	Columns      []string            `json:"columns"`
	Rows         []map[string]string `json:"rows"`
	APIError     string              `json:"api_error"`
}

func (a *App) peerList(ctx context.Context) PeerListView {
	if a.peerListProvider != nil {
		view, err := a.peerListProvider(ctx)
		if err == nil {
			return withPeerListDefaults(view)
		}
		return peerListErrorView(err)
	}

	url := first(
		env("BDAG_NODE_RPC_URL", ""),
		env("NODE_RPC_URL", ""),
		env("BDAG_RPC_URL", ""),
		defaultNativeRPCURL,
	)
	view, err := cachedNativePeerList(ctx, url)
	if err != nil {
		return peerListErrorView(err)
	}
	return view
}

func cachedNativePeerList(ctx context.Context, url string) (PeerListView, error) {
	key := strings.TrimSpace(url)
	now := time.Now()

	peerListCache.mu.Lock()
	if peerListCache.key == key && peerListCache.expires.After(now) {
		view := peerListCache.view
		peerListCache.mu.Unlock()
		return view, nil
	}
	peerListCache.mu.Unlock()

	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	result, method, err := nativePeerListRPC(requestCtx, key)
	if err != nil {
		return PeerListView{}, err
	}
	view := buildPeerListView(result, method, now)

	peerListCache.mu.Lock()
	peerListCache.key = key
	peerListCache.view = view
	peerListCache.fetchedAt = now
	peerListCache.expires = now.Add(peerListRefreshInterval)
	peerListCache.mu.Unlock()

	return view, nil
}

func nativePeerListRPC(ctx context.Context, url string) (any, string, error) {
	var result any
	if err := nativeRPC(ctx, url, "getPeerForks", []any{}, &result); err == nil {
		return result, "getPeerForks", nil
	} else if !nativeRPCMethodUnavailable(err) {
		return nil, "getPeerForks", err
	}
	if err := nativeRPC(ctx, url, "getPeerList", []any{}, &result); err == nil {
		return result, "getPeerList", nil
	} else if !nativeRPCMethodUnavailable(err) {
		return nil, "getPeerList", err
	}
	if err := nativeRPC(ctx, url, "getPeerInfo", []any{}, &result); err != nil {
		return nil, "getPeerInfo", err
	}
	return result, "getPeerInfo", nil
}

func nativeRPCMethodUnavailable(err error) bool {
	text := err.Error()
	return strings.Contains(text, "error -32601") || strings.Contains(text, "method") && strings.Contains(text, "does not exist")
}

func buildPeerListView(result any, method string, fetchedAt time.Time) PeerListView {
	peers := normalizePeerListRows(result)
	columns := peerListColumns(peers)
	rows := make([]map[string]string, 0, len(peers))
	for _, peer := range peers {
		row := make(map[string]string, len(peer))
		for _, column := range columns {
			row[column] = peerListCell(peer[column])
		}
		rows = append(rows, row)
	}
	return withPeerListDefaults(PeerListView{
		State:     fmt.Sprintf("%d peers", len(rows)),
		StateTone: "ok",
		UpdatedAt: fetchedAt.In(displayLocation).Format("2006-01-02 15:04:05 MST"),
		Source:    "native-rpc",
		Method:    first(method, "getPeerForks"),
		PeerCount: len(rows),
		Columns:   columns,
		Rows:      rows,
	})
}

func withPeerListDefaults(view PeerListView) PeerListView {
	if view.Title == "" {
		view.Title = "Peers"
	}
	if view.State == "" {
		view.State = fmt.Sprintf("%d peers", view.PeerCount)
	}
	if view.StateTone == "" {
		view.StateTone = "neutral"
	}
	if view.RefreshEvery == "" {
		view.RefreshEvery = "10m"
	}
	if view.Source == "" {
		view.Source = "native-rpc"
	}
	if view.Method == "" {
		view.Method = "getPeerForks"
	}
	if view.PeerCount == 0 && len(view.Rows) > 0 {
		view.PeerCount = len(view.Rows)
	}
	return view
}

func peerListErrorView(err error) PeerListView {
	return withPeerListDefaults(PeerListView{
		State:     "Unavailable",
		StateTone: "down",
		APIError:  err.Error(),
	})
}

func normalizePeerListRows(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		return normalizePeerListArray(typed)
	case []map[string]any:
		rows := make([]map[string]any, 0, len(typed))
		for _, row := range typed {
			rows = append(rows, row)
		}
		return rows
	case map[string]any:
		for _, key := range []string{"peers", "Peers", "peerList", "peerlist", "peer_list", "connected_peers", "data", "result"} {
			if nested, ok := typed[key]; ok {
				rows := normalizePeerListRows(nested)
				if len(rows) > 0 {
					return rows
				}
			}
		}
		if rows := mapValuesAsPeerRows(typed); len(rows) > 0 {
			return rows
		}
		return []map[string]any{typed}
	default:
		if value == nil {
			return nil
		}
		return []map[string]any{{"value": value}}
	}
}

func normalizePeerListArray(values []any) []map[string]any {
	rows := make([]map[string]any, 0, len(values))
	for idx, value := range values {
		if row, ok := value.(map[string]any); ok {
			rows = append(rows, row)
			continue
		}
		rows = append(rows, map[string]any{
			"index": idx,
			"value": value,
		})
	}
	return rows
}

func mapValuesAsPeerRows(values map[string]any) []map[string]any {
	rows := make([]map[string]any, 0, len(values))
	for key, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		copyRow := make(map[string]any, len(row)+1)
		copyRow["key"] = key
		for field, fieldValue := range row {
			copyRow[field] = fieldValue
		}
		rows = append(rows, copyRow)
	}
	sort.Slice(rows, func(i, j int) bool {
		return peerListCell(rows[i]["key"]) < peerListCell(rows[j]["key"])
	})
	return rows
}

func peerListColumns(rows []map[string]any) []string {
	seen := map[string]bool{}
	for _, row := range rows {
		for key := range row {
			seen[key] = true
		}
	}
	preferred := []string{
		"id", "key", "peer_id", "peerId", "node_id", "nodeID",
		"addr", "address", "host", "ip", "port", "network",
		"direction", "inbound", "outbound", "active", "state", "connected",
		"fork_status", "same_native_fork", "fork_confidence", "fork_reason", "commitment_status",
		"peer_main_order", "local_main_order", "peer_main_tip_hash", "local_main_tip_hash", "sync_point_hash", "divergence_order",
		"sampled_block_hash", "sampled_block_order", "sampled_state_root_got", "sampled_state_root_want",
		"evm_checkpoint_number", "evm_checkpoint_hash", "evm_checkpoint_root",
		"last_checked", "fork_last_checked", "last_error", "fork_last_error",
		"version", "subver", "protocol", "services",
		"lastsend", "lastrecv", "bytessent", "bytesrecv", "conntime",
		"pingtime", "pingwait", "lastpingtime", "startingheight", "banscore", "syncnode",
		"genesis", "graphstate",
	}
	columns := make([]string, 0, len(seen))
	for _, key := range preferred {
		if seen[key] {
			columns = append(columns, key)
			delete(seen, key)
		}
	}
	remaining := make([]string, 0, len(seen))
	for key := range seen {
		remaining = append(remaining, key)
	}
	sort.Strings(remaining)
	columns = append(columns, remaining...)
	return columns
}

func peerListCell(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return strconvFormatFloat(typed)
	case float32:
		return strconvFormatFloat(float64(typed))
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	case uint:
		return fmt.Sprintf("%d", typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	}
}

func strconvFormatFloat(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%g", value)
}

func resetPeerListCacheForTest() {
	peerListCache.mu.Lock()
	peerListCache.key = ""
	peerListCache.view = PeerListView{}
	peerListCache.fetchedAt = time.Time{}
	peerListCache.expires = time.Time{}
	peerListCache.mu.Unlock()
}
