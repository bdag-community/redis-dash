package main

type StatusReadinessSnapshot struct {
	Mode         string
	Overall      string
	CanAccept    bool
	CanSubmit    bool
	CanMine      bool
	State        string
	StateTone    string
	SyncProgress map[string]any
	MinerHealth  map[string]any
	PoolHealth   map[string]any
	Template     map[string]any
	Steps        []StatusStep
	Evidence     []Evidence
	Secondary    []Evidence
	Warnings     []string
	Notes        []string
}

func newStatusReadinessSnapshot(data map[string]any) StatusReadinessSnapshot {
	liveSource := stringValue(data, "source") == "redis-live"
	waitingLabel := "Waiting for Collector"
	modeLabel := "Collector Mode"
	overallLabel := "Collector Overall"
	if liveSource {
		waitingLabel = "Waiting for Live Data"
		modeLabel = "Live Mode"
		overallLabel = "Live Overall"
	}

	mode := lower(stringValue(data, "mode"))
	overall := lower(stringValue(data, "overall"))
	accepting := boolValue(data, "can_accept_shares")
	workFresh := !nestedBool(data, "pool_health", "stale_jobs")
	submitReady := boolValue(data, "can_submit_blocks")
	templateDegraded := templateHealthDegraded(data)
	displayOverall := first(overall, "unknown")
	if templateDegraded && displayOverall == "ok" {
		displayOverall = "degraded"
	}

	paidHealthy := recentAcceptedBlockSignal(data)
	minerSignal := minerPresenceSignal(data)
	poolRunning := nestedBool(data, "containers", "pool", "running")
	minersPresent := accepting || (poolRunning && minerSignal > 0)
	state, tone := collectorPoolState(data)
	templateUnsafe := templateCoinbaseUnsafe(data) || templateDegraded

	warnings := appendUniqueStrings(nil, stringSlice(data["warnings"])...)
	warnings = appendUniqueStrings(warnings, stringSlice(data["maintenance_warnings"])...)
	warnings = appendUniqueStrings(warnings, stringSlice(data["degraded_reasons"])...)
	if warning := asicFleetCapacityWarning(data); warning != "" {
		warnings = appendUniqueStrings(warnings, warning)
	}
	if boolValue(data, "collector_status_unavailable") {
		warnings = appendUniqueStrings(warnings, "Collector status sample is warming up; live node and pool state is not available yet.")
	}

	var notes []string
	if reason := stringValue(data, "status_reason"); reason != "" {
		notes = append(notes, reason)
	}
	if note := collectorCacheNote(data); note != "" {
		notes = append(notes, note)
	}

	return StatusReadinessSnapshot{
		Mode:         first(mode, "unknown"),
		Overall:      displayOverall,
		CanAccept:    accepting,
		CanSubmit:    submitReady,
		CanMine:      (boolValue(data, "can_mine") || (accepting && submitReady)) && !templateDegraded,
		State:        state,
		StateTone:    tone,
		SyncProgress: firstNestedMap(data, []string{"sync_progress"}),
		MinerHealth:  firstNestedMap(data, []string{"miner_health"}),
		PoolHealth:   firstNestedMap(data, []string{"pool_health"}),
		Template:     firstNestedMap(data, []string{"template_health"}),
		Steps: []StatusStep{
			{waitingLabel, state == "Waiting for Collector" || state == "Waiting for Live Data"},
			{"Down", state == "Down"},
			{"Waiting for a Miner", state == "Waiting for a Miner"},
			{"Waiting for Node Sync", state == "Waiting for Node Sync"},
			{"Template Unsafe", templateUnsafe},
			{"Degraded", state == "Degraded" || state == "Mining Degraded"},
			{"Mining", state == "Mining"},
		},
		Evidence: []Evidence{
			{modeLabel, first(mode, "unknown")},
			{overallLabel, first(overall, "unknown")},
			{"Miner Activity", yesNo(minersPresent)},
			{"Accepting Shares", yesNo(accepting)},
			{"Submit Ready", yesNo(submitReady)},
			{"Getting Fresh Work", yesNo(workFresh)},
			{"Recent Accepted Block", yesNo(paidHealthy)},
		},
		Secondary: []Evidence{
			{"Miner Inventory", minerInventoryStatus(data)},
			{"Valid Shares", nestedString(data, "pool_health", "valid_share_count")},
			{"Last Share Age", seconds(nestedString(data, "pool_health", "last_valid_share_age_seconds"))},
			{"Submit Errors", nestedString(data, "pool_health", "block_submit_error_count")},
			{"Template Coinbase Valid", templateCoinbaseValidStat(data)},
			{"Zero Coinbase Reject Rate", templateZeroCoinbaseRejectRate(data)},
			{"Ready Miner Jobs", templateReadyMinerJobs(data)},
			{"Local Stale Ratio", templateLocalStaleRatio(data)},
			{"Last Automation Restart", templateAutomationRestart(data)},
			{"Pool Endpoint", poolEndpointFromStatus(data)},
		},
		Warnings: warnings,
		Notes:    notes,
	}
}

func (s StatusReadinessSnapshot) applyTo(view *TabView) {
	view.Mode = s.Mode
	view.Overall = s.Overall
	view.CanAccept = s.CanAccept
	view.CanSubmit = s.CanSubmit
	view.CanMine = s.CanMine
	view.State = s.State
	view.StateTone = s.StateTone
	view.SyncProgress = s.SyncProgress
	view.MinerHealth = s.MinerHealth
	view.PoolHealth = s.PoolHealth
	view.Template = s.Template
	view.StatusSteps = s.Steps
	view.Evidence = s.Evidence
	view.Secondary = s.Secondary
	view.Warnings = appendUniqueStrings(view.Warnings, s.Warnings...)
	view.Notes = append(view.Notes, s.Notes...)
}
