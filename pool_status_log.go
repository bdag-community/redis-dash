package main

import (
	"context"
	"strings"
)

const poolStatusSchema = "bdag.pool.status.v1"

type PoolStatusLogSnapshot struct {
	Found      bool
	Event      string
	Schema     string
	Timestamp  string
	PoolStatus map[string]any
}

type poolStatusField struct {
	Key    string
	Label  string
	Format string
}

func (a *App) poolLogStatus(ctx context.Context) TabView {
	logs := a.logContent(ctx, "pool", 240)
	return newPoolStatusLogSnapshotFromLogs(logs).TabView()
}

func newPoolStatusLogSnapshotFromLogs(content string) PoolStatusLogSnapshot {
	var latest PoolStatusLogSnapshot
	for _, raw := range jsonObjectsInText(content) {
		payload := map[string]any{}
		if err := decodeJSONObject(raw, &payload); err != nil {
			continue
		}
		if lower(mapString(payload, "event")) != "pool_status" && mapString(payload, "schema") != poolStatusSchema {
			continue
		}
		status, _ := payload["pool_status"].(map[string]any)
		if status == nil {
			status = map[string]any{}
		}
		latest = PoolStatusLogSnapshot{
			Found:      true,
			Event:      mapString(payload, "event"),
			Schema:     mapString(payload, "schema"),
			Timestamp:  mapString(payload, "timestamp"),
			PoolStatus: status,
		}
	}
	return latest
}

func (s PoolStatusLogSnapshot) TabView() TabView {
	if !s.Found {
		return TabView{
			Title:        "Pool Status",
			State:        "Waiting for pool_status",
			StateTone:    "warn",
			RefreshEvery: "3s",
			Evidence: []Evidence{
				{"State", "waiting for pool_status"},
				{"Source", "pool logs"},
				{"Schema", poolStatusSchema},
			},
			Groups: []EvidenceGroup{
				{Title: "Health", Items: []Evidence{{"Status", "waiting"}}},
			},
		}
	}
	state := first(titleStatus(mapString(s.PoolStatus, "status")), "Unknown")
	return TabView{
		Title:        "Pool Status",
		State:        state,
		StateTone:    poolStatusTone(mapString(s.PoolStatus, "status")),
		RefreshEvery: "3s",
		Evidence:     s.summaryEvidence(state),
		Groups:       s.groups(state),
	}
}

func (s PoolStatusLogSnapshot) summaryEvidence(state string) []Evidence {
	rows := poolStatusEvidence(s.PoolStatus, []poolStatusField{
		{"status", "Status", "status"},
		{"reason_code", "Reason", ""},
		{"active_connections", "Active", "int"},
		{"authorized_connections", "Authorized", "int"},
		{"subscribed_connections", "Subscribed", "int"},
		{"ready_connections", "Ready", "int"},
		{"connections_without_current_job", "Without Job", "int"},
		{"stale_current_job_connections", "Stale Jobs", "int"},
		{"invalid_current_job_connections", "Invalid Jobs", "int"},
		{"expired_current_job_connections", "Expired Jobs", "int"},
		{"current_template_seq", "Template Seq", "int"},
		{"current_parent", "Current Parent", ""},
		{"last_broadcast_age_ms", "Last Broadcast", "ms"},
		{"max_current_job_age_ms", "Max Job Age", "ms"},
		{"generated_at_unix_ms", "Generated At", "unixms"},
	})
	rows = append([]Evidence{{"State", state}}, rows...)
	rows = append(rows,
		Evidence{"Event Time", humanTime(s.Timestamp)},
		Evidence{"Schema", first(s.Schema, poolStatusSchema)},
	)
	return rows
}

func (s PoolStatusLogSnapshot) groups(state string) []EvidenceGroup {
	status := s.PoolStatus
	return []EvidenceGroup{
		{
			Title: "Health",
			Items: s.summaryEvidence(state),
		},
		{
			Title: "Accepted Jobs",
			Items: poolStatusEvidence(mapValue(status, "accepted_jobs"), []poolStatusField{
				{"total", "Total", "int"},
				{"stale", "Stale", "int"},
				{"invalidated", "Invalidated", "int"},
				{"submitted", "Submitted", "int"},
				{"expired", "Expired", "int"},
			}),
		},
		{
			Title: "Expired Job Recovery",
			Items: poolStatusEvidence(mapValue(status, "expired_job"), []poolStatusField{
				{"enabled", "Enabled", "bool"},
				{"window_ms", "Window", "ms"},
				{"window_age_ms", "Window Age", "ms"},
				{"window_rejects", "Window Rejects", "int"},
				{"window_clients", "Window Clients", "int"},
				{"client_resend_threshold", "Client Resend Threshold", "int"},
				{"client_reconnect_threshold", "Client Reconnect Threshold", "int"},
				{"client_reconnect_window_ms", "Reconnect Window", "ms"},
				{"client_reconnect_cooldown_ms", "Reconnect Cooldown", "ms"},
				{"global_clean_refresh_threshold", "Clean Refresh Threshold", "int"},
				{"global_clean_refresh_min_clients", "Clean Refresh Min Clients", "int"},
				{"cooldown_ms", "Cooldown", "ms"},
				{"last_recovery_age_ms", "Last Recovery Age", "ms"},
				{"cooldown_active", "Cooldown Active", "bool"},
			}),
		},
		{
			Title: "Submit Stall",
			Items: poolStatusEvidence(mapValue(status, "submit_stall"), []poolStatusField{
				{"enabled", "Enabled", "bool"},
				{"window_ms", "Window", "ms"},
				{"window_age_ms", "Window Age", "ms"},
				{"min_age_ms", "Min Age", "ms"},
				{"failures", "Failures", "int"},
				{"stale_failures", "Stale Failures", "int"},
				{"duplicate_failures", "Duplicate Failures", "int"},
				{"failure_threshold", "Failure Threshold", "int"},
				{"stale_threshold", "Stale Threshold", "int"},
				{"duplicate_threshold", "Duplicate Threshold", "int"},
				{"cooldown_ms", "Cooldown", "ms"},
				{"last_recovery_age_ms", "Last Recovery Age", "ms"},
				{"cooldown_active", "Cooldown Active", "bool"},
				{"reason_code", "Reason", ""},
			}),
		},
		{
			Title: "Template Conversion Stall",
			Items: poolStatusEvidence(mapValue(status, "template_conversion_stall"), []poolStatusField{
				{"enabled", "Enabled", "bool"},
				{"window_ms", "Window", "ms"},
				{"window_age_ms", "Window Age", "ms"},
				{"min_age_ms", "Min Age", "ms"},
				{"accepted", "Accepted", "int"},
				{"failures", "Failures", "int"},
				{"stale_failures", "Stale Failures", "int"},
				{"duplicate_failures", "Duplicate Failures", "int"},
				{"total_candidates", "Total Candidates", "int"},
				{"failure_ratio", "Failure Ratio", "percent"},
				{"failure_ratio_threshold", "Failure Ratio Threshold", "percent"},
				{"min_candidates", "Min Candidates", "int"},
				{"active_miners", "Active Miners", "int"},
				{"min_active_miners", "Min Active Miners", "int"},
				{"cooldown_ms", "Cooldown", "ms"},
				{"last_recovery_age_ms", "Last Recovery Age", "ms"},
				{"cooldown_active", "Cooldown Active", "bool"},
				{"last_reason", "Last Reason", ""},
				{"reason_code", "Reason", ""},
			}),
		},
	}
}

func poolStatusEvidence(data map[string]any, fields []poolStatusField) []Evidence {
	rows := make([]Evidence, 0, len(fields))
	for _, field := range fields {
		rows = append(rows, Evidence{field.Label, poolStatusValue(data[field.Key], field.Format)})
	}
	return rows
}

func poolStatusValue(value any, format string) string {
	raw := stringify(value)
	if strings.TrimSpace(raw) == "" {
		switch format {
		case "bool":
			return "unknown"
		case "int":
			return "not known"
		case "status":
			return "unknown"
		default:
			return "unknown"
		}
	}
	switch format {
	case "bool":
		return knownBoolStat(value)
	case "int":
		return integerStat(raw)
	case "ms":
		return milliseconds(raw)
	case "percent":
		return percent(raw)
	case "status":
		return first(titleStatus(raw), "unknown")
	case "unixms":
		return humanUnixMilli(raw)
	default:
		return raw
	}
}

func poolStatusTone(status string) string {
	switch lower(status) {
	case "ok", "ready", "mining", "healthy":
		return "ok"
	case "degraded", "warning", "warn", "no_active_miners":
		return "warn"
	case "":
		return "neutral"
	default:
		return "down"
	}
}
