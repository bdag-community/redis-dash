package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGlobalProducerColorsStayStableWhenRankChanges(t *testing.T) {
	first := globalPayload(
		globalProducer("0xaaa0000000000000000000000000000000000001", "Pool A", "12"),
		globalProducer("0xbbb0000000000000000000000000000000000002", "Pool B", "4"),
	)
	second := globalPayload(
		globalProducer("0xaaa0000000000000000000000000000000000001", "Pool A", "3"),
		globalProducer("0xbbb0000000000000000000000000000000000002", "Pool B", "14"),
	)

	_, firstColors, _ := globalChartJSON(first)
	_, secondColors, _ := globalChartJSON(second)

	for _, key := range []string{
		"0xaaa0000000000000000000000000000000000001",
		"0xbbb0000000000000000000000000000000000002",
	} {
		if firstColors[key] == "" {
			t.Fatalf("first refresh has no color for %s", key)
		}
		if secondColors[key] == "" {
			t.Fatalf("second refresh has no color for %s", key)
		}
		if firstColors[key] != secondColors[key] {
			t.Fatalf("color for %s changed from %s to %s after rank changed", key, firstColors[key], secondColors[key])
		}
	}
}

func TestNativeDAGGlobalPayloadProvidesBestEffortProducerRows(t *testing.T) {
	now := time.Now().UTC()
	localProducer := "0xd5f8df0a60bc1ff4636250b2a2dff319bf79b1f5"
	t.Setenv("MINING_POOL_ADDRESS", localProducer)
	payload := nativeDAGGlobalPayload(map[string]any{
		"updated_at":       now.Format(time.RFC3339),
		"updated_at_epoch": float64(now.Unix()),
		"age_seconds":      float64(0),
		"window": map[string]any{
			"requested": float64(120),
		},
		"nodes": []any{
			map[string]any{"producer": localProducer, "timestamp": now.Add(-20 * time.Second).Format(time.RFC3339), "order": float64(100)},
			map[string]any{"producer": localProducer, "timestamp": now.Add(-10 * time.Second).Format(time.RFC3339), "order": float64(101)},
			map[string]any{"producer": "0x1719e0ee598c15957448d5e568948101df78e7a0", "timestamp": now.Format(time.RFC3339), "order": float64(102)},
			map[string]any{"producer": "", "producer_pending": true, "timestamp": now.Format(time.RFC3339), "order": float64(103)},
		},
	}, nil)
	if payload == nil {
		t.Fatal("native DAG global payload is nil")
	}
	if stringValue(payload, "source") != "chain-dag-live" || !globalBestEffortDisplayable(payload) {
		t.Fatalf("payload is not marked displayable best effort: %#v", payload)
	}
	if got := stringValue(payload, "fetched_blocks"); got != "4" {
		t.Fatalf("fetched_blocks = %q, want 4 retained blocks with timestamps", got)
	}
	if got := stringValue(payload, "attributed_blocks"); got != "3" {
		t.Fatalf("attributed_blocks = %q, want 3 producer-attributed blocks", got)
	}
	if got := poolEarningsTitle(payload, false); !strings.Contains(got, "best-effort live 3/600 blocks") {
		t.Fatalf("title = %q", got)
	}
	rows := poolEarningRows(payload, nil, nil)
	if len(rows) != 2 || rows[0]["blocks"] != "2" || rows[0]["local"] != "true" {
		t.Fatalf("best-effort rows = %#v", rows)
	}
}

func TestNativeDAGGlobalSourceUsableAcceptsDegradedWindowsWithRows(t *testing.T) {
	now := time.Now().UTC()
	base := map[string]any{
		"updated_at":       now.Format(time.RFC3339),
		"updated_at_epoch": float64(now.Unix()),
		"age_seconds":      float64(0),
		"status":           "ok",
		"nodes": []any{
			map[string]any{"producer": "0xaaa0000000000000000000000000000000000001", "timestamp": now.Format(time.RFC3339), "order": float64(1)},
		},
	}
	if !nativeDAGGlobalSourceUsable(base) {
		t.Fatalf("fresh native DAG payload should be usable: %#v", base)
	}
	degraded := cloneMap(base)
	degraded["status"] = "degraded"
	degraded["coverage"] = map[string]any{"requested": float64(600), "failures": float64(1)}
	if !nativeDAGGlobalSourceUsable(degraded) {
		t.Fatalf("degraded native DAG payload with usable rows should remain displayable: %#v", degraded)
	}
	stale := cloneMap(base)
	stale["status"] = "stale"
	if nativeDAGGlobalSourceUsable(stale) {
		t.Fatalf("stale native DAG payload should not be a Global source: %#v", stale)
	}
	sourceError := cloneMap(base)
	sourceError["source_error"] = "native RPC failed"
	if nativeDAGGlobalSourceUsable(sourceError) {
		t.Fatalf("source-error native DAG payload should not be a Global source: %#v", sourceError)
	}
	fullFailure := cloneMap(base)
	fullFailure["coverage"] = map[string]any{"requested": float64(600), "failures": float64(600)}
	if nativeDAGGlobalSourceUsable(fullFailure) {
		t.Fatalf("full-failure native DAG payload should not be a Global source: %#v", fullFailure)
	}
}

func TestGlobalWindowProgressNoteForNativeDAGBestEffortRows(t *testing.T) {
	now := time.Now().UTC()
	payload := nativeDAGGlobalPayload(map[string]any{
		"updated_at":       now.Format(time.RFC3339),
		"updated_at_epoch": float64(now.Unix()),
		"age_seconds":      float64(0),
		"window": map[string]any{
			"requested": float64(600),
		},
		"nodes": []any{
			map[string]any{"producer": "0xaaa0000000000000000000000000000000000001", "timestamp": now.Format(time.RFC3339), "order": float64(10)},
		},
	}, nil)

	note := globalWindowProgressNote(payload, false)

	if !strings.Contains(note, "Showing best-effort producer visibility") {
		t.Fatalf("note = %q, want visible best-effort wording", note)
	}
	if strings.Contains(note, "not shown") {
		t.Fatalf("note = %q, should not claim displayed rows are hidden", note)
	}
	if !strings.Contains(note, "complete 600-block earnings truth") {
		t.Fatalf("note = %q, want canonical truth caveat", note)
	}
}

func TestChainDAGGraphJSONUsesStableProducerColorAndLocalFlag(t *testing.T) {
	producer := "0xD5F8DF0A60bC1Ff4636250b2a2dff319Bf79B1f5"
	t.Setenv("MINING_POOL_ADDRESS", strings.ToLower(producer))
	raw := chainDAGGraphJSON(map[string]any{
		"mode":   "native-dag-linear-spine",
		"source": "native-rpc",
		"nodes": []any{map[string]any{
			"hash":     "0xabc",
			"producer": producer,
			"tip":      true,
			"dag_set":  "tip-valid",
		}},
		"edges": []any{},
	})
	var graph struct {
		Nodes []map[string]any `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(raw), &graph); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(graph.Nodes))
	}
	node := graph.Nodes[0]
	if got := mapString(node, "producer_color"); got != stableProducerColor(strings.ToLower(producer)) {
		t.Fatalf("producer_color = %q, want stable global color", got)
	}
	if node["local_pool"] != true {
		t.Fatalf("local_pool = %v, want true", node["local_pool"])
	}
	if node["tip"] != true || mapString(node, "dag_set") != "tip-valid" {
		t.Fatalf("tip metadata not preserved: %#v", node)
	}
}

func TestChainDAGRejectsBlankNativeTopologyWithoutLinearGlobalFallback(t *testing.T) {
	native := map[string]any{
		"status": "stale",
		"mode":   "native-dag-live",
		"source": "native-rpc-live",
		"nodes":  []any{},
		"edges":  []any{},
		"warnings": []any{
			"Native DAG live source is stale: rebuild fetched no usable blocks for the current topology window.",
		},
	}

	if chainDAGPayloadHasNodes(native) {
		t.Fatal("blank native DAG topology must not be treated as populated")
	}
	if chainDAGNativePayloadSafeForDisplay(native) {
		t.Fatal("blank stale native DAG topology must not be display-safe")
	}
	warnings := strings.Join(stringSlice(native["warnings"]), "\n")
	if strings.Contains(warnings, "showing Redis EVM-linear fallback instead of a blank topology window") {
		t.Fatalf("warnings = %#v, must not contain removed linear fallback wording", native["warnings"])
	}
}

func TestChainDAGRejectsUnsafeNativeRowsWithoutLinearGlobalFallback(t *testing.T) {
	native := map[string]any{
		"status":               "stale",
		"mode":                 "native-dag-linear-spine",
		"source":               "native-rpc-live",
		"live_pipeline_status": "source-degraded",
		"nodes": []any{
			map[string]any{"hash": "0xstale", "height": 100},
		},
		"edges": []any{},
		"coverage": map[string]any{
			"requested": 600,
			"failures":  600,
		},
		"warnings": []any{
			"Skipped 600 native DAG block RPC results while building the topology window.",
			"Native DAG tips are unavailable: node RPC backoff active: EVM RPC eth_getBlockByNumber returned 429 Too Many Requests: global rate limit exceed",
		},
	}

	if chainDAGNativePayloadSafeForDisplay(native) {
		t.Fatal("stale native DAG rows with full RPC failures must not be treated as display-safe")
	}
	warnings := strings.Join(stringSlice(native["warnings"]), "\n")
	if !strings.Contains(warnings, "Skipped 600 native DAG block RPC results") {
		t.Fatalf("warnings = %#v, want retained native RPC diagnostic", native["warnings"])
	}
	if strings.Contains(warnings, "until native topology is safe again") {
		t.Fatalf("warnings = %#v, must not contain removed linear fallback wording", native["warnings"])
	}
}

func TestGlobalPoolEarningFallbackColorsStayStableWhenRankChanges(t *testing.T) {
	first := poolEarningRows(globalPayload(
		globalProducer("0xaaa0000000000000000000000000000000000001", "Pool A", "12"),
		globalProducer("0xbbb0000000000000000000000000000000000002", "Pool B", "4"),
	), nil, nil)
	second := poolEarningRows(globalPayload(
		globalProducer("0xaaa0000000000000000000000000000000000001", "Pool A", "3"),
		globalProducer("0xbbb0000000000000000000000000000000000002", "Pool B", "14"),
	), nil, nil)

	firstColors := map[string]string{}
	for _, row := range first {
		firstColors[row["key"]] = row["color"]
	}
	for _, row := range second {
		key := row["key"]
		if firstColors[key] == "" {
			t.Fatalf("first refresh has no fallback color for %s", key)
		}
		if row["color"] != firstColors[key] {
			t.Fatalf("fallback color for %s changed from %s to %s after rank changed", key, firstColors[key], row["color"])
		}
	}
}

func TestNativeDAGGlobalPayloadReportsPartialAttributionAsBestEffort(t *testing.T) {
	now := time.Now().UTC()
	payload := nativeDAGGlobalPayload(map[string]any{
		"updated_at":       now.Format(time.RFC3339),
		"updated_at_epoch": float64(now.Unix()),
		"age_seconds":      float64(0),
		"window":           map[string]any{"requested": float64(4)},
		"nodes": []any{
			map[string]any{"producer": "0xaaa0000000000000000000000000000000000001", "timestamp": now.Add(-3 * time.Second).Format(time.RFC3339), "order": float64(1)},
			map[string]any{"producer": "0xaaa0000000000000000000000000000000000001", "timestamp": now.Add(-2 * time.Second).Format(time.RFC3339), "order": float64(2)},
			map[string]any{"producer": "", "producer_pending": true, "timestamp": now.Add(-1 * time.Second).Format(time.RFC3339), "order": float64(3)},
			map[string]any{"producer": "", "producer_pending": true, "timestamp": now.Format(time.RFC3339), "order": float64(4)},
		},
	}, nil)
	if payload == nil {
		t.Fatal("native DAG global payload is nil")
	}
	if got := stringValue(payload, "fetched_blocks"); got != "4" {
		t.Fatalf("fetched_blocks = %q, want 4 scanned blocks", got)
	}
	if got := stringValue(payload, "attributed_blocks"); got != "2" {
		t.Fatalf("attributed_blocks = %q, want 2 attributed blocks", got)
	}
	if snapshotFlag(payload, "window_complete") {
		t.Fatalf("window_complete = true, want false until all retained blocks are attributed: %#v", payload)
	}
	if !globalBestEffortDisplayable(payload) {
		t.Fatalf("partial native DAG attribution should remain displayable: %#v", payload)
	}
	notes := strings.Join(stringSlice(payload["warnings"]), "\n")
	if !strings.Contains(notes, "producer attribution for 2/4 retained blocks") {
		t.Fatalf("warnings = %#v, want partial attribution warning", payload["warnings"])
	}
}

func TestPoolEarningRowsAcceptsChainClustersPayload(t *testing.T) {
	payload := map[string]any{
		"chain_clusters": []any{
			globalProducer("0xaaa0000000000000000000000000000000000001", "Pool A", "12"),
		},
	}

	rows := poolEarningRows(payload, nil, nil)

	if len(rows) != 1 {
		t.Fatalf("pool earning rows = %d, want 1 from chain_clusters", len(rows))
	}
	if got := rows[0]["pool"]; got != "Pool A" {
		t.Fatalf("pool = %q, want Pool A", got)
	}
	if got := rows[0]["color"]; got == "" {
		t.Fatal("row has no stable producer color")
	}
}

func globalPayload(producers ...map[string]any) map[string]any {
	clusters := make([]any, 0, len(producers))
	for _, producer := range producers {
		clusters = append(clusters, producer)
	}
	return map[string]any{
		"generated_at":       "2026-06-18T12:00:00Z",
		"scan_window_hours":  "1",
		"requested_blocks":   "600",
		"fetched_blocks":     "600",
		"status":             "ok",
		"clusters":           clusters,
		"avg_blocks_per_sec": "1",
	}
}

func globalProducer(address, label, blocks string) map[string]any {
	return map[string]any{
		"address":       address,
		"label":         label,
		"pool_label":    label,
		"address_short": address[:8],
		"blocks":        blocks,
		"share_percent": "10",
		"last_seen_at":  "2026-06-18T12:00:00Z",
	}
}
