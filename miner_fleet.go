package main

import (
	"net"
	"sort"
	"strings"
)

// MinerFleetSnapshot is a read-only projection over the current miner evidence.
type MinerFleetSnapshot struct {
	miners           []MinerFleetMiner
	performanceLanes []MinerFleetLane
	deviceRows       []MinerFleetMiner
}

type MinerFleetMiner struct {
	raw map[string]any
}

type MinerFleetLane struct {
	raw map[string]any
}

func newMinerFleetSnapshot(data map[string]any) MinerFleetSnapshot {
	displayRows := minerDisplayRows(data)
	miners := make([]MinerFleetMiner, 0, len(displayRows))
	for _, row := range displayRows {
		miners = append(miners, MinerFleetMiner{raw: row})
	}

	laneRows := listMaps(nestedAny(data, "asic_performance", "lanes"))
	performanceLanes := make([]MinerFleetLane, 0, len(laneRows))
	if len(laneRows) > 0 {
		for _, lane := range laneRows {
			performanceLanes = append(performanceLanes, MinerFleetLane{raw: lane})
		}
	} else {
		for _, row := range displayRows {
			performanceLanes = append(performanceLanes, MinerFleetLane{raw: row})
		}
	}

	deviceRows := miners
	if len(deviceRows) == 0 {
		telemetryRows := telemetryDeviceRows(data)
		deviceRows = make([]MinerFleetMiner, 0, len(telemetryRows))
		for _, row := range telemetryRows {
			deviceRows = append(deviceRows, MinerFleetMiner{raw: row})
		}
	}

	return MinerFleetSnapshot{
		miners:           miners,
		performanceLanes: performanceLanes,
		deviceRows:       deviceRows,
	}
}

func (s MinerFleetSnapshot) StatusRows() []map[string]string {
	out := make([]map[string]string, 0, len(s.miners))
	for _, miner := range s.miners {
		out = append(out, miner.statusRow())
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["observed"] < out[j]["observed"] })
	return out
}

func (s MinerFleetSnapshot) PerformanceRows() []map[string]string {
	out := make([]map[string]string, 0, len(s.performanceLanes))
	for _, lane := range s.performanceLanes {
		out = append(out, lane.performanceRow())
	}
	sort.SliceStable(out, func(i, j int) bool {
		return first(out[i]["ip"], out[i]["mac"]) < first(out[j]["ip"], out[j]["mac"])
	})
	return out
}

func (s MinerFleetSnapshot) DeviceRows() []map[string]string {
	out := make([]map[string]string, 0, len(s.deviceRows))
	for _, miner := range s.deviceRows {
		out = append(out, miner.deviceRow())
	}
	sort.SliceStable(out, func(i, j int) bool {
		return first(out[i]["ip"], out[i]["mac"]) < first(out[j]["ip"], out[j]["mac"])
	})
	return out
}

func (s MinerFleetSnapshot) ControlRows() []map[string]string {
	statusRows := s.StatusRows()
	out := make([]map[string]string, 0, len(statusRows))
	for _, row := range statusRows {
		parsedIP := net.ParseIP(row["ip"])
		if parsedIP == nil || parsedIP.To4() == nil || !parsedIP.IsPrivate() || parsedIP.IsLoopback() || parsedIP.IsMulticast() || parsedIP.IsUnspecified() {
			continue
		}
		ip := row["ip"]
		mac := normalizedMAC(row["mac"])
		candidate := map[string]string{}
		for key, value := range row {
			candidate[key] = value
		}
		candidate["target"] = ip + "|" + mac
		candidate["label"] = first(strings.ToUpper(mac), ip)
		out = append(out, candidate)
	}
	return out
}

func (m MinerFleetMiner) statusRow() map[string]string {
	ip := mapString(m.raw, "ip")
	return map[string]string{
		"mac":      minerMAC(m.raw),
		"identity": first(mapString(m.raw, "mac"), mapString(m.raw, "identity"), mapString(m.raw, "name"), "not known"),
		"ip":       first(ip, "not known"),
		"observed": strings.Join(nonEmpty(ip, mapString(m.raw, "worker"), mapString(m.raw, "port")), " / "),
		"intent":   first(mapString(m.raw, "configured_pool_url"), mapString(m.raw, "pool_url"), mapString(m.raw, "intended_wallet"), "not known"),
		"status":   first(mapString(m.raw, "status"), mapString(m.raw, "health"), "not known"),
		"shares":   first(mapString(m.raw, "shares"), mapString(m.raw, "valid_shares"), "0"),
		"last":     humanTime(first(mapString(m.raw, "last_share_at"), mapString(m.raw, "last_seen_at"), "not known")),
	}
}

func (m MinerFleetMiner) deviceRow() map[string]string {
	mac := minerMAC(m.raw)
	if mac == "not known" {
		mac = normalizedMAC(mapString(m.raw, "mac"))
	}
	return map[string]string{
		"mac":           first(mac, "not known"),
		"ip":            first(mapString(m.raw, "ip"), "not known"),
		"model":         first(mapString(m.raw, "device_model"), mapString(m.raw, "model"), "unknown"),
		"firmware":      first(mapString(m.raw, "firmware"), "unknown"),
		"clock":         first(mapString(m.raw, "clock_mhz"), "unknown"),
		"voltage":       first(mapString(m.raw, "voltage_v"), "unknown"),
		"chips":         first(mapString(m.raw, "chip_count"), "unknown"),
		"temp":          first(mapString(m.raw, "temperature"), "unknown"),
		"fan":           first(mapString(m.raw, "fanspeed"), "unknown"),
		"hashrate":      first(mapString(m.raw, "hashrate"), mapString(m.raw, "avg_hashrate"), "unknown"),
		"powerplan":     first(mapString(m.raw, "manual_powerplan"), "unknown"),
		"active_pool":   first(mapString(m.raw, "active_pool_url"), mapString(m.raw, "configured_pool_url"), "unknown"),
		"pool_user":     first(mapString(m.raw, "active_pool_user"), shortAddress(mapString(m.raw, "intended_wallet")), "unknown"),
		"pool_legal":    first(mapString(m.raw, "active_pool_legal"), "unknown"),
		"pool_pass":     yesNo(truthyValue(m.raw["pool_pass_present"])),
		"miner_status":  first(mapString(m.raw, "device_miner_status"), mapString(m.raw, "status"), "unknown"),
		"accepted":      first(mapString(m.raw, "device_accepted"), "0"),
		"rejected":      first(mapString(m.raw, "device_rejected"), "0"),
		"hw_errors":     first(mapString(m.raw, "device_hw_errors"), "0"),
		"telemetry_age": seconds(mapString(m.raw, "device_telemetry_age_seconds")),
		"telemetry":     first(mapString(m.raw, "device_telemetry_status"), "unknown"),
	}
}

func (l MinerFleetLane) performanceRow() map[string]string {
	mac := first(normalizedMAC(mapString(l.raw, "asic_mac")), normalizedMAC(mapString(l.raw, "mac")), "not known")
	ip := first(mapString(l.raw, "remote_host"), mapString(l.raw, "client_addr"), mapString(l.raw, "ip"), "not known")
	rejected30M := number(mapString(l.raw, "rejected_30m")) + number(mapString(l.raw, "rejected_local_30m"))
	return map[string]string{
		"mac":         mac,
		"ip":          ip,
		"ready":       yesNo(truthyValue(l.raw["ready"]) || truthyValue(l.raw["connected"])),
		"health":      first(mapString(l.raw, "health"), mapString(l.raw, "status"), "not known"),
		"job_age":     milliseconds(mapString(l.raw, "current_job_age_ms")),
		"blocks_30m":  blockCountDisplay(l.raw, "blocks_30m"),
		"blocks_1h":   blockCountDisplay(l.raw, "blocks_1h"),
		"blocks_24h":  blockCountDisplay(l.raw, "blocks_24h"),
		"rejects_30m": decimalInt(rejected30M),
		"protocol":    first(mapString(l.raw, "protocol_variant"), "stratum-v1-basic"),
		"last_block":  first(humanUnixMilli(mapString(l.raw, "last_accepted_unix_ms"), mapString(l.raw, "last_block_at_epoch_ms"), mapString(l.raw, "last_event_unix_ms"), mapString(l.raw, "last_activity_unix_ms")), humanTime(mapString(l.raw, "last_seen_at"))),
	}
}
