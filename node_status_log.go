package main

import (
	"context"
	"strconv"
	"strings"
	"time"
)

const (
	nodeStatusLogEvent        = "node_status_log"
	nodeStatusLogSchema       = "bdag.nodeworker.status_log.v1"
	nodeStatusLogHistoryLimit = 48
	defaultNodeStatusLogTail  = 5000
)

type NodeStatusLogSnapshot struct {
	Found     bool
	Event     string
	Schema    string
	Timestamp string

	NativeSync map[string]any
	EVMSync    map[string]any
	History    []nodeStatusLogSample
}

type nodeStatusLogSample struct {
	At           time.Time
	Timestamp    string
	NativeStatus string
	EVMStatus    string
}

func (a *App) nodeStatusLogStatus(ctx context.Context) StatusPanel {
	logs := a.logContent(ctx, "node", nodeStatusLogTail())
	return newNodeStatusLogSnapshotFromLogs(logs).StatusPanel()
}

func nodeStatusLogTail() int {
	tail := intEnv("BDAG_NODE_STATUS_LOG_TAIL", defaultNodeStatusLogTail)
	if tail <= 0 {
		return defaultNodeStatusLogTail
	}
	return tail
}

func newNodeStatusLogSnapshotFromLogs(content string) NodeStatusLogSnapshot {
	var latest NodeStatusLogSnapshot
	history := make([]nodeStatusLogSample, 0, nodeStatusLogHistoryLimit)
	for _, raw := range jsonObjectsInText(content) {
		var payload map[string]any
		if err := decodeJSONObject(raw, &payload); err != nil {
			continue
		}
		if lower(mapString(payload, "event")) != nodeStatusLogEvent && mapString(payload, "schema") != nodeStatusLogSchema {
			continue
		}
		nativeSync, _ := payload["native_sync"].(map[string]any)
		if nativeSync == nil {
			nativeSync = map[string]any{}
		}
		evmSync, _ := payload["evm_sync"].(map[string]any)
		if evmSync == nil {
			evmSync = map[string]any{}
		}
		if sample, ok := newNodeStatusLogSample(payload, nativeSync, evmSync); ok {
			history = append(history, sample)
			if len(history) > nodeStatusLogHistoryLimit {
				history = history[len(history)-nodeStatusLogHistoryLimit:]
			}
		}
		latest = NodeStatusLogSnapshot{
			Found:     true,
			Event:     mapString(payload, "event"),
			Schema:    mapString(payload, "schema"),
			Timestamp: mapString(payload, "timestamp"),

			NativeSync: nativeSync,
			EVMSync:    evmSync,
			History:    append([]nodeStatusLogSample(nil), history...),
		}
	}
	return latest
}

func newNodeStatusLogSample(payload, nativeSync, evmSync map[string]any) (nodeStatusLogSample, bool) {
	timestamp := mapString(payload, "timestamp")
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return nodeStatusLogSample{}, false
	}
	return nodeStatusLogSample{
		At:           parsed,
		Timestamp:    timestamp,
		NativeStatus: lower(mapString(nativeSync, "status")),
		EVMStatus:    lower(mapString(evmSync, "status")),
	}, true
}

func (s NodeStatusLogSnapshot) StatusPanel() StatusPanel {
	if !s.Found {
		return StatusPanel{
			Title: "Node Status",
			State: "Waiting for node_status_log",
			Tone:  "warn",
			Evidence: []Evidence{
				{"State", "waiting for node_status_log"},
				{"Source", "node logs"},
				{"Schema", nodeStatusLogSchema},
			},
			Groups: []EvidenceGroup{
				{Title: "Native Sync", Items: []Evidence{{"Status", "waiting"}}},
				{Title: "EVM Sync", Items: []Evidence{{"Status", "waiting"}}},
				{Title: "Sync History", Items: []Evidence{
					{"Samples", "0"},
					{"Window", "waiting"},
					{"Native Sync Time", "0s"},
					{"EVM Sync Time", "0s"},
				}},
			},
		}
	}
	state, tone := s.stateAndTone()
	return StatusPanel{
		Title:    "Node Status",
		State:    state,
		Tone:     tone,
		Evidence: s.evidence(state),
		Groups:   s.groups(),
	}
}

func (s NodeStatusLogSnapshot) stateAndTone() (string, string) {
	nativeStatus := lower(mapString(s.NativeSync, "status"))
	evmStatus := lower(mapString(s.EVMSync, "status"))
	gap, gapKnown := s.nativeBlockGap()
	p2pCurrent, p2pKnown := boolKnown(s.NativeSync["p2p_current"])
	peerCount, peerCountKnown := parseIntStat(mapString(s.NativeSync, "peer_count"))

	switch {
	case nativeStatus == "p2p_down" || nativeStatus == "unable_to_sync" || (p2pKnown && !p2pCurrent) || (peerCountKnown && peerCount == 0):
		return "Native P2P Down", "down"
	case nativeStatus == "synced" && evmStatus == "synced" && (!gapKnown || gap == 0) && (!p2pKnown || p2pCurrent):
		return "Synced", "ok"
	case nativeStatus == "synced" && nodeStatusActive(evmStatus):
		return "EVM " + titleStatus(evmStatus), "warn"
	case nodeStatusActive(nativeStatus) || (gapKnown && gap > 0):
		return "Native " + first(titleStatus(nativeStatus), "Catching Up"), "warn"
	case nativeStatus != "synced" && nativeStatus != "":
		return "Native " + titleStatus(nativeStatus), "warn"
	case evmStatus != "synced" && evmStatus != "":
		return "EVM " + titleStatus(evmStatus), "warn"
	case gapKnown && gap > 0:
		return "Catching Up", "warn"
	default:
		return "Unknown", "neutral"
	}
}

func (s NodeStatusLogSnapshot) evidence(state string) []Evidence {
	gap, gapKnown := s.nativeBlockGap()
	rows := []Evidence{
		{"State", state},
		{"Native Status", first(titleStatus(mapString(s.NativeSync, "status")), "unknown")},
		{"EVM Status", first(titleStatus(mapString(s.EVMSync, "status")), "unknown")},
		{"Native Current", integerStat(mapString(s.NativeSync, "current_block"))},
		{"Native Target", integerStat(mapString(s.NativeSync, "target_block"))},
		{"Native Remaining", integerStat(mapString(s.NativeSync, "remaining_blocks"))},
		{"EVM Current", integerStat(mapString(s.EVMSync, "current_block"))},
		{"EVM Target", integerStat(mapString(s.EVMSync, "target_block"))},
		{"EVM Remaining", integerStat(mapString(s.EVMSync, "remaining_blocks"))},
		{"Native Peers", integerStat(mapString(s.NativeSync, "peer_count"))},
		{"Fresh Peers", integerStat(mapString(s.NativeSync, "fresh_peer_count"))},
		{"P2P Current", knownBoolStat(s.NativeSync["p2p_current"])},
		{"Mining Fresh", knownBoolStat(s.NativeSync["p2p_mining_fresh"])},
		{"EVM Eth Syncing", knownBoolStat(s.EVMSync["eth_syncing"])},
		{"EVM Remaining States", integerStat(mapString(s.EVMSync, "remaining_states"))},
	}
	if gapKnown {
		rows = append(rows, Evidence{"Native Block Gap", commaInt(gap)})
	}
	if eta := first(mapString(s.NativeSync, "eta_text"), mapString(s.NativeSync, "eta_seconds")); eta != "" {
		rows = append(rows, Evidence{"Native ETA", eta})
	}
	if eta := first(mapString(s.EVMSync, "eta_text"), mapString(s.EVMSync, "eta_seconds")); eta != "" {
		rows = append(rows, Evidence{"EVM ETA", eta})
	}
	if eta := first(mapString(s.EVMSync, "state_eta_text"), mapString(s.EVMSync, "state_eta_seconds")); eta != "" {
		rows = append(rows, Evidence{"EVM State ETA", eta})
	}
	rows = append(rows,
		Evidence{"Event Time", humanTime(s.Timestamp)},
		Evidence{"Schema", first(s.Schema, nodeStatusLogSchema)},
	)
	return rows
}

func (s NodeStatusLogSnapshot) groups() []EvidenceGroup {
	return []EvidenceGroup{
		{
			Title: "Native Sync",
			Items: []Evidence{
				{"Status", first(titleStatus(mapString(s.NativeSync, "status")), "unknown")},
				{"Current", integerStat(mapString(s.NativeSync, "current_block"))},
				{"Target", integerStat(mapString(s.NativeSync, "target_block"))},
				{"Remaining", integerStat(mapString(s.NativeSync, "remaining_blocks"))},
				{"Peers", integerStat(mapString(s.NativeSync, "peer_count"))},
				{"ETA", first(mapString(s.NativeSync, "eta_text"), mapString(s.NativeSync, "eta_seconds"), "unknown")},
			},
		},
		{
			Title: "EVM Sync",
			Items: []Evidence{
				{"Status", first(titleStatus(mapString(s.EVMSync, "status")), "unknown")},
				{"Current", integerStat(mapString(s.EVMSync, "current_block"))},
				{"Target", integerStat(mapString(s.EVMSync, "target_block"))},
				{"Remaining", integerStat(mapString(s.EVMSync, "remaining_blocks"))},
				{"Remaining States", integerStat(mapString(s.EVMSync, "remaining_states"))},
				{"ETA", first(mapString(s.EVMSync, "eta_text"), mapString(s.EVMSync, "eta_seconds"), "unknown")},
			},
		},
		s.syncHistoryGroup(),
	}
}

func (s NodeStatusLogSnapshot) syncHistoryGroup() EvidenceGroup {
	history := s.History
	if len(history) == 0 {
		return EvidenceGroup{
			Title: "Sync History",
			Items: []Evidence{
				{"Samples", "0"},
				{"Window", "waiting"},
				{"Native Sync Time", "0s"},
				{"EVM Sync Time", "0s"},
				{"Both Synced Time", "0s"},
				{"Latest Sample", humanTime(s.Timestamp)},
				{"Native Sync Samples", "0/0"},
				{"EVM Sync Samples", "0/0"},
			},
		}
	}
	firstSample := history[0]
	latestSample := history[len(history)-1]
	var nativeSyncing, evmSyncing, bothSynced time.Duration
	nativeActiveSamples := 0
	evmActiveSamples := 0
	for i, sample := range history {
		if nodeStatusActive(sample.NativeStatus) {
			nativeActiveSamples++
		}
		if nodeStatusActive(sample.EVMStatus) {
			evmActiveSamples++
		}
		if i+1 >= len(history) {
			continue
		}
		next := history[i+1]
		elapsed := next.At.Sub(sample.At)
		if elapsed <= 0 {
			continue
		}
		if nodeStatusActive(sample.NativeStatus) {
			nativeSyncing += elapsed
		}
		if nodeStatusActive(sample.EVMStatus) {
			evmSyncing += elapsed
		}
		if sample.NativeStatus == "synced" && sample.EVMStatus == "synced" {
			bothSynced += elapsed
		}
	}
	window := latestSample.At.Sub(firstSample.At)
	if window < 0 {
		window = 0
	}
	total := strconv.Itoa(len(history))
	return EvidenceGroup{
		Title: "Sync History",
		Items: []Evidence{
			{"Samples", total},
			{"Window", compactDuration(window)},
			{"Native Sync Time", compactDuration(nativeSyncing)},
			{"EVM Sync Time", compactDuration(evmSyncing)},
			{"Both Synced Time", compactDuration(bothSynced)},
			{"Latest Sample", humanTime(latestSample.Timestamp)},
			{"Native Sync Samples", strconv.Itoa(nativeActiveSamples) + "/" + total},
			{"EVM Sync Samples", strconv.Itoa(evmActiveSamples) + "/" + total},
		},
	}
}

func compactDuration(d time.Duration) string {
	seconds := int64(d.Round(time.Second) / time.Second)
	if seconds <= 0 {
		return "0s"
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	remainingSeconds := seconds % 60
	if hours > 0 {
		if minutes > 0 {
			return strconv.FormatInt(hours, 10) + "h " + strconv.FormatInt(minutes, 10) + "m"
		}
		return strconv.FormatInt(hours, 10) + "h"
	}
	if minutes > 0 {
		if remainingSeconds > 0 {
			return strconv.FormatInt(minutes, 10) + "m " + strconv.FormatInt(remainingSeconds, 10) + "s"
		}
		return strconv.FormatInt(minutes, 10) + "m"
	}
	return strconv.FormatInt(remainingSeconds, 10) + "s"
}

func (s NodeStatusLogSnapshot) nativeBlockGap() (int64, bool) {
	if remaining, remainingOK := parseIntStat(mapString(s.NativeSync, "remaining_blocks")); remainingOK {
		return remaining, true
	}
	current, currentOK := parseIntStat(mapString(s.NativeSync, "current_block"))
	target, targetOK := parseIntStat(mapString(s.NativeSync, "target_block"))
	if currentOK && targetOK {
		if target < current {
			return 0, true
		}
		return target - current, true
	}
	return 0, false
}

func nodeStatusActive(status string) bool {
	switch status {
	case "syncing", "catching_up":
		return true
	default:
		return false
	}
}

func parseIntStat(value string) (int64, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false
	}
	return int64(parsed), true
}

func knownBoolStat(value any) string {
	known, ok := boolKnown(value)
	if !ok {
		return "unknown"
	}
	return yesNo(known)
}
