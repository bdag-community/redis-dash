package main

import "testing"

func TestStatusReadinessSnapshotProjectsLiveReadiness(t *testing.T) {
	data := map[string]any{
		"source":            "redis-live",
		"mode":              "mining",
		"overall":           "ok",
		"can_accept_shares": true,
		"can_submit_blocks": true,
		"can_mine":          true,
		"pool_health": map[string]any{
			"valid_share_count":            "12",
			"block_submit_error_count":     "0",
			"last_valid_share_age_seconds": "3",
		},
		"pool_metrics": map[string]any{
			"accepted_block_recent": true,
		},
		"template_health": map[string]any{
			"submit_ready": false,
		},
		"warnings":      []any{"pool warning"},
		"status_reason": "template submit path is degraded",
	}

	snapshot := newStatusReadinessSnapshot(data)

	if snapshot.Mode != "mining" {
		t.Fatalf("Mode = %q, want mining", snapshot.Mode)
	}
	if snapshot.Overall != "degraded" {
		t.Fatalf("Overall = %q, want degraded", snapshot.Overall)
	}
	if snapshot.CanMine {
		t.Fatalf("CanMine = true, want false when template health is degraded")
	}
	if snapshot.State != "Mining Degraded" {
		t.Fatalf("State = %q, want Mining Degraded", snapshot.State)
	}
	if !statusStepActive(snapshot.Steps, "Template Unsafe") {
		t.Fatalf("Template Unsafe step is not active: %#v", snapshot.Steps)
	}
	if got := evidenceValue(snapshot.Evidence, "Live Mode"); got != "mining" {
		t.Fatalf("Live Mode evidence = %q, want mining", got)
	}
	if got := evidenceValue(snapshot.Evidence, "Recent Accepted Block"); got != "yes" {
		t.Fatalf("Recent Accepted Block evidence = %q, want yes", got)
	}
	if len(snapshot.Warnings) != 1 || snapshot.Warnings[0] != "pool warning" {
		t.Fatalf("Warnings = %#v, want pool warning", snapshot.Warnings)
	}
	if len(snapshot.Notes) != 1 || snapshot.Notes[0] != "template submit path is degraded" {
		t.Fatalf("Notes = %#v, want status reason", snapshot.Notes)
	}
}
