package main

import "testing"

func TestMinerFleetSnapshotProjectsExistingRows(t *testing.T) {
	data := map[string]any{
		"miner_health": map[string]any{
			"managed_miners": []any{
				map[string]any{
					"managed":             true,
					"device_type":         "asic",
					"mac":                 "28-E2-97-2E-00-1B",
					"ip":                  "192.168.100.80",
					"status":              "connected",
					"valid_shares":        "42",
					"last_seen_at":        "2026-06-26T10:00:00Z",
					"configured_pool_url": "stratum+tcp://pool.local:3334",
					"device_model":        "X100",
					"active_pool_user":    "bdag1example000000000000000000000000000000000",
					"device_accepted":     "7",
				},
			},
		},
		"asic_performance": map[string]any{
			"lanes": []any{
				map[string]any{
					"asic_mac":              "28:e2:97:2e:00:1b",
					"remote_host":           "192.168.100.80",
					"ready":                 true,
					"health":                "connected",
					"current_job_age_ms":    "1500",
					"blocks_30m":            "2",
					"blocks_1h":             "3",
					"blocks_24h":            "9",
					"rejected_30m":          "1",
					"rejected_local_30m":    "2",
					"protocol_variant":      "stratum-v1-basic",
					"last_accepted_unix_ms": "1782477600000",
				},
			},
		},
	}

	snapshot := newMinerFleetSnapshot(data)

	statusRows := snapshot.StatusRows()
	if len(statusRows) != 1 {
		t.Fatalf("status rows len = %d, want 1", len(statusRows))
	}
	if got := statusRows[0]["mac"]; got != "28:e2:97:2e:00:1b" {
		t.Fatalf("status mac = %q", got)
	}
	if got := statusRows[0]["intent"]; got != "stratum+tcp://pool.local:3334" {
		t.Fatalf("status intent = %q", got)
	}

	controlRows := snapshot.ControlRows()
	if len(controlRows) != 1 {
		t.Fatalf("control rows len = %d, want 1", len(controlRows))
	}
	if got := controlRows[0]["target"]; got != "192.168.100.80|28:e2:97:2e:00:1b" {
		t.Fatalf("control target = %q", got)
	}

	performanceRows := snapshot.PerformanceRows()
	if len(performanceRows) != 1 {
		t.Fatalf("performance rows len = %d, want 1", len(performanceRows))
	}
	if got := performanceRows[0]["rejects_30m"]; got != "3" {
		t.Fatalf("performance rejects_30m = %q", got)
	}

	deviceRows := snapshot.DeviceRows()
	if len(deviceRows) != 1 {
		t.Fatalf("device rows len = %d, want 1", len(deviceRows))
	}
	if got := deviceRows[0]["model"]; got != "X100" {
		t.Fatalf("device model = %q", got)
	}
}
