package main

import (
	"encoding/json"
	"testing"
)

func TestGlobalProductionSnapshotProjectsProducers(t *testing.T) {
	payload := map[string]any{
		"updated_at":        "2026-06-26T12:00:00Z",
		"scan_window_hours": "1",
		"requested_blocks":  "600",
		"fetched_blocks":    "600",
		"status":            "ok",
		"chain_clusters": []any{
			map[string]any{
				"address":       "0xaaa0000000000000000000000000000000000001",
				"label":         "Pool A",
				"address_short": "0xaaa000",
				"blocks":        "12",
				"share_percent": "60",
				"last_seen_at":  "2026-06-26T12:00:00Z",
			},
			map[string]any{
				"address":       "0xbbb0000000000000000000000000000000000002",
				"label":         "Pool B",
				"address_short": "0xbbb000",
				"blocks":        "3",
				"share_percent": "15",
				"last_seen_at":  "2026-06-26T11:00:00Z",
				"local_pool":    true,
			},
		},
	}

	snapshot := newGlobalProductionSnapshot(payload)
	if !snapshot.HasProducerRows() {
		t.Fatal("snapshot has no producer rows")
	}

	rows := snapshot.PoolEarningRows(nil, nil)
	if len(rows) != 2 {
		t.Fatalf("pool earning rows = %d, want 2", len(rows))
	}
	if rows[0]["pool"] != "Pool A" || rows[0]["color"] == "" {
		t.Fatalf("first row = %#v, want Pool A with color", rows[0])
	}
	if rows[1]["local"] != "true" {
		t.Fatalf("second row local = %q, want true", rows[1]["local"])
	}

	raw, colors, local := snapshot.ChartJSON()
	if colors["0xaaa0000000000000000000000000000000000001"] == "" {
		t.Fatalf("chart colors missing Pool A: %#v", colors)
	}
	if !local["0xbbb0000000000000000000000000000000000002"] {
		t.Fatalf("chart local producers missing Pool B: %#v", local)
	}

	var series []struct {
		Label  string       `json:"label"`
		Local  bool         `json:"local"`
		Points []chartPoint `json:"points"`
	}
	if err := json.Unmarshal([]byte(raw), &series); err != nil {
		t.Fatalf("chart JSON decode failed: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("chart series = %d, want 2: %s", len(series), raw)
	}
}
