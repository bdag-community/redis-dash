package main

import (
	"context"
	"testing"
)

func TestNodeStatusLogSnapshotProjectsLatestLogEvent(t *testing.T) {
	logs := `noise
{"event":"node_status_log","schema":"bdag.nodeworker.status_log.v1","timestamp":"2026-06-26T22:10:18.964256675Z","native_sync":{"status":"synced","current_block":12769927,"target_block":12769927,"remaining_blocks":0,"peer_count":20,"fresh_peer_count":20,"p2p_current":true,"p2p_mining_fresh":true,"eta_text":"0s"},"evm_sync":{"status":"synced","current_block":12769927,"target_block":12769927,"remaining_blocks":0,"eth_syncing":false,"remaining_states":0,"eta_text":"0s","state_eta_text":"0s"}}
{"event":"node_status_log","schema":"bdag.nodeworker.status_log.v1","timestamp":"2026-06-26T22:11:18Z","native_sync":{"status":"syncing","current_block":12400419,"target_block":12769927,"remaining_blocks":369508,"peer_count":20,"fresh_peer_count":19,"p2p_current":true,"p2p_mining_fresh":false,"eta_text":"1h1m"},"evm_sync":{"status":"catching_up","current_block":12400400,"target_block":12769927,"remaining_blocks":369527,"eth_syncing":false,"remaining_states":1200,"eta_text":"1h2m","state_eta_text":"4m"}}`

	panel := newNodeStatusLogSnapshotFromLogs(logs).StatusPanel()

	if panel.State != "Native Syncing" || panel.Tone != "warn" {
		t.Fatalf("node state = %s/%s, want Native Syncing/warn", panel.State, panel.Tone)
	}
	if got := evidenceValue(panel.Evidence, "Native Current"); got != "12,400,419" {
		t.Fatalf("Native Current = %q, want 12,400,419", got)
	}
	if got := evidenceValue(panel.Evidence, "Native Target"); got != "12,769,927" {
		t.Fatalf("Native Target = %q, want 12,769,927", got)
	}
	if got := evidenceValue(panel.Evidence, "Native Block Gap"); got != "369,508" {
		t.Fatalf("Native Block Gap = %q, want 369,508", got)
	}
	if got := evidenceValue(panel.Evidence, "P2P Current"); got != "yes" {
		t.Fatalf("P2P Current = %q, want yes", got)
	}
	if got := evidenceValue(panel.Evidence, "EVM Status"); got != "Catching Up" {
		t.Fatalf("EVM Status = %q, want Catching Up", got)
	}
	if got := evidenceValue(panel.Evidence, "EVM Remaining States"); got != "1,200" {
		t.Fatalf("EVM Remaining States = %q, want 1,200", got)
	}
	if len(panel.Groups) != 3 {
		t.Fatalf("node groups len = %d, want 3", len(panel.Groups))
	}
	if panel.Groups[0].Title != "Native Sync" || panel.Groups[1].Title != "EVM Sync" || panel.Groups[2].Title != "Sync History" {
		t.Fatalf("node groups = %#v, want Native Sync, EVM Sync, and Sync History", panel.Groups)
	}
}

func TestNodeStatusLogSnapshotProjectsSyncHistory(t *testing.T) {
	logs := `{"event":"node_status_log","schema":"bdag.nodeworker.status_log.v1","timestamp":"2026-06-26T22:00:00Z","native_sync":{"status":"syncing"},"evm_sync":{"status":"synced"}}
{"event":"node_status_log","schema":"bdag.nodeworker.status_log.v1","timestamp":"2026-06-26T22:00:10Z","native_sync":{"status":"syncing"},"evm_sync":{"status":"catching_up"}}
{"event":"node_status_log","schema":"bdag.nodeworker.status_log.v1","timestamp":"2026-06-26T22:00:20Z","native_sync":{"status":"synced"},"evm_sync":{"status":"catching_up"}}
{"event":"node_status_log","schema":"bdag.nodeworker.status_log.v1","timestamp":"2026-06-26T22:00:30Z","native_sync":{"status":"synced"},"evm_sync":{"status":"synced"}}`

	panel := newNodeStatusLogSnapshotFromLogs(logs).StatusPanel()
	if len(panel.Groups) != 3 {
		t.Fatalf("node groups len = %d, want 3", len(panel.Groups))
	}
	history := panel.Groups[2]
	if history.Title != "Sync History" {
		t.Fatalf("history group title = %q, want Sync History", history.Title)
	}
	if got := evidenceValue(history.Items, "Samples"); got != "4" {
		t.Fatalf("Samples = %q, want 4", got)
	}
	if got := evidenceValue(history.Items, "Window"); got != "30s" {
		t.Fatalf("Window = %q, want 30s", got)
	}
	if got := evidenceValue(history.Items, "Native Sync Time"); got != "20s" {
		t.Fatalf("Native Sync Time = %q, want 20s", got)
	}
	if got := evidenceValue(history.Items, "EVM Sync Time"); got != "20s" {
		t.Fatalf("EVM Sync Time = %q, want 20s", got)
	}
	if got := evidenceValue(history.Items, "Native Sync Samples"); got != "2/4" {
		t.Fatalf("Native Sync Samples = %q, want 2/4", got)
	}
	if got := evidenceValue(history.Items, "EVM Sync Samples"); got != "2/4" {
		t.Fatalf("EVM Sync Samples = %q, want 2/4", got)
	}
}

func TestNodeStatusLogSnapshotDoesNotTreatMissingPeerCountAsZero(t *testing.T) {
	logs := `{"event":"node_status_log","schema":"bdag.nodeworker.status_log.v1","timestamp":"2026-06-26T22:11:18Z","native_sync":{"status":"syncing","current_block":12400419,"target_block":12769927,"p2p_current":true},"evm_sync":{"status":"synced"}}`

	panel := newNodeStatusLogSnapshotFromLogs(logs).StatusPanel()

	if panel.State != "Native Syncing" || panel.Tone != "warn" {
		t.Fatalf("node state = %s/%s, want Native Syncing/warn", panel.State, panel.Tone)
	}
}

func TestNodeStatusLogSnapshotRejectsOldSyncETAEvent(t *testing.T) {
	logs := `{"event":"node_sync_eta","schema":"bdag.nodeworker.sync_eta.v1","timestamp":"2026-06-26T22:11:18Z","native_sync":{"status":"syncing"}}`

	panel := newNodeStatusLogSnapshotFromLogs(logs).StatusPanel()

	if panel.State != "Waiting for node_status_log" {
		t.Fatalf("node state = %q, want waiting for node_status_log", panel.State)
	}
}

func TestNodeStatusLogStatusUsesWideTailForHighVolumeSyncLogs(t *testing.T) {
	var gotTail int
	app := &App{
		nodeLogProvider: func(_ context.Context, tail int) string {
			gotTail = tail
			return `{"event":"node_status_log","schema":"bdag.nodeworker.status_log.v1","timestamp":"2026-06-26T22:11:18Z","native_sync":{"status":"syncing"},"evm_sync":{"status":"synced"}}`
		},
	}

	panel := app.nodeStatusLogStatus(context.Background())

	if gotTail != defaultNodeStatusLogTail {
		t.Fatalf("tail = %d, want %d", gotTail, defaultNodeStatusLogTail)
	}
	if panel.State != "Native Syncing" {
		t.Fatalf("node state = %q, want Native Syncing", panel.State)
	}
}

func TestNodeStatusLogTailCanBeConfigured(t *testing.T) {
	t.Setenv("BDAG_NODE_STATUS_LOG_TAIL", "9000")

	if got := nodeStatusLogTail(); got != 9000 {
		t.Fatalf("nodeStatusLogTail = %d, want 9000", got)
	}
}
