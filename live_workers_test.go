package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLatestRetrievableBlockUsesLatestTagWhenItMatchesBlockNumber(t *testing.T) {
	var blockNumberCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		var result any
		switch req.Method {
		case "eth_blockNumber":
			atomic.AddInt32(&blockNumberCalls, 1)
			result = "0x3ed"
		case "eth_getBlockByNumber":
			tag, _ := req.Params[0].(string)
			if tag == "latest" {
				result = map[string]any{
					"number":     "0x3e8",
					"hash":       "0xlatest",
					"parentHash": "0xparent",
					"timestamp":  "0x64",
					"miner":      "0xproducer",
				}
			} else {
				result = map[string]any{
					"number":     tag,
					"hash":       "0xnumeric",
					"parentHash": "0xparent",
					"timestamp":  "0x64",
					"miner":      "0xproducer",
				}
			}
		default:
			t.Fatalf("unexpected rpc method %s", req.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		})
	}))
	defer server.Close()

	worker := &liveWorkers{
		httpClient: server.Client(),
		evmHTTP:    server.URL,
		windowSize: defaultGlobalBlockWindow,
	}
	header, err := worker.latestRetrievableBlock(context.Background())
	if err != nil {
		t.Fatalf("latestRetrievableBlock returned error: %v", err)
	}
	if got := header.height(); got != 1000 {
		t.Fatalf("height = %d, want 1000", got)
	}
	if calls := atomic.LoadInt32(&blockNumberCalls); calls != 1 {
		t.Fatalf("eth_blockNumber calls = %d, want 1 to validate latest tag freshness", calls)
	}
}

func TestLatestRetrievableBlockRejectsStaleLatestTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		var result any
		switch req.Method {
		case "eth_blockNumber":
			result = "0x3e8"
		case "eth_getBlockByNumber":
			tag, _ := req.Params[0].(string)
			if tag == "latest" {
				result = map[string]any{
					"number":     "0x64",
					"hash":       "0xstale",
					"parentHash": "0xparent",
					"timestamp":  "0x64",
					"miner":      "0xproducer",
				}
			}
		default:
			t.Fatalf("unexpected rpc method %s", req.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		})
	}))
	defer server.Close()

	worker := &liveWorkers{
		httpClient: server.Client(),
		evmHTTP:    server.URL,
		windowSize: defaultGlobalBlockWindow,
	}
	_, err := worker.latestRetrievableBlock(context.Background())
	if err == nil {
		t.Fatal("latestRetrievableBlock returned nil error for stale latest tag")
	}
	if !strings.Contains(err.Error(), "behind eth_blockNumber") {
		t.Fatalf("error = %v, want stale latest tag diagnostic", err)
	}
}

func TestPoolASICPerformanceURLDerivedFromMetricsURL(t *testing.T) {
	got := poolASICPerformanceURLFromMetrics("http://pool:9090/metrics?x=1")
	want := "http://pool:9090/health/asic-performance"
	if got != want {
		t.Fatalf("poolASICPerformanceURLFromMetrics() = %q, want %q", got, want)
	}
}

func TestPoolJobStateURLDerivedFromMetricsURL(t *testing.T) {
	got := poolHealthURLFromMetrics("http://pool:9090/metrics?x=1", "/health/job-state", defaultPoolJobStateURL)
	want := "http://pool:9090/health/job-state"
	if got != want {
		t.Fatalf("poolHealthURLFromMetrics() = %q, want %q", got, want)
	}
}

func TestASICTelemetrySanitizesPoolsAndParsesPowerPlan(t *testing.T) {
	pools := sanitizeASICPools([]any{
		map[string]any{
			"url":    "stratum+tcp://192.168.1.100:3334",
			"user":   "0x1719E0ee598c15957448D5E568948101DF78e7A0",
			"pass":   "secret",
			"active": true,
			"legal":  true,
		},
	})
	if len(pools) != 1 {
		t.Fatalf("pools len = %d, want 1", len(pools))
	}
	if _, ok := pools[0]["pass"]; ok {
		t.Fatalf("pool password was not redacted: %#v", pools[0])
	}
	if pools[0]["pass_present"] != true {
		t.Fatalf("pass_present = %v, want true", pools[0]["pass_present"])
	}

	summary := asicTelemetrySummary(map[string]any{
		"controller_status": map[string]any{"model": "X100", "firmware": "1.2.3"},
		"pool_config":       pools,
		"settings": map[string]any{
			"manual":          true,
			"manualPowerplan": "850 MHz 0.76 V 15 RPM 9.4 RPM",
		},
		"cgminer_devs": map[string]any{
			"data": []any{map[string]any{
				"valid":       6,
				"hashrate":    "1.00 TH/s",
				"temp":        "58",
				"fanspeed":    "3000",
				"accepted":    12,
				"rejected":    1,
				"hwerrors":    0,
				"minerstatus": 0,
			}},
		},
	})
	if summary["clock_mhz"] != "850 MHz" || summary["voltage_v"] != "0.76 V" {
		t.Fatalf("clock/voltage summary = %#v", summary)
	}
	if summary["chip_count"] != "6" {
		t.Fatalf("chip_count = %v, want 6", summary["chip_count"])
	}
	if summary["pool_pass_present"] != true {
		t.Fatalf("pool_pass_present = %v, want true", summary["pool_pass_present"])
	}
	point := asicHashrateHistoryPoint(map[string]any{
		"status":  "ok",
		"summary": summary,
	}, time.Unix(1782229200, 0).UTC())
	if point["t"] != "2026-06-23T15:40:00Z" {
		t.Fatalf("history point time = %v", point["t"])
	}
	if point["hashrate"] != float64(1000) {
		t.Fatalf("history point hashrate = %v, want 1000 GH/s from 1 TH/s", point["hashrate"])
	}
	if point["accepted"] != float64(12) || point["rejected"] != float64(1) {
		t.Fatalf("history point share counters = %#v", point)
	}
}

func TestLatestRetrievableBlockStopsOnExplicitEVMSyncing(t *testing.T) {
	var latestCalls int32
	var blockNumberCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		switch req.Method {
		case "eth_getBlockByNumber":
			atomic.AddInt32(&latestCalls, 1)
			http.Error(w, "syncing but no active peers", http.StatusServiceUnavailable)
		case "eth_blockNumber":
			atomic.AddInt32(&blockNumberCalls, 1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  "0x64",
			})
		default:
			t.Fatalf("unexpected rpc method %s", req.Method)
		}
	}))
	defer server.Close()

	worker := &liveWorkers{
		httpClient: server.Client(),
		evmHTTP:    server.URL,
		windowSize: defaultGlobalBlockWindow,
	}
	_, err := worker.latestRetrievableBlock(context.Background())
	if err == nil {
		t.Fatal("latestRetrievableBlock returned nil error during explicit EVM syncing")
	}
	if !isEVMSyncBackoffError(err) {
		t.Fatalf("error was not classified for EVM sync backoff: %v", err)
	}
	if calls := atomic.LoadInt32(&latestCalls); calls != 1 {
		t.Fatalf("eth_getBlockByNumber calls = %d, want 1", calls)
	}
	if calls := atomic.LoadInt32(&blockNumberCalls); calls != 0 {
		t.Fatalf("eth_blockNumber calls = %d, want 0", calls)
	}
}

func TestEVMSyncBackoffRecordsCooldown(t *testing.T) {
	worker := &liveWorkers{evmSyncBackoff: time.Minute}
	if worker.evmSyncBackoffActive() {
		t.Fatal("new worker has active EVM sync backoff")
	}
	if worker.noteEVMSyncBackoff(errors.New("temporary dial failure")) {
		t.Fatal("unrelated error should not trigger EVM sync backoff")
	}
	if !worker.noteEVMSyncBackoff(errors.New("EVM RPC eth_getBlockByNumber returned 503 Service Unavailable: syncing but no active peers")) {
		t.Fatal("explicit EVM syncing error did not trigger backoff")
	}
	if !worker.evmSyncBackoffActive() {
		t.Fatal("backoff should be active after explicit EVM syncing error")
	}
	remaining, ok := worker.evmSyncBackoffRemaining()
	if !ok || remaining <= 0 || remaining > time.Minute {
		t.Fatalf("remaining backoff = %v, active = %v; want within configured duration", remaining, ok)
	}

	worker.mu.Lock()
	worker.evmSyncBackoffUntil = time.Now().Add(-time.Second)
	worker.mu.Unlock()
	if worker.evmSyncBackoffActive() {
		t.Fatal("expired EVM sync backoff should not remain active")
	}
}

func TestLowImpactConfigFromEnvUsesBoundedThrottles(t *testing.T) {
	t.Setenv("BDAG_DASHBOARD_LOW_IMPACT_MODE", "on")
	t.Setenv("BDAG_DASHBOARD_LOW_IMPACT_LAG_BLOCKS", "12")
	t.Setenv("BDAG_LOW_IMPACT_TREND_RECENT_BLOCKS", "90")
	t.Setenv("BDAG_LOW_IMPACT_TREND_CHUNK_SIZE", "12")
	t.Setenv("BDAG_LOW_IMPACT_TREND_RPC_WORKERS", "1")
	t.Setenv("BDAG_LOW_IMPACT_TREND_SPARSE_SAMPLES_PER_PASS", "1")
	t.Setenv("BDAG_LOW_IMPACT_TREND_SPARSE_MAX_SAMPLE_BLOCKS", "48")
	t.Setenv("BDAG_LOW_IMPACT_TREND_CATCHUP_INTERVAL_SECONDS", "300")
	t.Setenv("BDAG_LOW_IMPACT_TREND_BACKFILL_SLEEP_MS", "750")
	t.Setenv("BDAG_LOW_IMPACT_NATIVE_DAG_POLL_MS", "6000")
	t.Setenv("BDAG_LOW_IMPACT_NATIVE_DAG_MAX_BLOCKS_PER_PASS", "3")

	cfg := lowImpactConfigFromEnv()
	if cfg.Mode != "on" {
		t.Fatalf("mode = %q, want on", cfg.Mode)
	}
	if cfg.LagBlocks != 12 {
		t.Fatalf("low-impact lag blocks = %d, want 12", cfg.LagBlocks)
	}
	if cfg.TrendRecentBlocks != 90 || cfg.TrendChunkSize != 12 || cfg.TrendRPCWorkers != 1 || cfg.TrendSamplesPerPass != 1 || cfg.TrendSampleMaxBlocks != 48 {
		t.Fatalf("trend low-impact config = %#v", cfg)
	}
	if cfg.TrendCatchupEvery != 5*time.Minute || cfg.TrendChunkSleep != 750*time.Millisecond {
		t.Fatalf("trend durations = catchup %v sleep %v", cfg.TrendCatchupEvery, cfg.TrendChunkSleep)
	}
	if cfg.NativeDAGPoll != 6*time.Second || cfg.NativeDAGMaxPass != 3 {
		t.Fatalf("native DAG low-impact config = %#v", cfg)
	}
}

func TestForcedLowImpactCapsTrendAndNativeDAGWorkWithoutShrinkingLiveWindow(t *testing.T) {
	worker := &liveWorkers{
		windowSize:           600,
		trendRecentBlocks:    3600,
		trendChunkSize:       128,
		trendRPCWorkers:      4,
		trendSamplesPerPass:  24,
		trendSampleMaxBlocks: 2048,
		trendCatchupEvery:    30 * time.Second,
		trendChunkSleep:      100 * time.Millisecond,
		nativeDAGPoll:        time.Second,
		nativeDAGWindow:      120,
		nativeDAGMaxPass:     24,
		lowImpact: lowImpactConfig{
			Mode:                 "on",
			TrendRecentBlocks:    240,
			TrendChunkSize:       16,
			TrendRPCWorkers:      1,
			TrendSamplesPerPass:  1,
			TrendSampleMaxBlocks: 128,
			TrendCatchupEvery:    5 * time.Minute,
			TrendChunkSleep:      time.Second,
			NativeDAGPoll:        5 * time.Second,
			NativeDAGMaxPass:     4,
		},
	}
	worker.applyForcedLowImpactStartupLimits()

	limits := worker.trendLimits(context.Background())
	if limits.recentBlocks != 240 || limits.chunkSize != 16 || limits.rpcWorkers != 1 || limits.samplesPerPass != 1 || limits.sampleMaxBlocks != 128 {
		t.Fatalf("trend limits = %#v", limits)
	}
	if limits.catchupEvery != 5*time.Minute || limits.chunkSleep != time.Second {
		t.Fatalf("trend durations = catchup %v sleep %v", limits.catchupEvery, limits.chunkSleep)
	}
	poll, window, maxPass, active := worker.nativeDAGLimits(context.Background())
	if !active || poll != 5*time.Second || window != 120 || maxPass != 4 {
		t.Fatalf("native DAG limits = poll %v window %d maxPass %d active %v", poll, window, maxPass, active)
	}
}

func TestNativeDAGLargeGapRebuildsEvenWhenLowImpactWouldCapPass(t *testing.T) {
	if !nativeDAGNeedsRebuildForGap(241, 240) {
		t.Fatal("large native DAG cursor gaps must force a rebase/rebuild")
	}
	if nativeDAGNeedsRebuildForGap(240, 240) {
		t.Fatal("native DAG cursor gap at the threshold should stay incremental")
	}
}

func TestNativeDAGStalledOrderFailureTracking(t *testing.T) {
	worker := &liveWorkers{}
	if got := worker.noteNativeDAGFetchFailure(100); got != 1 {
		t.Fatalf("first failed order count = %d, want 1", got)
	}
	if got := worker.noteNativeDAGFetchFailure(100); got != 2 {
		t.Fatalf("second failed order count = %d, want 2", got)
	}
	if got := worker.noteNativeDAGFetchFailure(101); got != 1 {
		t.Fatalf("new failed order count = %d, want reset to 1", got)
	}
	worker.clearNativeDAGFetchFailures(100)
	if worker.nativeDAGStalledOrder != 101 || worker.nativeDAGStalledFailures != 1 {
		t.Fatalf("clearing a different order should not reset current stalled order: order=%d failures=%d", worker.nativeDAGStalledOrder, worker.nativeDAGStalledFailures)
	}
	worker.clearNativeDAGFetchFailures(101)
	if worker.nativeDAGStalledOrder != 0 || worker.nativeDAGStalledFailures != 0 {
		t.Fatalf("stalled order state was not cleared: order=%d failures=%d", worker.nativeDAGStalledOrder, worker.nativeDAGStalledFailures)
	}
}

func TestNativeDAGRebuildAttemptCooldownSuppressesImmediateRetry(t *testing.T) {
	t.Setenv("BDAG_NATIVE_DAG_REBUILD_ATTEMPT_COOLDOWN_SECONDS", "60")
	worker := &liveWorkers{rpcBudget: newNodeRPCBudget(1, time.Second), lastNativeDAGRepairAttempt: time.Now()}
	rebuilt, err := worker.tryNativeDAGRebuildLiveWindow(context.Background(), 90, "gap-too-large", false)
	if err != nil {
		t.Fatalf("cooldown-suppressed rebuild returned error: %v", err)
	}
	if rebuilt {
		t.Fatal("cooldown-suppressed rebuild should report no rebuild")
	}
}

func TestNativeDAGRebuildSkipsWhileNodeRPCBackoffActive(t *testing.T) {
	budget := newNodeRPCBudget(1, time.Hour)
	budget.noteBusy("503 service unavailable")
	worker := &liveWorkers{rpcBudget: budget}

	rebuilt, err := worker.tryNativeDAGRebuildLiveWindow(withLowPriorityNodeRPC(context.Background()), 90, "periodic-heal", true)

	if rebuilt {
		t.Fatal("native DAG rebuild should not run during node RPC backoff")
	}
	if !isNodeRPCBusyError(err) {
		t.Fatalf("rebuild error = %v, want node RPC backoff", err)
	}
}

func TestNativeDAGOrderRangeKeepsMovingPastUnavailableOrder(t *testing.T) {
	worker := &liveWorkers{nativeDAGRPC: "http://native-rpc"}
	fetches := []uint64{}
	applied := []uint64{}
	result, err := worker.applyNativeDAGOrderRange(
		context.Background(),
		90,
		101,
		104,
		func(_ context.Context, _ string, order uint64) (nativeDAGBlock, error) {
			fetches = append(fetches, order)
			if order == 101 {
				return nativeDAGBlock{}, errors.New("getBlockByOrder error -32600: unauthorized")
			}
			return nativeDAGBlock{Hash: fmt.Sprintf("0x%x", order), Order: int64(order), Height: int64(order)}, nil
		},
		func(_ context.Context, _ string, block nativeDAGBlock, _ int) (map[string]any, error) {
			applied = append(applied, uint64(block.Order))
			return map[string]any{}, nil
		},
	)

	if err != nil {
		t.Fatalf("applyNativeDAGOrderRange returned error: %v", err)
	}
	if result.applied != 3 || result.skipped != 1 || result.firstFailureOrder != 101 {
		t.Fatalf("result = %#v, want 3 applied, 1 skipped at 101", result)
	}
	if got := strings.Trim(fmt.Sprint(fetches), "[]"); got != "101 102 103 104" {
		t.Fatalf("fetches = %v", fetches)
	}
	if got := strings.Trim(fmt.Sprint(applied), "[]"); got != "102 103 104" {
		t.Fatalf("applied = %v", applied)
	}
}

func TestNativeDAGOrderRangeDoesNotFetchDuringNodeRPCBackoff(t *testing.T) {
	budget := newNodeRPCBudget(1, time.Hour)
	budget.noteBusy("503 service unavailable")
	worker := &liveWorkers{nativeDAGRPC: "http://native-rpc", rpcBudget: budget}
	var fetches int32

	result, err := worker.applyNativeDAGOrderRange(
		withLowPriorityNodeRPC(context.Background()),
		90,
		101,
		104,
		func(_ context.Context, _ string, order uint64) (nativeDAGBlock, error) {
			atomic.AddInt32(&fetches, 1)
			return nativeDAGBlock{Hash: fmt.Sprintf("0x%x", order), Order: int64(order), Height: int64(order)}, nil
		},
		func(_ context.Context, _ string, _ nativeDAGBlock, _ int) (map[string]any, error) {
			t.Fatal("backoff should prevent native DAG apply")
			return nil, nil
		},
	)

	if err != nil {
		t.Fatalf("applyNativeDAGOrderRange returned error: %v", err)
	}
	if got := atomic.LoadInt32(&fetches); got != 0 {
		t.Fatalf("fetches = %d, want 0 during node RPC backoff", got)
	}
	if !isNodeRPCBusyError(result.firstErr) || result.firstFailureOrder != 101 {
		t.Fatalf("result = %#v, want node RPC backoff at first order", result)
	}
}

func TestNativeDAGOrderRangeTreatsTerminalNotReadyAsSoftWait(t *testing.T) {
	worker := &liveWorkers{nativeDAGRPC: "http://native-rpc"}
	result, err := worker.applyNativeDAGOrderRange(
		context.Background(),
		90,
		105,
		105,
		func(_ context.Context, _ string, _ uint64) (nativeDAGBlock, error) {
			return nativeDAGBlock{}, errors.New("getBlockByOrder error -32603: order is too big")
		},
		func(_ context.Context, _ string, _ nativeDAGBlock, _ int) (map[string]any, error) {
			t.Fatal("terminal not-ready order must not be applied")
			return nil, nil
		},
	)

	if err != nil {
		t.Fatalf("applyNativeDAGOrderRange returned error: %v", err)
	}
	if !result.terminalNotReady || result.firstErr != nil || result.applied != 0 || result.skipped != 0 {
		t.Fatalf("result = %#v, want soft terminal wait", result)
	}
}

func TestFetchBlockRangeBoundedSkipsNetworkDuringLowPriorityBackoff(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer server.Close()

	budget := newNodeRPCBudget(1, time.Hour)
	budget.noteBusy("503 service unavailable")
	worker := &liveWorkers{
		evmHTTP:    server.URL,
		httpClient: server.Client(),
		rpcBudget:  budget,
		windowSize: 600,
	}

	headers, err := worker.fetchBlockRangeBounded(withLowPriorityNodeRPC(context.Background()), 1, 3, 2, 10)

	if len(headers) != 0 {
		t.Fatalf("headers = %d, want 0 during node RPC backoff", len(headers))
	}
	if !isNodeRPCBusyError(err) {
		t.Fatalf("fetchBlockRangeBounded error = %v, want node RPC backoff", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("HTTP calls = %d, want 0 during node RPC backoff", got)
	}
}

func TestASICStatusEventsReconcileStaleOverridesWithHostARP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arp")
	if err := os.WriteFile(path, []byte(`IP address       HW type     Flags       HW address            Mask     Device
192.168.1.14     0x1         0x2         28:e2:97:3e:39:63     *        eth0
192.168.1.101    0x1         0x2         28:e2:97:1e:c0:b5     *        eth0
192.168.1.102    0x1         0x2         2a:71:c7:f5:1f:1e     *        eth0
192.168.1.105    0x1         0x2         28:e2:97:4d:44:3a     *        eth0
192.168.1.106    0x1         0x0         00:00:00:00:00:00     *        eth0
`), 0o600); err != nil {
		t.Fatalf("write arp table: %v", err)
	}
	now := time.Date(2026, 6, 21, 1, 2, 3, 0, time.UTC)
	events := asicStatusEventsFromEnvAndARP(
		now,
		"192.168.1.101=2a:71:c7:f5:1f:1e,192.168.1.14=28:e2:97:3e:39:63,192.168.1.106=28:e2:97:1e:c0:b5,192.168.1.105=28:e2:97:4d:44:3a",
		"2a:71:c7:f5:1f:1e,28:e2:97:3e:39:63,28:e2:97:1e:c0:b5,28:e2:97:4d:44:3a",
		path,
	)

	byMAC := make(map[string]map[string]any, len(events))
	for _, event := range events {
		byMAC[event["mac"].(string)] = event
		if event["ip"] == "192.168.1.106" {
			t.Fatalf("dead stale override IP emitted: %#v", event)
		}
	}
	for mac, ip := range map[string]string{
		"28:e2:97:3e:39:63": "192.168.1.14",
		"28:e2:97:1e:c0:b5": "192.168.1.101",
		"2a:71:c7:f5:1f:1e": "192.168.1.102",
		"28:e2:97:4d:44:3a": "192.168.1.105",
	} {
		event := byMAC[mac]
		if event == nil {
			t.Fatalf("missing event for %s in %#v", mac, events)
		}
		if event["ip"] != ip {
			t.Fatalf("%s IP = %v, want %s", mac, event["ip"], ip)
		}
		if event["status"] != "connected" {
			t.Fatalf("%s status = %v, want connected", mac, event["status"])
		}
	}
}

func TestASICStatusEventsFallBackToConfiguredOverridesWithoutARP(t *testing.T) {
	now := time.Date(2026, 6, 21, 1, 2, 3, 0, time.UTC)
	events := asicStatusEventsFromEnvAndARP(
		now,
		"192.168.1.105=28:e2:97:4d:44:3a",
		"28:e2:97:4d:44:3a",
		filepath.Join(t.TempDir(), "missing-arp"),
	)
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1: %#v", len(events), events)
	}
	if events[0]["ip"] != "192.168.1.105" || events[0]["status"] != "configured" || events[0]["source"] != "env-overrides" {
		t.Fatalf("fallback event = %#v, want configured env override", events[0])
	}
}

func TestASICStatusEventsDoNotPromoteUnallowlistedARPNeighbors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "arp")
	if err := os.WriteFile(path, []byte(`IP address       HW type     Flags       HW address            Mask     Device
172.18.0.1       0x1         0x2         fa:e7:9a:2d:be:86     *        docker0
192.168.1.105    0x1         0x2         28:e2:97:4d:44:3a     *        eth0
`), 0o600); err != nil {
		t.Fatalf("write arp table: %v", err)
	}
	now := time.Date(2026, 6, 21, 1, 2, 3, 0, time.UTC)
	events := asicStatusEventsFromEnvAndARP(now, "192.168.1.105=28:e2:97:4d:44:3a", "", path)
	if len(events) != 1 {
		t.Fatalf("events len = %d, want only configured override: %#v", len(events), events)
	}
	if events[0]["mac"] != "28:e2:97:4d:44:3a" || events[0]["ip"] != "192.168.1.105" {
		t.Fatalf("event = %#v, want only override MAC/IP", events[0])
	}
}

func TestLatestRetrievableBlockFallsBackToNumericBackscan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		var result any
		switch req.Method {
		case "eth_blockNumber":
			result = "0x66"
		case "eth_getBlockByNumber":
			tag, _ := req.Params[0].(string)
			if tag == "0x64" {
				result = map[string]any{
					"number":     "0x64",
					"hash":       "0xnumeric",
					"parentHash": "0xparent",
					"timestamp":  "0x64",
					"miner":      "0xproducer",
				}
			}
		default:
			t.Fatalf("unexpected rpc method %s", req.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		})
	}))
	defer server.Close()

	worker := &liveWorkers{
		httpClient: server.Client(),
		evmHTTP:    server.URL,
		windowSize: defaultGlobalBlockWindow,
	}
	header, err := worker.latestRetrievableBlock(context.Background())
	if err != nil {
		t.Fatalf("latestRetrievableBlock returned error: %v", err)
	}
	if got := header.height(); got != 100 {
		t.Fatalf("height = %d, want 100", got)
	}
}

func TestLatestStableRetrievableBlockUsesBackscannedTip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode rpc request: %v", err)
		}
		var result any
		switch req.Method {
		case "eth_blockNumber":
			result = "0x66"
		case "eth_getBlockByNumber":
			tag, _ := req.Params[0].(string)
			if tag == "0x64" {
				result = map[string]any{
					"number":     "0x64",
					"hash":       "0xstable",
					"parentHash": "0xparent",
					"timestamp":  "0x64",
					"miner":      "0xproducer",
				}
			}
		default:
			t.Fatalf("unexpected rpc method %s", req.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		})
	}))
	defer server.Close()

	worker := &liveWorkers{
		httpClient:  server.Client(),
		evmHTTP:     server.URL,
		stableDelay: 5 * time.Second,
		windowSize:  defaultGlobalBlockWindow,
	}
	header, err := worker.latestStableRetrievableBlock(context.Background())
	if err != nil {
		t.Fatalf("latestStableRetrievableBlock returned error: %v", err)
	}
	if got := header.height(); got != 100 {
		t.Fatalf("fallback height = %d, want 100", got)
	}
}

func TestWindowStartBoundsWarmupRanges(t *testing.T) {
	tests := []struct {
		name     string
		latest   uint64
		window   int
		expected uint64
	}{
		{name: "genesis range", latest: 50, window: 120, expected: 0},
		{name: "fast warmup range", latest: 1000, window: 120, expected: 881},
		{name: "full window range", latest: 1000, window: 600, expected: 401},
		{name: "invalid window stays bounded", latest: 1000, window: 0, expected: 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := windowStart(tt.latest, tt.window); got != tt.expected {
				t.Fatalf("windowStart(%d, %d) = %d, want %d", tt.latest, tt.window, got, tt.expected)
			}
		})
	}
}

func TestTrendUsesSeparateCurrentAndBackfillCheckpoints(t *testing.T) {
	if trendCurrentStateKey == trendBackfillStateKey {
		t.Fatal("current trend state and historical backfill state must be separate checkpoints")
	}
	if trendCurrentStateKey != "bdag:trend:v2:state" {
		t.Fatalf("current trend state key = %q", trendCurrentStateKey)
	}
	if trendBackfillStateKey != "bdag:trend:v2:backfill_state" {
		t.Fatalf("backfill trend state key = %q", trendBackfillStateKey)
	}
}

func TestApplyPoolMetricDeltasUsesFreshAcceptedBlockWindow(t *testing.T) {
	now := time.Unix(1_800, 0).UTC()
	data := map[string]any{"accepted_block_submissions": "105"}
	applyPoolMetricDeltas(
		map[string]any{
			"updated_at_epoch": float64(1_795),
			"data": map[string]any{
				"accepted_block_submissions": "100",
			},
		},
		data,
		now,
	)
	if got := number(mapString(data, "accepted_block_submissions_delta")); got != 5 {
		t.Fatalf("accepted delta = %v, want 5", got)
	}
	if got := number(mapString(data, "accepted_block_submissions_rate_per_second")); got != 1 {
		t.Fatalf("accepted rate = %v, want 1", got)
	}
	if got := data["accepted_block_recent"]; got != true {
		t.Fatalf("accepted_block_recent = %v, want true", got)
	}
	if got := number(mapString(data, "last_accepted_block_submission_age_seconds")); got != 0 {
		t.Fatalf("last accepted block age = %v, want 0", got)
	}

	carry := map[string]any{"accepted_block_submissions": "105"}
	applyPoolMetricDeltas(
		map[string]any{
			"updated_at_epoch": float64(1_800),
			"data": map[string]any{
				"accepted_block_submissions":                 "105",
				"accepted_block_recent":                      true,
				"last_accepted_block_submission_at_epoch":    float64(1_800),
				"last_accepted_block_submission_age_seconds": float64(0),
			},
		},
		carry,
		time.Unix(1_825, 0).UTC(),
	)
	if got := number(mapString(carry, "accepted_block_submissions_delta")); got != 0 {
		t.Fatalf("carried accepted delta = %v, want 0", got)
	}
	if got := carry["accepted_block_recent"]; got != true {
		t.Fatalf("carried accepted_block_recent = %v, want true", got)
	}
	if got := number(mapString(carry, "last_accepted_block_submission_age_seconds")); got != 25 {
		t.Fatalf("carried accepted block age = %v, want 25", got)
	}

	carryLonger := map[string]any{"accepted_block_submissions": "105"}
	applyPoolMetricDeltas(
		map[string]any{
			"updated_at_epoch": float64(1_825),
			"data": map[string]any{
				"accepted_block_submissions":                 "105",
				"accepted_block_recent":                      true,
				"last_accepted_block_submission_at_epoch":    float64(1_800),
				"last_accepted_block_submission_age_seconds": float64(25),
			},
		},
		carryLonger,
		time.Unix(1_850, 0).UTC(),
	)
	if got := carryLonger["accepted_block_recent"]; got != true {
		t.Fatalf("longer carried accepted_block_recent = %v, want true", got)
	}
	if got := number(mapString(carryLonger, "last_accepted_block_submission_age_seconds")); got != 50 {
		t.Fatalf("longer carried accepted block age = %v, want 50", got)
	}

	expired := map[string]any{"accepted_block_submissions": "105"}
	applyPoolMetricDeltas(
		map[string]any{
			"updated_at_epoch": float64(1_850),
			"data": map[string]any{
				"accepted_block_submissions":                 "105",
				"accepted_block_recent":                      true,
				"last_accepted_block_submission_at_epoch":    float64(1_800),
				"last_accepted_block_submission_age_seconds": float64(50),
			},
		},
		expired,
		time.Unix(1_865, 0).UTC(),
	)
	if got := expired["accepted_block_recent"]; got != false {
		t.Fatalf("expired accepted_block_recent = %v, want false", got)
	}
	if got := number(mapString(expired, "last_accepted_block_submission_age_seconds")); got != 65 {
		t.Fatalf("expired accepted block age = %v, want 65", got)
	}

	stale := map[string]any{"accepted_block_submissions": "110"}
	applyPoolMetricDeltas(
		map[string]any{
			"updated_at_epoch": float64(1_700),
			"data": map[string]any{
				"accepted_block_submissions": "100",
			},
		},
		stale,
		now,
	)
	if got := number(mapString(stale, "accepted_block_submissions_delta")); got != 0 {
		t.Fatalf("stale accepted delta = %v, want 0", got)
	}
	if got := stale["accepted_block_recent"]; got != false {
		t.Fatalf("stale accepted_block_recent = %v, want false", got)
	}

	reset := map[string]any{"accepted_block_submissions": "10"}
	applyPoolMetricDeltas(
		map[string]any{
			"updated_at_epoch": float64(1_795),
			"data": map[string]any{
				"accepted_block_submissions": "100",
			},
		},
		reset,
		now,
	)
	if got := number(mapString(reset, "accepted_block_submissions_delta")); got != 0 {
		t.Fatalf("reset accepted delta = %v, want 0", got)
	}
}

func TestSelectTopPriceMarketsUsesEligibleTopVolumeMarkets(t *testing.T) {
	markets := []cmcMarketPair{
		{ExchangeName: "BitMart", BaseSymbol: "BDAG", Category: "spot", Price: 0.00004125, VolumeUSD: 22304},
		{ExchangeName: "XT.COM", BaseSymbol: "BDAG", Category: "spot", Price: 0.00004115, VolumeUSD: 285110},
		{ExchangeName: "P2B", BaseSymbol: "BDAG", Category: "spot", Price: 0.00004150, VolumeUSD: 778356},
		{ExchangeName: "AscendEX", BaseSymbol: "BDAG", Category: "spot", Price: 0.00004054, VolumeUSD: 126363},
		{ExchangeName: "Excluded", BaseSymbol: "BDAG", Category: "spot", Price: 0.00004994, VolumeUSD: 999999, PriceExcluded: 1},
		{ExchangeName: "Futures", BaseSymbol: "BDAG", Category: "derivatives", Price: 0.000041, VolumeUSD: 888888},
	}
	got := selectTopPriceMarkets(markets, 3)
	if len(got) != 3 {
		t.Fatalf("markets = %d, want 3", len(got))
	}
	names := []string{got[0].ExchangeName, got[1].ExchangeName, got[2].ExchangeName}
	if strings.Join(names, ",") != "P2B,XT.COM,AscendEX" {
		t.Fatalf("selected exchanges = %v", names)
	}
	weighted, arithmetic, totalVolume := averageBDAGPrice(got)
	if totalVolume != 1_189_829 {
		t.Fatalf("total volume = %v, want 1189829", totalVolume)
	}
	if weighted <= 0.0000412 || weighted >= 0.0000414 {
		t.Fatalf("weighted price = %.12f, want around 0.0000413", weighted)
	}
	if arithmetic <= 0.000041 || arithmetic >= 0.0000412 {
		t.Fatalf("arithmetic price = %.12f, want around 0.00004106", arithmetic)
	}
}

func TestTrendRecentCutoffUsesBoundedRecentWindow(t *testing.T) {
	if got := trendRecentCutoffEpoch(10_000, 100); got != 9_800 {
		t.Fatalf("trendRecentCutoffEpoch = %d, want 9800", got)
	}
	if got := trendRecentCutoffEpoch(10_000, 0); got != 10_000-int64(defaultTrendRecentBlocks*2) {
		t.Fatalf("trendRecentCutoffEpoch fallback = %d", got)
	}
}

func TestRequestRecentTrendRebuildSuppressesConcurrentRebuild(t *testing.T) {
	before := time.Now().Add(-10 * time.Minute)
	worker := &liveWorkers{
		lastTrendRebuild:   before,
		trendRebuildActive: true,
	}

	worker.requestRecentTrendRebuild(context.Background(), "unit-test")

	if !worker.lastTrendRebuild.Equal(before) {
		t.Fatalf("active rebuild should not update lastTrendRebuild: got %v want %v", worker.lastTrendRebuild, before)
	}
	if !worker.trendRebuildActive {
		t.Fatal("active rebuild marker should remain set")
	}
}

func TestSparseTrendPlanUsesIndicativeSamples(t *testing.T) {
	worker := &liveWorkers{
		trendBackfillDays: 30,
		trendRecentBlocks: 3600,
	}
	latest := &evmBlockHeader{
		Number:    "0x989680",
		Hash:      "0xlatest",
		Timestamp: "0x6973c480",
		Miner:     "0xproducer",
	}
	plan := worker.sparseTrendPlan(latest)
	if len(plan) == 0 {
		t.Fatal("sparse trend plan was empty")
	}
	seen := map[string]trendSparseSample{}
	for _, sample := range plan {
		seen[sample.Tier] = sample
		if sample.TotalPlanned != len(plan) {
			t.Fatalf("sample %s total planned = %d, want %d", sample.ID, sample.TotalPlanned, len(plan))
		}
	}
	if got := seen["10m"].Window; got != time.Minute {
		t.Fatalf("10m tier sample window = %v, want 1m", got)
	}
	if got := seen["1h"].Window; got != 6*time.Minute {
		t.Fatalf("1h tier sample window = %v, want 6m", got)
	}
	if got := seen["1d"].Window; got != 30*time.Minute {
		t.Fatalf("1d tier sample window = %v, want 30m", got)
	}
	if !intSliceEqual(seen["10m"].Resolutions, []int{30, 60, 300, 900, 3600}) {
		t.Fatalf("10m tier resolutions = %#v", seen["10m"].Resolutions)
	}
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
