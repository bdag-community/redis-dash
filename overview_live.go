package main

import "strings"

// liveNodeStatus projects the same cached status sample used by the pool view.
// Missing or unsafe evidence is never presented as a synced node.
func liveNodeStatus(data map[string]any) StatusPanel {
	sync := mapValue(data, "sync_progress")
	status := strings.ToLower(strings.TrimSpace(mapString(sync, "status")))
	current, currentKnown := boolKnown(sync["native_is_current"])
	evmSyncing, evmKnown := boolKnown(sync["eth_syncing"])
	state, tone := "Unknown", "neutral"
	switch {
	case boolValue(data, "collector_status_unavailable"):
		state, tone = "Unavailable", "down"
	case status == "p2p_down" || status == "unable_to_sync":
		state, tone = "P2P Down", "down"
	case mapString(sync, "chain_rpc_error") != "" || mapString(sync, "error") != "":
		state, tone = "Degraded", "warn"
	case status == "synced" && currentKnown && !current:
		state, tone = "Catching Up", "warn"
	case status == "synced" && evmKnown && evmSyncing:
		state, tone = "EVM Syncing", "warn"
	case status == "synced" && (!currentKnown || !evmKnown):
		state, tone = "Unknown", "neutral"
	case status == "synced":
		state, tone = "Synced", "ok"
	case status != "" && status != "unknown":
		state, tone = titleStatus(status), "warn"
	}
	return StatusPanel{
		Title: "Node Status", State: state, Tone: tone,
		StatusSteps: []StatusStep{
			{Label: "Unavailable", Active: state == "Unavailable" || state == "P2P Down"},
			{Label: "Syncing", Active: tone == "warn"},
			{Label: "Synced", Active: state == "Synced"},
			{Label: "Unknown", Active: state == "Unknown"},
		},
		Evidence: []Evidence{
			{Label: "Native Status", Value: first(titleStatus(status), "unknown")},
			{Label: "Chain Block Count", Value: integerStat(first(mapString(sync, "chain_block_count"), mapString(sync, "current_block")))},
			{Label: "EVM Block", Value: integerStat(mapString(sync, "evm_block_count"))},
			{Label: "Peers", Value: integerStat(first(mapString(sync, "peer_count"), mapString(sync, "p2p_connections")))},
			{Label: "Native Current", Value: knownBoolStat(sync["native_is_current"])},
			{Label: "EVM Syncing", Value: knownBoolStat(sync["eth_syncing"])},
		},
	}
}
