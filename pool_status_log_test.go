package main

import "testing"

func TestPoolStatusLogSnapshotProjectsPoolStatusEvent(t *testing.T) {
	logs := `2026-06-26T22:28:42Z stdout {"event":"pool_status","schema":"bdag.pool.status.v1","timestamp":"2026-06-26T22:28:42.288288973Z","pool_status":{"generated_at_unix_ms":1782512922288,"status":"degraded","reason_code":"no_active_miners","active_connections":0,"authorized_connections":0,"subscribed_connections":0,"ready_connections":0,"connections_without_current_job":0,"stale_current_job_connections":0,"invalid_current_job_connections":0,"expired_current_job_connections":0,"current_template_seq":0,"current_parent":"","last_broadcast_age_ms":0,"max_current_job_age_ms":0,"accepted_jobs":{"total":0,"stale":0,"invalidated":0,"submitted":0,"expired":0},"expired_job":{"enabled":true,"window_ms":10000,"cooldown_active":false},"submit_stall":{"enabled":true,"reason_code":"ok","failures":0},"template_conversion_stall":{"enabled":true,"reason_code":"insufficient_active_miners","failure_ratio":0},"router":{"enabled":true,"node_name":"node","node":{"name":"node","selected":true,"healthy":false,"reason":"node-health-http-error","score":0,"ws_connected":false}}}}`

	view := newPoolStatusLogSnapshotFromLogs(logs).TabView()

	if view.State != "Degraded" || view.StateTone != "warn" {
		t.Fatalf("pool state = %s/%s, want Degraded/warn", view.State, view.StateTone)
	}
	if got := evidenceValue(view.Evidence, "Reason"); got != "no_active_miners" {
		t.Fatalf("Reason = %q, want no_active_miners", got)
	}
	if got := evidenceValue(view.Evidence, "Active"); got != "0" {
		t.Fatalf("Active = %q, want 0", got)
	}
	if len(view.Groups) != 5 {
		t.Fatalf("groups len = %d, want 5", len(view.Groups))
	}
	if view.Groups[0].Title != "Health" {
		t.Fatalf("first group = %q, want Health", view.Groups[0].Title)
	}
	if got := evidenceValue(view.Groups[1].Items, "Total"); got != "0" {
		t.Fatalf("accepted total = %q, want 0", got)
	}
	for _, group := range view.Groups {
		if group.Title == "Router" || group.Title == "Router Node" {
			t.Fatalf("router group should not be projected: %#v", group)
		}
	}
}

func TestPoolStatusLogSnapshotDoesNotRenderMissingDurationsAsZero(t *testing.T) {
	logs := `{"event":"pool_status","schema":"bdag.pool.status.v1","timestamp":"2026-06-26T22:28:42Z","pool_status":{"status":"degraded"}}`

	view := newPoolStatusLogSnapshotFromLogs(logs).TabView()

	if got := evidenceValue(view.Evidence, "Last Broadcast"); got != "unknown" {
		t.Fatalf("missing Last Broadcast = %q, want unknown", got)
	}
	if got := evidenceValue(view.Groups[2].Items, "Window"); got != "unknown" {
		t.Fatalf("missing recovery Window = %q, want unknown", got)
	}
}
