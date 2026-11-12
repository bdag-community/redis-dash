package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPoolStatusShowsDownWhenPoolContainerStoppedDuringNodeSync(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "sync_only_no_miners",
			"can_accept_shares": false,
			"can_submit_blocks": false,
			"blocking_failures": []any{"pool is not running"},
			"containers": map[string]any{
				"pool": map[string]any{
					"running": false,
					"status":  "missing",
				},
			},
			"pool_health": map[string]any{
				"stale_jobs": false,
			},
			"pool_metrics": map[string]any{
				"active_connections": 2,
			},
			"sync_progress": map[string]any{
				"status": "unable_to_sync",
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())
	if view.State != "Down" {
		t.Fatalf("pool status state = %q, want Down", view.State)
	}
	if view.StateTone != "down" {
		t.Fatalf("pool status tone = %q, want down", view.StateTone)
	}
	if value := evidenceValue(view.Evidence, "Miner Activity"); value != "no" {
		t.Fatalf("Miner Activity = %q, want no when pool is stopped", value)
	}
}

func TestPoolStatusShowsMiningWhenAcceptingSharesAndWinningBlocks(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "mining",
			"can_accept_shares": true,
			"can_submit_blocks": true,
			"containers": map[string]any{
				"pool": map[string]any{
					"running": true,
					"status":  "running",
				},
			},
			"pool_health": map[string]any{
				"block_submit_success_count": 2,
				"stale_jobs":                 false,
				"valid_share_count":          42,
			},
			"sync_progress": map[string]any{
				"status": "synced",
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())
	if view.State != "Mining" {
		t.Fatalf("pool status state = %q, want Mining", view.State)
	}
	if view.StateTone != "ok" {
		t.Fatalf("pool status tone = %q, want ok", view.StateTone)
	}
}

func TestPoolStatusWarnsWhenExpectedASICFleetIsPartiallyReady(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "mining",
			"can_accept_shares": true,
			"can_submit_blocks": true,
			"containers": map[string]any{
				"pool": map[string]any{"running": true, "status": "running"},
			},
			"pool_health": map[string]any{"stale_jobs": false, "valid_share_count": 42},
			"miner_health": map[string]any{
				"managed_count": 2,
			},
			"template_safety": map[string]any{
				"ready_miner_job_count": 1,
			},
			"warnings": []any{"upstream warning"},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())
	if view.State != "Mining Degraded" {
		t.Fatalf("pool status state = %q, want Mining Degraded", view.State)
	}
	if !containsString(view.Warnings, "upstream warning") {
		t.Fatalf("warnings did not preserve upstream warning: %#v", view.Warnings)
	}
	if !containsString(view.Warnings, "ASIC fleet degraded: 1 of 2 expected ASIC lanes have ready jobs.") {
		t.Fatalf("warnings missing ASIC fleet degraded warning: %#v", view.Warnings)
	}
}

func TestWatchdogStatusWarningsFromHardwarePowerCycleStateFile(t *testing.T) {
	path := t.TempDir() + "/watchdog-state.json"
	if err := os.WriteFile(
		path,
		[]byte(`{
  "last_status": "pool_sync_template_pause",
  "last_share_warnings": ["staged recovery reached hardware-power-cycle-required"],
  "last_asic_hardware_power_cycle_required": [
    {"mac": "28:e2:97:2e:00:1b", "ip": "192.168.100.83"}
  ]
}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BDAG_WATCHDOG_STATE_FILE", path)

	warnings := watchdogStatusWarnings()

	if len(warnings) != 1 {
		t.Fatalf("warnings len = %d, want 1: %#v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "28:e2:97:2e:00:1b") ||
		!strings.Contains(warnings[0], "hardware-power-cycle-required") {
		t.Fatalf("warning did not describe hardware power-cycle state: %#v", warnings)
	}
}

func TestMinerHealthASICStallWarningsIgnoreReadyMiner(t *testing.T) {
	warnings := minerHealthASICStallWarnings(map[string]any{
		"miners": []any{
			map[string]any{
				"managed":                 true,
				"device_type":             "asic",
				"mac":                     "28:e2:97:3d:95:13",
				"ip":                      "192.168.100.76",
				"status":                  "degraded",
				"ready":                   true,
				"device_telemetry_errors": "cgminer_devs: HTTP 500",
			},
			map[string]any{
				"managed":                 true,
				"device_type":             "asic",
				"mac":                     "28:e2:97:2e:00:1b",
				"ip":                      "192.168.100.83",
				"status":                  "no-stratum",
				"device_telemetry_errors": "Get /mcb/pools: EOF; no-request-eof",
			},
		},
	})

	if len(warnings) != 1 {
		t.Fatalf("warnings len = %d, want 1: %#v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "00:1b") {
		t.Fatalf("warning targeted wrong miner: %#v", warnings)
	}
}

func TestPoolStatusShowsMiningWhenCollectorModeMiningDuringSyncWarning(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "mining",
			"overall":           "syncing",
			"can_accept_shares": true,
			"can_submit_blocks": false,
			"containers": map[string]any{
				"pool": map[string]any{
					"running": true,
					"status":  "running",
				},
			},
			"pool_health": map[string]any{
				"last_block_submit_age_seconds": 5,
				"stale_jobs":                    false,
				"valid_share_count":             42,
			},
			"sync_progress": map[string]any{
				"status": "syncing",
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())
	if view.State != "Mining" {
		t.Fatalf("pool status state = %q, want Mining", view.State)
	}
	if view.StateTone != "warn" {
		t.Fatalf("pool status tone = %q, want warn while submit readiness is false", view.StateTone)
	}
}

func TestPoolStatusShowsTemplateUnsafeSeparateFromSyncedNode(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "mining",
			"overall":           "syncing",
			"can_accept_shares": true,
			"can_submit_blocks": false,
			"containers": map[string]any{
				"pool": map[string]any{
					"running": true,
					"status":  "running",
				},
				"node": map[string]any{
					"running": true,
				},
			},
			"sync_progress": map[string]any{
				"status": "synced",
			},
			"template_safety": map[string]any{
				"template_coinbase_valid":                     false,
				"zero_coinbase_reject_rate_per_second":        0.2,
				"ready_miner_job_count":                       4,
				"local_stale_ratio":                           0.333333,
				"zero_coinbase_reject_total":                  12,
				"zero_coinbase_reject_sample_elapsed_seconds": 60,
				"automation_restart": map[string]any{
					"recent": true,
					"name":   "restart-node",
					"status": "suppressed",
				},
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())
	if view.Node.State != "" {
		t.Fatalf("pool status should not attach synthetic node state, got %q", view.Node.State)
	}
	if view.State != "Mining Degraded" {
		t.Fatalf("pool status state = %q, want Mining Degraded", view.State)
	}
	if view.StateTone != "warn" {
		t.Fatalf("pool status tone = %q, want warn", view.StateTone)
	}
	if !statusStepActive(view.StatusSteps, "Template Unsafe") {
		t.Fatalf("Template Unsafe step not active: %#v", view.StatusSteps)
	}
	for label, want := range map[string]string{
		"Template Coinbase Valid":   "no",
		"Zero Coinbase Reject Rate": "12.00/min",
		"Ready Miner Jobs":          "4",
		"Local Stale Ratio":         "33.33%",
		"Last Automation Restart":   "restart-node suppressed",
	} {
		if value := evidenceValue(view.Secondary, label); value != want {
			t.Fatalf("%s = %q, want %q", label, value, want)
		}
	}
}

func TestPoolStatusExposesMachineReadableTemplateHealth(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "mining",
			"overall":           "degraded",
			"can_accept_shares": true,
			"can_submit_blocks": false,
			"can_mine":          false,
			"sync_progress": map[string]any{
				"status":            "synced",
				"native_is_current": true,
				"p2p_network_gap":   0,
			},
			"miner_health": map[string]any{
				"connected_count": 4,
				"managed_count":   4,
			},
			"pool_health": map[string]any{
				"source_job_health": map[string]any{
					"authorized_miners": "4",
					"ready_miners":      "4",
				},
			},
			"template_health": map[string]any{
				"reason_code":             "zero-template-coinbase",
				"mineable_now":            false,
				"submit_ready":            false,
				"template_coinbase_valid": false,
			},
			"template_safety": map[string]any{
				"template_coinbase_valid": false,
				"ready_miner_job_count":   "4",
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())

	if view.Mode != "mining" || view.Overall != "degraded" {
		t.Fatalf("view mode/overall = %q/%q, want mining/degraded", view.Mode, view.Overall)
	}
	if view.CanMine || view.CanSubmit != false || !view.CanAccept {
		t.Fatalf("view mining flags = can_mine:%v can_submit:%v can_accept:%v", view.CanMine, view.CanSubmit, view.CanAccept)
	}
	if got := mapString(view.Template, "reason_code"); got != "zero-template-coinbase" {
		t.Fatalf("template reason = %q, want zero-template-coinbase", got)
	}
	if got, known := boolKnown(view.Template["template_coinbase_valid"]); !known || got {
		t.Fatalf("template coinbase = %v known:%v, want false known", got, known)
	}
	if got := mapString(view.SyncProgress, "status"); got != "synced" {
		t.Fatalf("sync status = %q, want synced", got)
	}
	if got := nestedString(view.PoolHealth, "source_job_health", "ready_miners"); got != "4" {
		t.Fatalf("pool ready miners = %q, want 4", got)
	}
}

func TestPoolStatusDoesNotHideTemplateHealthDegradedBehindMiningOK(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "mining",
			"overall":           "ok",
			"can_accept_shares": true,
			"can_submit_blocks": true,
			"can_mine":          true,
			"sync_progress": map[string]any{
				"status":            "synced",
				"native_is_current": true,
			},
			"pool_health": map[string]any{
				"stale_jobs": false,
				"source_job_health": map[string]any{
					"authorized_miners": "4",
					"ready_miners":      "4",
				},
			},
			"template_health": map[string]any{
				"reason_code":             "submit-not-ready",
				"mineable_now":            false,
				"submit_ready":            false,
				"template_coinbase_valid": true,
			},
			"template_safety": map[string]any{
				"template_coinbase_valid": true,
				"ready_miner_job_count":   "4",
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())

	if view.State != "Mining Degraded" || view.StateTone != "warn" {
		t.Fatalf("state = %q/%q, want Mining Degraded/warn", view.State, view.StateTone)
	}
	if view.Overall != "degraded" {
		t.Fatalf("overall = %q, want degraded", view.Overall)
	}
	if view.CanMine {
		t.Fatal("can_mine = true while template health is degraded")
	}
	if !view.CanSubmit {
		t.Fatal("can_submit_blocks should preserve raw pool policy signal")
	}
	if !statusStepActive(view.StatusSteps, "Template Unsafe") {
		t.Fatalf("Template Unsafe step not active: %#v", view.StatusSteps)
	}
}

func TestPoolStatusUsesCollectorReadyNoMinersOverPoolSessionMetrics(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "ready_no_miners",
			"can_accept_shares": false,
			"can_submit_blocks": false,
			"containers": map[string]any{
				"pool": map[string]any{
					"running": true,
					"status":  "running",
				},
			},
			"pool_metrics": map[string]any{
				"active_connections": 1,
				"source_job_health": map[string]any{
					"authorized_miners": 1,
					"ready_miners":      1,
				},
			},
			"pool_health": map[string]any{
				"source_job_health": map[string]any{
					"authorized_miners": 1,
					"ready_miners":      1,
				},
				"stale_jobs":        false,
				"valid_share_count": 42,
			},
			"sync_progress": map[string]any{
				"status": "synced",
			},
			"miner_health": map[string]any{
				"connected_count": 0,
				"managed_count":   0,
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())
	if view.State != "Waiting for a Miner" {
		t.Fatalf("pool status state = %q, want Waiting for a Miner", view.State)
	}
	if view.StateTone != "warn" {
		t.Fatalf("pool status tone = %q, want warn", view.StateTone)
	}
}

func TestPoolStatusShowsMiningDegradedWhenExpectedASICFleetIsNotReady(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "mining",
			"overall":           "ok",
			"can_accept_shares": true,
			"can_submit_blocks": true,
			"containers": map[string]any{
				"pool": map[string]any{"running": true},
			},
			"pool_metrics": map[string]any{
				"source_job_health": map[string]any{
					"authorized_miners": 1,
					"ready_miners":      1,
				},
			},
			"pool_health": map[string]any{
				"stale_jobs": false,
			},
			"miner_health": map[string]any{
				"configured_count": 4,
				"managed_count":    4,
			},
			"sync_progress": map[string]any{"status": "synced"},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())

	if view.State != "Mining Degraded" {
		t.Fatalf("pool status state = %q, want Mining Degraded", view.State)
	}
	if view.StateTone != "warn" {
		t.Fatalf("pool status tone = %q, want warn", view.StateTone)
	}
	if !statusStepActive(view.StatusSteps, "Degraded") {
		t.Fatalf("Degraded step not active: %#v", view.StatusSteps)
	}
	if value := evidenceValue(view.Secondary, "Ready Miner Jobs"); value != "1" {
		t.Fatalf("Ready Miner Jobs = %q, want 1", value)
	}
}

func TestPoolStatusUsesCollectorCatchupModeOverRawReadySignals(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "catchup_pause",
			"overall":           "degraded",
			"can_accept_shares": true,
			"can_submit_blocks": true,
			"containers": map[string]any{
				"pool": map[string]any{
					"running": true,
					"status":  "running",
				},
			},
			"sync_progress": map[string]any{
				"status": "synced",
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())
	if view.State != "Waiting for Node Sync" {
		t.Fatalf("pool status state = %q, want Waiting for Node Sync", view.State)
	}
	if view.StateTone != "warn" {
		t.Fatalf("pool status tone = %q, want warn", view.StateTone)
	}
}

func TestPoolStatusShowsCollectorWarmup(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":                         "waiting_for_status_sample",
			"overall":                      "unknown",
			"collector_status_unavailable": true,
			"collector_api_cache": map[string]any{
				"hit":              false,
				"refresh_inflight": true,
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())
	if view.State != "Waiting for Collector" {
		t.Fatalf("pool status state = %q, want Waiting for Collector", view.State)
	}
	if view.StateTone != "warn" {
		t.Fatalf("pool status tone = %q, want warn", view.StateTone)
	}
	if len(view.Warnings) == 0 {
		t.Fatalf("pool status warnings absent, want collector warmup warning")
	}
}

func TestPoolStatusDoesNotDisplayPoolReadySessionsAsReadyMinerCount(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"mode":              "ready_no_miners",
			"can_accept_shares": false,
			"can_submit_blocks": false,
			"containers": map[string]any{
				"pool": map[string]any{
					"running": true,
					"status":  "running",
				},
			},
			"pool_metrics": map[string]any{
				"active_connections": 2,
				"source_job_health": map[string]any{
					"authorized_miners": 2,
					"ready_miners":      2,
				},
			},
			"pool_health": map[string]any{
				"source_job_health": map[string]any{
					"authorized_miners": 2,
					"ready_miners":      2,
				},
				"stale_jobs":        false,
				"valid_share_count": 7,
			},
			"sync_progress": map[string]any{
				"status": "synced",
			},
			"miner_health": map[string]any{
				"connected_count": 0,
				"managed_count":   0,
				"tracked_count":   0,
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolStatus(context.Background())
	if value := evidenceValue(view.Secondary, "Ready Miners"); value != "" {
		t.Fatalf("Ready Miners chip = %q, want absent", value)
	}
	if value := evidenceValue(view.Secondary, "Miner Inventory"); value != "activity only" {
		t.Fatalf("Miner Inventory = %q, want activity only", value)
	}
	if value := evidenceValue(view.Evidence, "Miner Activity"); value != "yes" {
		t.Fatalf("Miner Activity = %q, want yes", value)
	}
}

func TestStratumRuntimeShowsAwaitingBroadcastSeparatelyFromStaleJobs(t *testing.T) {
	view := stratumRuntimeView(map[string]any{
		"status":                           "degraded",
		"reason_code":                      "awaiting_current_job_broadcast",
		"active_connections":               4,
		"authorized_connections":           4,
		"subscribed_connections":           4,
		"ready_connections":                0,
		"connections_without_current_job":  0,
		"awaiting_current_job_connections": 4,
		"stale_current_job_connections":    0,
		"invalid_current_job_connections":  0,
		"expired_current_job_connections":  0,
		"current_template_seq":             88,
		"last_broadcast_age_ms":            750,
		"max_current_job_age_ms":           1250,
	}, nil)

	if view.State != "Degraded" || view.StateTone != "warn" {
		t.Fatalf("stratum runtime state = %s/%s, want Degraded/warn", view.State, view.StateTone)
	}
	if value := evidenceValue(view.Evidence, "Awaiting Broadcast"); value != "4" {
		t.Fatalf("Awaiting Broadcast = %q, want 4", value)
	}
	if value := evidenceValue(view.Evidence, "Stale Jobs"); value != "0" {
		t.Fatalf("Stale Jobs = %q, want 0", value)
	}
	if value := evidenceValue(view.Evidence, "Reason"); value != "awaiting_current_job_broadcast" {
		t.Fatalf("Reason = %q, want awaiting_current_job_broadcast", value)
	}
}

func TestMinerRowsIncludeMACAddress(t *testing.T) {
	rows := minerRows(map[string]any{
		"miner_health": map[string]any{
			"miners": []any{
				map[string]any{
					"device_id":     "mac:28:e2:97:4c:e4:0a",
					"ip":            "192.168.100.107",
					"status":        "connected",
					"valid_shares":  9,
					"last_share_at": "2026-06-16T14:00:00Z",
				},
			},
		},
	})

	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if rows[0]["mac"] != "28:e2:97:4c:e4:0a" {
		t.Fatalf("mac = %q, want physical MAC", rows[0]["mac"])
	}
}

func TestMinerRowsPreferManagedX100Fleet(t *testing.T) {
	rows := minerRows(map[string]any{
		"miner_health": map[string]any{
			"miners": []any{
				map[string]any{
					"ip":          "192.168.1.1",
					"device_type": "asic",
					"status":      "inactive",
					"managed":     false,
				},
				map[string]any{
					"ip":          "192.168.1.100",
					"device_type": "asic",
					"model":       "X100",
					"status":      "ok",
					"managed":     true,
				},
				map[string]any{
					"ip":          "192.168.1.101",
					"device_type": "asic",
					"status":      "ok",
					"managed":     true,
				},
				map[string]any{
					"ip":          "192.168.1.104",
					"device_type": "asic",
					"status":      "inactive",
					"managed":     false,
				},
			},
		},
	})

	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}
	if rows[0]["ip"] != "192.168.1.100" || rows[1]["ip"] != "192.168.1.101" {
		t.Fatalf("rows = %#v, want only managed X100 miners", rows)
	}
}

func TestMinerRowsIncludeConfiguredASICWithDegradedTelemetry(t *testing.T) {
	rows := minerRows(map[string]any{
		"miner_health": map[string]any{
			"managed_miners": []any{
				map[string]any{
					"managed":     true,
					"device_type": "asic",
					"mac":         "28:e2:97:3d:95:13",
					"ip":          "192.168.100.80",
					"status":      "connected",
					"connected":   true,
					"source":      "host-arp",
				},
				map[string]any{
					"managed":     true,
					"device_type": "asic",
					"mac":         "28:e2:97:2e:00:1b",
					"ip":          "192.168.100.83",
					"status":      "connected",
					"connected":   true,
					"source":      "host-arp",
				},
			},
		},
		"asic_telemetry": map[string]any{
			"devices": []any{
				map[string]any{
					"mac":                 "28:e2:97:3d:95:13",
					"ip":                  "192.168.100.80",
					"status":              "degraded",
					"age_seconds":         4,
					"stale_after_seconds": 90,
					"errors": map[string]any{
						"status":       "EOF",
						"cgminer_devs": "connection reset by peer",
					},
					"summary": map[string]any{
						"model":           "unknown",
						"hashrate":        "unknown",
						"active_pool_url": "unknown",
					},
				},
			},
		},
	})

	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want both configured ASICs: %#v", len(rows), rows)
	}
	var stalled map[string]string
	for _, row := range rows {
		if row["mac"] == "28:e2:97:3d:95:13" {
			stalled = row
		}
	}
	if stalled == nil {
		t.Fatalf("configured stalled ASIC missing from rows: %#v", rows)
	}
	if stalled["status"] != "api degraded" || !strings.Contains(stalled["diagnostic"], "Not mining") {
		t.Fatalf("stalled ASIC status/diagnostic = %#v", stalled)
	}
	if !strings.Contains(stalled["telemetry_errors"], "connection reset by peer") {
		t.Fatalf("stalled ASIC telemetry errors missing detail: %#v", stalled)
	}
}

func TestASICPerformanceRowsAndChartUsePoolLaneBuckets(t *testing.T) {
	data := map[string]any{
		"asic_performance": map[string]any{
			"lanes": []any{
				map[string]any{
					"lane_id":               "mac:2A:71:C7:F5:1F:1E",
					"asic_mac":              "2A:71:C7:F5:1F:1E",
					"remote_host":           "192.168.1.101",
					"ready":                 true,
					"health":                "ok",
					"protocol_variant":      "stratum-v1-extranonce",
					"current_job_age_ms":    float64(1500),
					"blocks_30m":            float64(4),
					"blocks_1h":             float64(8),
					"blocks_24h":            float64(96),
					"rejected_30m":          float64(1),
					"rejected_local_30m":    float64(2),
					"last_accepted_unix_ms": float64(1782043200000),
					"buckets": []any{
						map[string]any{
							"start_unix_ms": float64(1782042600000),
							"counts": map[string]any{
								"accepted":       float64(2),
								"rejected":       float64(1),
								"rejected_local": float64(0),
							},
						},
					},
					"hashrate_points": []any{
						map[string]any{
							"t":            "2026-06-23T15:00:00Z",
							"hashrate":     float64(244.93),
							"avg_hashrate": float64(230.50),
							"accepted":     float64(49),
							"rejected":     float64(1),
						},
					},
				},
			},
		},
	}

	rows := asicPerformanceRows(data)
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	row := rows[0]
	if row["mac"] != "2a:71:c7:f5:1f:1e" ||
		row["ready"] != "yes" ||
		row["blocks_30m"] != "4" ||
		row["blocks_1h"] != "8" ||
		row["blocks_24h"] != "96" ||
		row["rejects_30m"] != "3" ||
		row["protocol"] != "stratum-v1-extranonce" {
		t.Fatalf("ASIC row = %#v", row)
	}

	raw := asicPerformanceChartJSON(data)
	if !strings.Contains(raw, `"accepted":2`) || !strings.Contains(raw, `"label":"2a:71:c7:f5:1f:1e"`) {
		t.Fatalf("chart json missing ASIC bucket data: %s", raw)
	}
	if !strings.Contains(raw, `"hashrate":244.93`) || !strings.Contains(raw, `"avg_hashrate":230.5`) {
		t.Fatalf("chart json missing ASIC hashrate history: %s", raw)
	}
}

func TestRecentAcceptedBlockSignalUsesRecentDeltaNotLifetimeTotal(t *testing.T) {
	lifetimeOnly := map[string]any{
		"pool_health": map[string]any{
			"accepted_block_count":       "1200",
			"block_submit_success_count": "1200",
		},
		"pool_metrics": map[string]any{
			"accepted_block_submissions": "1200",
		},
	}
	if recentAcceptedBlockSignal(lifetimeOnly) {
		t.Fatal("recentAcceptedBlockSignal() = true for lifetime-only accepted count")
	}
	recentDelta := map[string]any{
		"pool_metrics": map[string]any{
			"accepted_block_submissions_delta": "2",
		},
	}
	if !recentAcceptedBlockSignal(recentDelta) {
		t.Fatal("recentAcceptedBlockSignal() = false for fresh accepted delta")
	}
	recentFlag := map[string]any{
		"pool_metrics": map[string]any{
			"accepted_block_recent": true,
		},
	}
	if !recentAcceptedBlockSignal(recentFlag) {
		t.Fatal("recentAcceptedBlockSignal() = false for accepted_block_recent")
	}
}

func TestASICDeviceRowsExposeReadOnlyClockVoltageAndPool(t *testing.T) {
	rows := asicDeviceRows(map[string]any{
		"miner_health": map[string]any{
			"managed_miners": []any{
				map[string]any{
					"managed":                      true,
					"device_type":                  "asic",
					"mac":                          "2a:71:c7:f5:1f:1e",
					"ip":                           "192.168.1.102",
					"device_model":                 "X100",
					"firmware":                     "1.2.3",
					"clock_mhz":                    "850 MHz",
					"voltage_v":                    "0.76 V",
					"chip_count":                   "6",
					"active_pool_url":              "stratum+tcp://192.168.1.100:3334",
					"pool_pass_present":            true,
					"device_telemetry_age_seconds": "2",
					"device_telemetry_status":      "ok",
				},
			},
		},
	})
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	row := rows[0]
	if row["clock"] != "850 MHz" || row["voltage"] != "0.76 V" || row["chips"] != "6" || row["pool_pass"] != "yes" {
		t.Fatalf("device row = %#v", row)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestMinerInventoryStatusPrefersManagedCount(t *testing.T) {
	status := minerInventoryStatus(map[string]any{
		"miner_health": map[string]any{
			"managed_count":   4,
			"tracked_count":   9,
			"connected_count": 4,
		},
	})

	if status != "4" {
		t.Fatalf("Miner Inventory = %q, want managed miner count", status)
	}
}

func evidenceValue(rows []Evidence, label string) string {
	for _, row := range rows {
		if row.Label == label {
			return row.Value
		}
	}
	return ""
}

func statusStepActive(steps []StatusStep, label string) bool {
	for _, step := range steps {
		if step.Label == label {
			return step.Active
		}
	}
	return false
}
