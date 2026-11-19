package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeSyncProgressReportsSyncingWhenPeersAreAhead(t *testing.T) {
	server := nativeRPCTestServer(t, map[string]any{
		"getBlockCount": float64(100),
		"isCurrent":     false,
		"getPeerInfo": []map[string]any{
			{
				"active": true,
				"state":  true,
				"graphstate": map[string]any{
					"mainorder": float64(150),
				},
			},
		},
	})
	defer server.Close()
	t.Setenv("BDAG_NODE_RPC_URL", server.URL)
	t.Setenv("BDAG_EVM_HTTP_URL", server.URL)
	t.Setenv("NODE_RPC_USER", "")
	t.Setenv("NODE_RPC_PASS", "")

	progress := nativeSyncProgress(context.Background())
	if got := progress["status"]; got != "syncing" {
		t.Fatalf("status = %v, want syncing", got)
	}
	if got := progress["chain_block_count"]; got != uint64(100) {
		t.Fatalf("chain_block_count = %v, want 100", got)
	}
	if got := progress["p2p_network_height"]; got != int64(150) {
		t.Fatalf("p2p_network_height = %v, want 150", got)
	}
	if got := progress["remaining_blocks"]; got != int64(50) {
		t.Fatalf("remaining_blocks = %v, want 50", got)
	}
}

func TestNativeSyncProgressReportsSyncedOnlyWhenCurrent(t *testing.T) {
	server := nativeRPCTestServer(t, map[string]any{
		"getBlockCount": float64(150),
		"isCurrent":     true,
		"getPeerInfo": []map[string]any{
			{
				"active": true,
				"state":  true,
				"graphstate": map[string]any{
					"mainorder": float64(150),
				},
			},
		},
	})
	defer server.Close()
	t.Setenv("BDAG_NODE_RPC_URL", server.URL)
	t.Setenv("BDAG_EVM_HTTP_URL", server.URL)
	t.Setenv("NODE_RPC_USER", "")
	t.Setenv("NODE_RPC_PASS", "")

	progress := nativeSyncProgress(context.Background())
	if got := progress["status"]; got != "synced" {
		t.Fatalf("status = %v, want synced", got)
	}
	if got := progress["remaining_blocks"]; got != int64(0) {
		t.Fatalf("remaining_blocks = %v, want 0", got)
	}
}

func TestNativeSyncProgressRequiresActivePeers(t *testing.T) {
	server := nativeRPCTestServer(t, map[string]any{
		"getBlockCount": float64(150),
		"isCurrent":     true,
		"getPeerInfo":   []map[string]any{},
	})
	defer server.Close()
	t.Setenv("BDAG_NODE_RPC_URL", server.URL)
	t.Setenv("BDAG_EVM_HTTP_URL", server.URL)
	t.Setenv("NODE_RPC_USER", "")
	t.Setenv("NODE_RPC_PASS", "")

	progress := nativeSyncProgress(context.Background())
	if got := progress["status"]; got != "p2p_down" {
		t.Fatalf("status = %v, want p2p_down", got)
	}
}

func TestNativeSyncProgressReportsEthSyncingAsAdvisoryWhenNativeIsCurrent(t *testing.T) {
	server := nativeRPCTestServer(t, map[string]any{
		"getBlockCount": float64(12000),
		"isCurrent":     true,
		"getPeerInfo": []map[string]any{
			{
				"active": true,
				"state":  true,
				"graphstate": map[string]any{
					"mainorder": float64(12000),
				},
			},
		},
		"eth_syncing": map[string]any{
			"currentBlock":  "0x1f40",
			"highestBlock":  "0x2710",
			"startingBlock": "0x0",
		},
	})
	defer server.Close()
	t.Setenv("BDAG_NODE_RPC_URL", server.URL)
	t.Setenv("BDAG_EVM_HTTP_URL", server.URL)
	t.Setenv("NODE_RPC_USER", "")
	t.Setenv("NODE_RPC_PASS", "")

	progress := nativeSyncProgress(context.Background())
	if got := progress["status"]; got != "synced" {
		t.Fatalf("status = %v, want synced", got)
	}
	if got := number(mapString(progress, "current_block")); got != 12000 {
		t.Fatalf("current_block = %v, want native block count 12000", got)
	}
	if got := number(mapString(progress, "highest_block")); got != 12000 {
		t.Fatalf("highest_block = %v, want native block count 12000", got)
	}
	if got := number(mapString(progress, "remaining_blocks")); got != 0 {
		t.Fatalf("remaining_blocks = %v, want 0", got)
	}
	if got := number(mapString(progress, "evm_block_count")); got != 8000 {
		t.Fatalf("evm_block_count = %v, want advisory EVM height 8000", got)
	}
	if got := progress["chain_syncing"]; got != false {
		t.Fatalf("chain_syncing = %v, want false for native-safe advisory EVM sync", got)
	}
	if got := progress["evm_chain_syncing"]; got != true {
		t.Fatalf("evm_chain_syncing = %v, want true", got)
	}
	if got := progress["mining_advisory_sync"]; got != true {
		t.Fatalf("mining_advisory_sync = %v, want true", got)
	}
	if got := progress["error"]; got != nil {
		t.Fatalf("error = %v, want no blocking sync error", got)
	}
}

func TestNativeSyncProgressCachedReusesStatusRPCResult(t *testing.T) {
	resetNativeSyncProgressCacheForTest()
	defer resetNativeSyncProgressCacheForTest()

	var calls int32
	server := nativeRPCTestServerFunc(t, func(method string) (any, int) {
		atomic.AddInt32(&calls, 1)
		switch method {
		case "getBlockCount":
			return float64(150), http.StatusOK
		case "isCurrent":
			return true, http.StatusOK
		case "getPeerInfo":
			return []map[string]any{{
				"active": true,
				"state":  true,
				"graphstate": map[string]any{
					"mainorder": float64(150),
				},
			}}, http.StatusOK
		case "eth_syncing":
			return false, http.StatusOK
		default:
			t.Fatalf("unexpected rpc method %s", method)
			return nil, http.StatusNotFound
		}
	})
	defer server.Close()
	t.Setenv("BDAG_NODE_RPC_URL", server.URL)
	t.Setenv("BDAG_EVM_HTTP_URL", server.URL)
	t.Setenv("NODE_RPC_USER", "")
	t.Setenv("NODE_RPC_PASS", "")

	first := nativeSyncProgressCached(context.Background())
	second := nativeSyncProgressCached(context.Background())

	if first["cache"] != "miss" {
		t.Fatalf("first cache marker = %v, want miss", first["cache"])
	}
	if second["cache"] != "hit" {
		t.Fatalf("second cache marker = %v, want hit", second["cache"])
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("RPC calls = %d, want one native sync sequence of 4 calls", got)
	}
}

func TestNodeRPCBudgetClassifiesBusyAndThrottlesLowPriority(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "syncing but no active peers", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	budget := newNodeRPCBudget(1, time.Minute)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := budget.do(context.Background(), server.Client(), req)
	if err != nil {
		t.Fatalf("normal priority request failed before response classification: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if !budget.backoffActive() {
		t.Fatal("503 response did not activate node RPC backoff")
	}

	lowCtx := withLowPriorityNodeRPC(context.Background())
	lowReq, err := http.NewRequestWithContext(lowCtx, http.MethodPost, server.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = budget.do(lowCtx, server.Client(), lowReq)
	if !isNodeRPCBusyError(err) {
		t.Fatalf("low-priority request error = %v, want node busy/backoff classification", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("HTTP calls = %d, want low-priority call throttled before network", got)
	}
}

func TestSyncProgressBlocksMiningUnlessSynced(t *testing.T) {
	cases := []struct {
		name     string
		progress map[string]any
		want     bool
	}{
		{"synced", map[string]any{"status": "synced"}, false},
		{"syncing", map[string]any{"status": "syncing"}, true},
		{"unknown", map[string]any{"status": "unknown"}, true},
		{"chain syncing", map[string]any{"status": "synced", "chain_syncing": true}, true},
		{
			"native current while EVM syncing",
			map[string]any{
				"status":               "synced",
				"chain_syncing":        false,
				"evm_chain_syncing":    true,
				"native_is_current":    true,
				"mining_advisory_sync": true,
				"p2p_network_gap":      0,
				"peer_mainorder_gap":   0,
			},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := syncProgressBlocksMining(tc.progress); got != tc.want {
				t.Fatalf("syncProgressBlocksMining() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPoolBackendMiningSafeRequiresNativeBackendProof(t *testing.T) {
	base := map[string]any{
		"active_connections":              "4",
		"backend_mineable":                "1",
		"backend_submit_ready":            "1",
		"backend_p2p_mining_fresh":        "1",
		"backend_peer_lead_blocks":        "0",
		"backend_template_coinbase_valid": "1",
	}
	if !poolBackendMiningSafe(base, nil) {
		t.Fatalf("poolBackendMiningSafe() = false, want true for native-safe backend")
	}
	for name, value := range map[string]string{
		"backend_mineable":                "0",
		"backend_submit_ready":            "0",
		"backend_p2p_mining_fresh":        "0",
		"backend_peer_lead_blocks":        "11",
		"backend_template_coinbase_valid": "0",
	} {
		metrics := map[string]any{}
		for key, baseValue := range base {
			metrics[key] = baseValue
		}
		metrics[name] = value
		if poolBackendMiningSafe(metrics, nil) {
			t.Fatalf("poolBackendMiningSafe(%s=%s) = true, want false", name, value)
		}
	}
	transientTemplateChurn := map[string]any{
		"active_connections":                         "4",
		"ready_miners":                               "4",
		"backend_mineable":                           "0",
		"backend_submit_ready":                       "0",
		"backend_p2p_mining_fresh":                   "1",
		"backend_peer_lead_blocks":                   "0",
		"template_age_seconds":                       "2",
		"accepted_block_submissions_delta":           "3",
		"accepted_block_submissions_rate_per_second": "0.6",
	}
	if !poolBackendMiningSafe(transientTemplateChurn, nil) {
		t.Fatal("poolBackendMiningSafe() = false during fresh accepted-block delta and P2P-safe template churn")
	}
	transientTemplateChurn["accepted_block_submissions_delta"] = "0"
	transientTemplateChurn["accepted_block_recent"] = true
	if !poolBackendMiningSafe(transientTemplateChurn, nil) {
		t.Fatal("poolBackendMiningSafe() = false during recent accepted block and P2P-safe template churn")
	}
	transientTemplateChurn["ready_miners"] = "0"
	if !poolBackendMiningSafe(transientTemplateChurn, nil) {
		t.Fatal("poolBackendMiningSafe() = false during recent accepted block and P2P-safe zero-ready flicker")
	}
	transientTemplateChurn["accepted_block_recent"] = false
	if poolBackendMiningSafe(transientTemplateChurn, nil) {
		t.Fatal("poolBackendMiningSafe() = true without a fresh or recent accepted block")
	}
	transientTemplateChurn["accepted_block_recent"] = true
	transientTemplateChurn["active_connections"] = "0"
	if poolBackendMiningSafe(transientTemplateChurn, nil) {
		t.Fatal("poolBackendMiningSafe() = true without active miners")
	}
	nativeProgressFallback := map[string]any{
		"active_connections":       "4",
		"backend_mineable":         "",
		"backend_submit_ready":     "",
		"backend_p2p_mining_fresh": "",
		"accepted_block_recent":    true,
	}
	nativeProgress := map[string]any{
		"status":               "synced",
		"native_is_current":    true,
		"chain_syncing":        false,
		"evm_chain_syncing":    true,
		"mining_advisory_sync": true,
		"remaining_blocks":     0,
		"p2p_network_gap":      0,
		"peer_mainorder_gap":   0,
	}
	if !poolBackendMiningSafe(nativeProgressFallback, nativeProgress) {
		t.Fatal("poolBackendMiningSafe() = false with native-current progress and recent accepted block")
	}
	nativeProgressFallback["backend_p2p_mining_fresh"] = "0"
	if poolBackendMiningSafe(nativeProgressFallback, nativeProgress) {
		t.Fatal("poolBackendMiningSafe() = true despite explicit stale backend P2P")
	}
}

func TestPoolMetricsDataIncludesTemplateSafetyInputs(t *testing.T) {
	metrics := parsePrometheusMetrics(strings.NewReader(`
pool_active_connections{pool_id="0"} 4
pool_job_health_authorized_miners{pool_id="0"} 4
pool_job_health_ready_miners{pool_id="0"} 4
pool_rpc_backend_node_health_mineable{node="node",pool_id="0"} 1
pool_rpc_backend_node_health_submit_ready{node="node",pool_id="0"} 1
pool_rpc_backend_node_health_p2p_mining_fresh{node="node",pool_id="0"} 1
pool_rpc_backend_node_health_p2p_best_peer_lead_blocks{node="node",pool_id="0"} 0
pool_rpc_backend_node_health_template_coinbase_valid{node="node",pool_id="0"} 1
pool_template_coinbase_rejects_total{pool_id="0"} 0
pool_template_conversion_stall_window_candidates{kind="accepted",pool_id="0"} 27
pool_template_conversion_stall_window_candidates{kind="failed",pool_id="0"} 3
pool_template_conversion_stall_window_candidates{kind="stale",pool_id="0"} 2
pool_template_conversion_stall_window_candidates{kind="total",pool_id="0"} 30
`))

	data := poolMetricsDataFromSamples(metrics)

	if got := mapString(data, "backend_template_coinbase_valid"); got != "1" {
		t.Fatalf("backend_template_coinbase_valid = %q, want 1", got)
	}
	if got := mapString(data, "template_coinbase_reject_total"); got != "0" {
		t.Fatalf("template_coinbase_reject_total = %q, want 0", got)
	}
	if got := number(mapString(data, "template_local_stale_ratio")); got < 0.066 || got > 0.067 {
		t.Fatalf("template_local_stale_ratio = %v, want 2/30", got)
	}
}

func TestTemplateHealthAndSafetyDerivedFromPoolMetrics(t *testing.T) {
	metrics := map[string]any{
		"backend_mineable":                "1",
		"backend_submit_ready":            "1",
		"backend_p2p_mining_fresh":        "1",
		"backend_template_coinbase_valid": "1",
		"template_age_seconds":            "0.25",
		"template_coinbase_reject_total":  "0",
		"template_local_stale_ratio":      "0.066667",
		"ready_miners":                    "4",
	}

	health := poolTemplateHealthFromMetrics(metrics)
	safety := poolTemplateSafetyFromMetrics(metrics)

	if got := health["reason_code"]; got != "ok" {
		t.Fatalf("reason_code = %v, want ok", got)
	}
	if got := health["template_coinbase_valid"]; got != true {
		t.Fatalf("template health coinbase = %v, want true", got)
	}
	if got := safety["template_coinbase_valid"]; got != true {
		t.Fatalf("template safety coinbase = %v, want true", got)
	}
	if got := safety["ready_miner_job_count"]; got != "4" {
		t.Fatalf("ready_miner_job_count = %v, want 4", got)
	}
	if got := safety["local_stale_ratio"]; got != "0.066667" {
		t.Fatalf("local_stale_ratio = %v, want 0.066667", got)
	}

	metrics["backend_template_coinbase_valid"] = "0"
	health = poolTemplateHealthFromMetrics(metrics)
	if got := health["reason_code"]; got != "zero-template-coinbase" {
		t.Fatalf("zero coinbase reason_code = %v, want zero-template-coinbase", got)
	}
}

func TestLiveStatusOverallRequiresSubmitReady(t *testing.T) {
	mode, overall := liveStatusModeAndOverall(
		false,
		map[string]any{
			"status":                "synced",
			"chain_syncing":         false,
			"evm_chain_syncing":     true,
			"native_is_current":     true,
			"mining_advisory_sync":  true,
			"backend_p2p_mining_ok": true,
		},
		true,
		true,
	)
	if mode != "mining" || overall != "ok" {
		t.Fatalf("native-safe advisory mode/overall = %s/%s, want mining/ok", mode, overall)
	}

	mode, overall = liveStatusModeAndOverall(
		false,
		map[string]any{"status": "synced", "native_is_current": true},
		true,
		false,
	)
	if mode != "mining" || overall != "degraded" {
		t.Fatalf("backend-unsafe mode/overall = %s/%s, want mining/degraded", mode, overall)
	}

	mode, overall = liveStatusModeAndOverall(
		true,
		map[string]any{"status": "syncing", "chain_syncing": true},
		true,
		false,
	)
	if mode != "node_syncing" || overall != "degraded" {
		t.Fatalf("sync-blocked mode/overall = %s/%s, want node_syncing/degraded", mode, overall)
	}
}

func TestMarkMiningAdvisorySyncClearsNativeChainSyncFlag(t *testing.T) {
	progress := map[string]any{
		"status":         "synced",
		"chain_syncing":  true,
		"native_current": true,
	}

	markMiningAdvisorySync(progress)

	if got := progress["chain_syncing"]; got != false {
		t.Fatalf("chain_syncing = %v, want false", got)
	}
	if got := progress["evm_chain_syncing"]; got != true {
		t.Fatalf("evm_chain_syncing = %v, want true", got)
	}
	if got := progress["mining_advisory_sync"]; got != true {
		t.Fatalf("mining_advisory_sync = %v, want true", got)
	}
	if syncProgressBlocksMining(progress) {
		t.Fatal("advisory sync should not block mining after native chain_syncing is cleared")
	}
}

func TestNativeDAGPayloadKeepsMultiParentsAndLinearSpine(t *testing.T) {
	payload := nativeDAGPayload([]nativeDAGBlock{
		{Hash: "0xaaa", Order: 10, Height: 7, Time: "2026-06-22T15:00:00Z", Parents: []string{"0x111"}},
		{Hash: "0xbbb", Order: 11, Height: 8, Time: "2026-06-22T15:00:01Z", Parents: []string{"0xaaa", "0x111"}},
		{Hash: "0xccc", Order: 12, Height: 9, Time: "2026-06-22T15:00:02Z", Parents: []string{"0xbbb"}},
	}, 3, 0, nativeDAGTipsResult{})
	if got := payload["mode"]; got != "native-dag-linear-spine" {
		t.Fatalf("mode = %v, want native-dag-linear-spine", got)
	}
	nodes := listMaps(payload["nodes"])
	if len(nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(nodes))
	}
	if got := mapString(nodes[1], "parent_count"); got != "2" {
		t.Fatalf("middle parent_count = %q, want 2", got)
	}
	if got := nodes[1]["multi_parent"]; got != true {
		t.Fatalf("middle multi_parent = %v, want true", got)
	}
	edges := listMaps(payload["edges"])
	parentEdges := 0
	linearEdges := 0
	for _, edge := range edges {
		switch mapString(edge, "type") {
		case "dag_parent":
			parentEdges++
		case "linear_path":
			linearEdges++
			if edge["glow"] != true {
				t.Fatalf("linear edge glow = %v, want true", edge["glow"])
			}
		}
	}
	if parentEdges != 4 {
		t.Fatalf("parent edges = %d, want 4", parentEdges)
	}
	if linearEdges != 2 {
		t.Fatalf("linear edges = %d, want 2", linearEdges)
	}
}

func TestNativeDAGPayloadMarksTipsAndProducerMetadata(t *testing.T) {
	producer := "0xD5F8DF0A60bC1Ff4636250b2a2dff319Bf79B1f5"
	t.Setenv("MINING_POOL_ADDRESS", producer)
	payload := nativeDAGPayload([]nativeDAGBlock{
		{Hash: "0xsettled", Order: 10, Height: 7, Time: "2026-06-22T15:00:00Z", CoinbaseAddress: producer},
		{Hash: "0xtip", Order: 11, Height: 8, Time: "2026-06-22T15:00:01Z", Parents: []string{"0xsettled"}},
		{Hash: "0xstale", Order: 12, Height: 9, Time: "2026-06-22T15:00:02Z"},
	}, 3, 0, nativeDAGTipsResult{
		Valid:   []nativeDAGTip{{Hash: "0xtip"}},
		Invalid: []nativeDAGTip{{Hash: "0xstale"}},
	})
	nodes := listMaps(payload["nodes"])
	byHash := map[string]map[string]any{}
	for _, node := range nodes {
		byHash[mapString(node, "hash")] = node
	}
	if byHash["0xtip"]["tip"] != true || mapString(byHash["0xtip"], "tip_status") != "valid" || mapString(byHash["0xtip"], "dag_set") != "tip-valid" {
		t.Fatalf("valid tip node metadata = %#v", byHash["0xtip"])
	}
	if byHash["0xstale"]["tip"] != true || mapString(byHash["0xstale"], "tip_status") != "invalid" || mapString(byHash["0xstale"], "dag_set") != "tip-invalid" {
		t.Fatalf("invalid tip node metadata = %#v", byHash["0xstale"])
	}
	if byHash["0xsettled"]["local_pool"] != true {
		t.Fatalf("local producer not marked local: %#v", byHash["0xsettled"])
	}
	if got := mapString(byHash["0xsettled"], "producer_source"); got != "coinbase-reward" {
		t.Fatalf("producer_source = %q, want coinbase-reward", got)
	}
	if got := mapString(byHash["0xsettled"], "producer_color"); got != stableProducerColor(strings.ToLower(producer)) {
		t.Fatalf("producer_color = %q, want stable color", got)
	}
	if byHash["0xtip"]["producer_pending"] != true {
		t.Fatalf("tip without coinbase should remain pending: %#v", byHash["0xtip"])
	}
}

func TestNativeDAGPayloadEstimatesLayoutOrderForTipsMissingOrder(t *testing.T) {
	payload := nativeDAGPayload([]nativeDAGBlock{
		{Hash: "0xknown1", Order: 1000, Height: 700, Time: "2026-06-22T15:00:00Z"},
		{Hash: "0xknown2", Order: 1001, Height: 701, Time: "2026-06-22T15:00:01Z"},
		{Hash: "0xtip", Order: 0, Height: 702, Time: "2026-06-22T15:00:02Z"},
	}, 3, 0, nativeDAGTipsResult{Invalid: []nativeDAGTip{{Hash: "0xtip"}}})

	nodes := listMaps(payload["nodes"])
	byHash := map[string]map[string]any{}
	for _, node := range nodes {
		byHash[mapString(node, "hash")] = node
	}
	if got := mapString(byHash["0xtip"], "order"); got != "0" {
		t.Fatalf("raw order = %q, want 0", got)
	}
	if got := mapString(byHash["0xtip"], "layout_order"); got != "1002" {
		t.Fatalf("layout_order = %q, want 1002", got)
	}
	if byHash["0xtip"]["order_estimated"] != true {
		t.Fatalf("order_estimated = %v, want true", byHash["0xtip"]["order_estimated"])
	}
	if got := mapString(payload, "order_estimated_count"); got != "1" {
		t.Fatalf("order_estimated_count = %q, want 1", got)
	}
	if payload["best_effort"] != true {
		t.Fatalf("best_effort = %v, want true when tip order is estimated", payload["best_effort"])
	}
}

func TestNativeDAGPayloadMarksBestEffortCoverage(t *testing.T) {
	payload := nativeDAGPayload([]nativeDAGBlock{
		{Hash: "0xaaa", Order: 10, Height: 7, Time: "2026-06-22T15:00:00Z"},
		{Hash: "0xbbb", Order: 11, Height: 8, Time: "2026-06-22T15:00:01Z", Parents: []string{"0xaaa"}},
	}, 4, 2, nativeDAGTipsResult{})
	if payload["best_effort"] != true {
		t.Fatalf("best_effort = %v, want true after skipped RPC results", payload["best_effort"])
	}
	if got := mapString(payload, "status"); got != "degraded" {
		t.Fatalf("status = %q, want degraded after skipped RPC results", got)
	}
	coverage := mapValue(payload, "coverage")
	if got := mapString(coverage, "requested"); got != "4" {
		t.Fatalf("coverage requested = %q, want 4", got)
	}
	if got := mapString(coverage, "available"); got != "2" {
		t.Fatalf("coverage available = %q, want 2", got)
	}
	if got := mapString(coverage, "complete"); got != "false" {
		t.Fatalf("coverage complete = %q, want false", got)
	}
}

func TestNativeDAGPatchFromPayloadIncludesChangedNodeEdges(t *testing.T) {
	payload := nativeDAGPayload([]nativeDAGBlock{
		{Hash: "0xparent", Order: 10, Height: 7, Time: "2026-06-22T15:00:00Z"},
		{Hash: "0xchild", Order: 11, Height: 8, Time: "2026-06-22T15:00:01Z", Parents: []string{"0xparent", "0xmissing"}},
	}, 2, 0, nativeDAGTipsResult{})

	patch := nativeDAGPatchFromPayload(payload, "0xchild", 120)
	nodes := listMaps(patch["nodes"])
	if len(nodes) != 1 || mapString(nodes[0], "hash") != "0xchild" {
		t.Fatalf("patch nodes = %#v, want changed child node", nodes)
	}
	edges := listMaps(patch["edges"])
	if len(edges) != 3 {
		t.Fatalf("patch edges = %d, want 2 parent edges plus linear trace: %#v", len(edges), edges)
	}
	if got := mapString(patch, "window_limit"); got != "120" {
		t.Fatalf("window_limit = %q, want 120", got)
	}
	if patch["best_effort"] != payload["best_effort"] {
		t.Fatalf("patch best_effort = %v, want %v", patch["best_effort"], payload["best_effort"])
	}
}

func TestChainDAGWindowLimitKeepsLargeRedisWindowAndVisibleNinety(t *testing.T) {
	if defaultNativeDAGVisibleWindow != 90 {
		t.Fatalf("defaultNativeDAGVisibleWindow = %d, want 90", defaultNativeDAGVisibleWindow)
	}
	if got := chainDAGWindowLimit(0); got != 600 {
		t.Fatalf("default live DAG Redis window = %d, want 600", got)
	}
	if got := chainDAGWindowLimit(90); got != 90 {
		t.Fatalf("visible DAG window = %d, want 90", got)
	}
	if got := chainDAGWindowLimit(900); got != 600 {
		t.Fatalf("live DAG window cap = %d, want 600", got)
	}
}

func TestNativeDAGRebuildRejectsAllFailedFetches(t *testing.T) {
	if !nativeDAGRebuildHasNoUsableBlocks(nil, 600) {
		t.Fatal("all failed fetches must be treated as a failed rebuild")
	}
	if nativeDAGRebuildHasNoUsableBlocks([]nativeDAGBlock{{Hash: "0xok", Order: 10}}, 599) {
		t.Fatal("partial native DAG rebuilds should remain usable best effort data")
	}
	if nativeDAGRebuildHasNoUsableBlocks(nil, 0) {
		t.Fatal("empty startup without fetch failures should remain warming, not a failed rebuild")
	}
}

func TestNativeDAGLiveSnapshotSourceDegraded(t *testing.T) {
	for _, payload := range []map[string]any{
		{"status": "stale"},
		{"status": "error"},
		{"status": "degraded"},
		{"status": "ok", "live_pipeline_status": "source-degraded"},
		{"status": "ok", "coverage": map[string]any{"requested": 600, "failures": 600}},
	} {
		if !nativeDAGLiveSnapshotSourceDegraded(payload) {
			t.Fatalf("payload should be source degraded: %#v", payload)
		}
	}
	if nativeDAGLiveSnapshotSourceDegraded(map[string]any{"status": "ok", "live_pipeline_status": "redis-incremental"}) {
		t.Fatal("healthy native DAG snapshot should not be treated as source degraded")
	}
}

func TestNativeDAGPreserveStaleDiagnostics(t *testing.T) {
	payload := map[string]any{
		"last_rebuild_target_order": float64(12647141),
		"last_rebuild_failures":     float64(90),
	}
	targetOrder, failures := nativeDAGPreserveStaleDiagnostics(payload, 0, 0)
	if targetOrder != 12647141 || failures != 90 {
		t.Fatalf("diagnostics = target %d failures %d, want target 12647141 failures 90", targetOrder, failures)
	}
	targetOrder, failures = nativeDAGPreserveStaleDiagnostics(payload, 12650000, 3)
	if targetOrder != 12650000 || failures != 3 {
		t.Fatalf("explicit diagnostics = target %d failures %d, want target 12650000 failures 3", targetOrder, failures)
	}
	_, failures = nativeDAGPreserveStaleDiagnostics(map[string]any{"last_rebuild_failures": -1}, 0, -5)
	if failures != 0 {
		t.Fatalf("negative failures should clamp to zero, got %d", failures)
	}
}

func TestTrimNativeDAGPayloadWindowKeepsNewestVisibleWindow(t *testing.T) {
	nodes := make([]map[string]any, 0, 120)
	edges := make([]map[string]any, 0, 119)
	for order := 120; order >= 1; order-- {
		hash := fmt.Sprintf("0x%03d", order)
		nodes = append(nodes, map[string]any{
			"hash":         hash,
			"order":        order,
			"layout_order": order,
		})
		if order > 1 {
			edges = append(edges, map[string]any{
				"child":  hash,
				"parent": fmt.Sprintf("0x%03d", order-1),
				"known":  true,
			})
		}
	}
	payload := map[string]any{
		"mode":     "native-dag-linear-spine",
		"source":   "native-rpc-live",
		"nodes":    nodes,
		"edges":    edges,
		"window":   map[string]any{"requested": 600, "available": 120},
		"coverage": map[string]any{"requested": 600, "available": 120},
	}

	trimNativeDAGPayloadWindow(payload, 90)

	trimmedNodes := listMaps(payload["nodes"])
	if len(trimmedNodes) != 90 {
		t.Fatalf("trimmed nodes = %d, want 90", len(trimmedNodes))
	}
	if mapString(trimmedNodes[0], "hash") != "0x120" || mapString(trimmedNodes[len(trimmedNodes)-1], "hash") != "0x031" {
		t.Fatalf("trimmed node range = %s..%s, want newest 90", mapString(trimmedNodes[0], "hash"), mapString(trimmedNodes[len(trimmedNodes)-1], "hash"))
	}
	for _, edge := range listMaps(payload["edges"]) {
		if mapString(edge, "parent") == "0x030" {
			t.Fatalf("trimmed edges must not include known parent outside visible window: %#v", edge)
		}
	}
	window := mapValue(payload, "window")
	if mapString(window, "requested") != "90" || mapString(window, "available") != "90" {
		t.Fatalf("trimmed window = %#v, want requested/available 90", window)
	}
}

func TestMinerHealthFromASICEventsBuildsManagedRows(t *testing.T) {
	t.Setenv("BDAG_POOL_URL", "stratum+tcp://192.168.1.100:3334")
	t.Setenv("MINING_POOL_ADDRESS", "0x1719E0ee598c15957448D5E568948101DF78e7A0")

	health := minerHealthFromASICEvents([]map[string]any{
		{
			"mac":        "28:e2:97:4d:44:3a",
			"ip":         "192.168.1.105",
			"status":     "configured",
			"source":     "env-overrides",
			"updated_at": "2026-06-19T21:18:08Z",
		},
		{
			"mac":        "2a:71:c7:f5:1f:1e",
			"ip":         "192.168.1.101",
			"status":     "configured",
			"source":     "env-overrides",
			"updated_at": "2026-06-19T21:18:08Z",
		},
	})

	if got := health["managed_count"]; got != 2 {
		t.Fatalf("managed_count = %v, want 2", got)
	}
	rows := health["managed_miners"].([]map[string]any)
	if len(rows) != 2 {
		t.Fatalf("managed rows = %d, want 2", len(rows))
	}
	if got := rows[0]["ip"]; got != "192.168.1.101" {
		t.Fatalf("first row IP = %v, want sorted ASIC 192.168.1.101", got)
	}
	if got := rows[0]["configured_pool_url"]; got != "stratum+tcp://192.168.1.100:3334" {
		t.Fatalf("configured_pool_url = %v", got)
	}
	if got := minerRows(map[string]any{"miner_health": health}); len(got) != 2 {
		t.Fatalf("minerRows count = %d, want 2", len(got))
	}
}

func TestASICWalletUsesOnlyAuthenticatedWorker(t *testing.T) {
	t.Setenv("MINING_POOL_ADDRESS", "0xoperator")
	health := minerHealthFromASICEvents([]map[string]any{{"mac": "28:e2:97:4d:44:3a", "ip": "192.168.1.105"}})
	row := health["managed_miners"].([]map[string]any)[0]
	if got := mapString(row, "intended_wallet"); got != "" {
		t.Fatalf("invented owner from operator coinbase: %q", got)
	}
	client := map[string]any{"asic_mac": "28:e2:97:4d:44:3a", "authorized": true, "authorized_worker": "0xowner"}
	lane := derivedASICLane(row, client, nil, time.Now())
	mergeASICPerformanceFields(row, lane)
	if got := mapString(row, "intended_wallet"); got != "0xowner" {
		t.Fatalf("authenticated owner = %q, want 0xowner", got)
	}
	client["authorized"] = false
	if got := mapString(derivedASICLane(nil, client, nil, time.Now()), "intended_wallet"); got != "" {
		t.Fatalf("unauthenticated owner = %q, want empty", got)
	}
}

func TestPoolJobCountsUseFreshPhysicalConnections(t *testing.T) {
	health := map[string]any{"connected_count": 3}
	applyFreshPoolJobCounts(health, map[string]any{"age_seconds": 0, "physical_miners": 1})
	if health["connected_count"] != 1 || health["connected_count_effective"] != 1 {
		t.Fatalf("fresh physical connections not applied: %#v", health)
	}
	applyFreshPoolJobCounts(health, map[string]any{"age_seconds": 20, "physical_miners": 3})
	if health["connected_count"] != 1 {
		t.Fatalf("stale pool state changed connections: %#v", health)
	}
}

func TestMinerHealthFromASICEventsFiltersToAllowlistedASICs(t *testing.T) {
	t.Setenv("POOL_ASIC_MAC_ALLOWLIST", "28:e2:97:4d:44:3a")

	health := minerHealthFromASICEvents([]map[string]any{
		{
			"mac":    "fa:e7:9a:2d:be:86",
			"ip":     "172.18.0.1",
			"status": "connected",
			"source": "host-arp",
		},
		{
			"mac":    "28:e2:97:4d:44:3a",
			"ip":     "192.168.1.105",
			"status": "configured",
			"source": "env-overrides",
		},
	})

	if got := health["managed_count"]; got != 1 {
		t.Fatalf("managed_count = %v, want only allowlisted ASIC", got)
	}
	rows := health["managed_miners"].([]map[string]any)
	if got := rows[0]["mac"]; got != "28:e2:97:4d:44:3a" {
		t.Fatalf("row mac = %v, want allowlisted ASIC", got)
	}
}

func TestApplyPoolReadinessMarksManagedASICRowsConnected(t *testing.T) {
	health := minerHealthFromASICEvents([]map[string]any{
		{"mac": "28:e2:97:1e:c0:b5", "ip": "192.168.1.101", "status": "configured"},
		{"mac": "2a:71:c7:f5:1f:1e", "ip": "192.168.1.102", "status": "configured"},
	})
	applyPoolReadinessToManagedASICRows(health, map[string]any{
		"active_connections": "2",
		"authorized_miners":  "2",
		"ready_miners":       "2",
	})

	if got := health["connected_count"]; got != 2 {
		t.Fatalf("connected_count = %v, want 2", got)
	}
	if got := health["connected_count_effective"]; got != 2 {
		t.Fatalf("connected_count_effective = %v, want 2", got)
	}
	rows := health["managed_miners"].([]map[string]any)
	for _, row := range rows {
		if row["status"] != "connected" || row["connected"] != true || row["connected_source"] != "pool-metrics" {
			t.Fatalf("row not marked connected from pool metrics: %#v", row)
		}
	}
}

func TestMergeASICPerformanceIntoManagedRows(t *testing.T) {
	health := minerHealthFromASICEvents([]map[string]any{
		{"mac": "2a:71:c7:f5:1f:1e", "ip": "192.168.1.101", "status": "configured"},
	})
	mergeASICPerformanceIntoMinerHealth(health, map[string]any{
		"lanes": []any{
			map[string]any{
				"lane_id":               "mac:2A:71:C7:F5:1F:1E",
				"lane_identity_source":  "mac",
				"asic_mac":              "2A:71:C7:F5:1F:1E",
				"remote_host":           "192.168.1.101",
				"ready":                 true,
				"health":                "ok",
				"protocol_variant":      "stratum-v1-extranonce",
				"extranonce_subscribed": true,
				"current_job_age_ms":    float64(1500),
				"blocks_30m":            float64(3),
				"blocks_1h":             float64(7),
				"blocks_24h":            float64(92),
				"rejected_30m":          float64(1),
				"rejected_local_30m":    float64(2),
				"last_accepted_unix_ms": float64(1782043200000),
			},
		},
	})

	rows := health["managed_miners"].([]map[string]any)
	row := rows[0]
	if row["status"] != "ok" ||
		row["connected"] != true ||
		row["protocol_variant"] != "stratum-v1-extranonce" ||
		row["blocks_30m"] != "3" ||
		row["blocks_1h"] != "7" ||
		row["blocks_24h"] != "92" ||
		row["rejected_30m"] != "1" ||
		row["rejected_local_30m"] != "2" {
		t.Fatalf("merged row = %#v", row)
	}
}

func TestMergeASICTelemetryIntoManagedRows(t *testing.T) {
	health := minerHealthFromASICEvents([]map[string]any{
		{"mac": "2a:71:c7:f5:1f:1e", "ip": "192.168.1.102", "status": "configured"},
	})
	mergeASICTelemetryIntoMinerHealth(health, map[string]any{
		"devices": []any{
			map[string]any{
				"mac":         "2A:71:C7:F5:1F:1E",
				"ip":          "192.168.1.102",
				"status":      "ok",
				"age_seconds": 2,
				"summary": map[string]any{
					"model":             "X100",
					"firmware":          "1.2.3",
					"manual_powerplan":  "850 MHz 0.76 V 15 RPM",
					"clock_mhz":         "850 MHz",
					"voltage_v":         "0.76 V",
					"chip_count":        "6",
					"temperature":       "58",
					"fanspeed":          "3000",
					"hashrate":          "1.00 TH/s",
					"active_pool_url":   "stratum+tcp://192.168.1.100:3334",
					"active_pool_user":  "0x1719...e7a0",
					"active_pool_legal": true,
					"pool_pass_present": true,
					"miner_status":      "0",
					"accepted":          "12",
					"rejected":          "1",
					"hw_errors":         "0",
				},
			},
		},
	})

	rows := health["managed_miners"].([]map[string]any)
	row := rows[0]
	if row["device_model"] != "X100" ||
		row["clock_mhz"] != "850 MHz" ||
		row["voltage_v"] != "0.76 V" ||
		row["chip_count"] != "6" ||
		row["active_pool_url"] != "stratum+tcp://192.168.1.100:3334" ||
		row["pool_pass_present"] != true ||
		row["device_telemetry_status"] != "ok" {
		t.Fatalf("merged telemetry row = %#v", row)
	}
}

func TestMinerHealthFromASICPerformanceBuildsRowsWithoutInventory(t *testing.T) {
	health := minerHealthFromASICPerformance(map[string]any{
		"lanes": []any{
			map[string]any{
				"lane_id":     "mac:28:E2:97:4D:C0:B5",
				"asic_mac":    "28:E2:97:4D:C0:B5",
				"remote_host": "192.168.1.102",
				"ready":       true,
				"health":      "ok",
				"blocks_1h":   float64(11),
			},
		},
	})
	if health == nil {
		t.Fatal("health is nil")
	}
	if got := health["managed_count"]; got != 1 {
		t.Fatalf("managed_count = %v, want 1", got)
	}
	rows := health["managed_miners"].([]map[string]any)
	if rows[0]["mac"] != "28:e2:97:4d:c0:b5" || rows[0]["blocks_1h"] != "11" {
		t.Fatalf("row = %#v", rows[0])
	}
}

func TestDerivedASICPerformanceUsesJobStateAndTelemetry(t *testing.T) {
	jobState := map[string]any{
		"clients": []any{
			map[string]any{
				"lane_id":                "mac:28:e2:97:2e:00:1b",
				"asic_mac":               "28:e2:97:2e:00:1b",
				"remote_host":            "192.168.100.80",
				"ready":                  true,
				"reason_code":            "ok",
				"current_job_age_ms":     float64(1500),
				"expired_window_rejects": float64(2),
			},
		},
	}
	minerHealth := map[string]any{
		"managed_miners": []map[string]any{
			{
				"managed":         true,
				"device_type":     "asic",
				"mac":             "28:e2:97:2e:00:1b",
				"ip":              "192.168.100.80",
				"connected":       true,
				"device_accepted": "29",
				"device_rejected": "1",
				"last_seen_at":    "2026-06-23T14:08:16Z",
			},
		},
	}
	payload := derivedASICPerformancePayload(jobState, minerHealth)
	if payload == nil {
		t.Fatal("derived ASIC performance payload is nil")
	}
	lanes := listMaps(payload["lanes"])
	if len(lanes) != 1 {
		t.Fatalf("lanes = %d, want 1", len(lanes))
	}
	lane := lanes[0]
	if mapString(lane, "asic_mac") != "28:e2:97:2e:00:1b" || mapString(lane, "health") != "connected" || !truthyValue(lane["ready"]) {
		t.Fatalf("lane identity/health = %#v", lane)
	}
	if got := mapString(lane, "blocks_1h"); got != "" {
		t.Fatalf("derived lane blocks_1h = %q, want absent because block attribution is unavailable", got)
	}
	rows := asicPerformanceRows(map[string]any{"asic_performance": payload})
	if rows[0]["blocks_1h"] != "n/a" || rows[0]["last_block"] == "not known" {
		t.Fatalf("derived ASIC row = %#v", rows[0])
	}
}

func TestDerivedASICPerformanceDoesNotTreatARPOnlyASICAsReady(t *testing.T) {
	minerHealth := map[string]any{
		"managed_miners": []map[string]any{
			{
				"managed":     true,
				"device_type": "asic",
				"mac":         "28:e2:97:3d:95:13",
				"ip":          "192.168.100.80",
				"status":      "connected",
				"health":      "connected",
				"connected":   true,
				"source":      "host-arp",
			},
		},
	}
	payload := derivedASICPerformancePayload(map[string]any{}, minerHealth)
	if payload == nil {
		t.Fatal("derived ASIC performance payload is nil")
	}
	lanes := listMaps(payload["lanes"])
	if len(lanes) != 1 {
		t.Fatalf("lanes = %d, want 1", len(lanes))
	}
	lane := lanes[0]
	if truthyValue(lane["ready"]) {
		t.Fatalf("ARP-only lane was marked ready: %#v", lane)
	}
	if got := mapString(lane, "health"); got != "no-stratum" {
		t.Fatalf("ARP-only lane health = %q, want no-stratum", got)
	}
}

func nativeRPCTestServer(t *testing.T, results map[string]any) *httptest.Server {
	t.Helper()
	return nativeRPCTestServerFunc(t, func(method string) (any, int) {
		result, ok := results[method]
		if !ok {
			if method == "eth_syncing" {
				return false, http.StatusOK
			}
			return "unexpected method", http.StatusNotFound
		}
		return result, http.StatusOK
	})
}

func nativeRPCTestServerFunc(t *testing.T, resultFor func(method string) (any, int)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string `json:"method"`
			ID     any    `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		result, status := resultFor(request.Method)
		if status >= 400 {
			http.Error(w, fmt.Sprint(result), status)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      request.ID,
			"result":  result,
		})
	}))
}

func resetNativeSyncProgressCacheForTest() {
	nativeSyncProgressCache.mu.Lock()
	nativeSyncProgressCache.key = ""
	nativeSyncProgressCache.payload = nil
	nativeSyncProgressCache.fetchedAt = time.Time{}
	nativeSyncProgressCache.expires = time.Time{}
	nativeSyncProgressCache.mu.Unlock()
}
