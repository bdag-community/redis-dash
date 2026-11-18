package main

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Do not infer the pool endpoint from an individual miner's desired route:
// a miner may be configured for another pool.
func localPoolHashpowerURL(status map[string]any) string {
	return first(env("BDAG_POOL_URL", ""), env("MINER_POOL_URL", ""),
		stringValue(status, "pool_endpoint"), nestedString(status, "pool_health", "configured_pool_url"), minerDefaultPoolURL())
}

// poolHashpower displays only fresh ASIC-reported rates whose active endpoint
// matches this pool. It is an observed sum, not a share-based measurement or
// proof that every pool participant has telemetry.
func poolHashpower(status map[string]any, poolURL string) string {
	unavailable := "unavailable"
	endpoint := hashpowerPoolEndpoint(poolURL)
	telemetry := mapValue(status, "asic_telemetry")
	if endpoint == "" || lower(mapString(telemetry, "status")) != "ok" || !hashpowerFresh(telemetry) {
		return unavailable
	}
	seen := make(map[string]bool)
	var total float64
	count := 0
	for _, device := range listMaps(telemetry["devices"]) {
		if lower(mapString(device, "status")) != "ok" || !hashpowerFresh(device) {
			continue
		}
		mac := normalizedMAC(mapString(device, "mac"))
		summary := mapValue(device, "summary")
		if mac == "" || seen[mac] || hashpowerPoolEndpoint(mapString(summary, "active_pool_url")) != endpoint {
			continue
		}
		rate, ok := hashpowerGH(mapString(summary, "hashrate"))
		if !ok {
			continue
		}
		seen[mac] = true
		total += rate
		count++
	}
	if count == 0 || math.IsInf(total, 0) {
		return unavailable
	}
	label := "ASICs"
	if count == 1 {
		label = "ASIC"
	}
	if poolHashpowerComplete(status, seen, time.Now()) {
		return fmt.Sprintf("%.2f GH/s total (%d %s)", total, count, label)
	}
	return fmt.Sprintf("%.2f GH/s observed (%d %s)", total, count, label)
}

// A total is justified only when every running configured pool reports fresh
// job state and every connected lane maps to a measured ASIC, with no extra
// measured ASICs. Otherwise the same sum is explicitly only observed.
func poolHashpowerComplete(status map[string]any, measured map[string]bool, now time.Time) bool {
	state := mapValue(status, "pool_job_state")
	if lower(mapString(state, "status")) != "ok" || mapString(state, "source") != "pool-health-job-state" {
		return false
	}
	timestamp := mapString(state, "generated_at")
	generated, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		generated, err = time.Parse("2006-01-02T15:04:05-0700", timestamp)
	}
	if err != nil && timestamp == "" {
		if milliseconds, ok := parseNumber(mapString(state, "generated_at_unix_ms")); ok && milliseconds > 0 && milliseconds == math.Trunc(milliseconds) {
			generated, err = time.UnixMilli(int64(milliseconds)), nil
		}
	}
	if err != nil || generated.After(now) || now.Sub(generated) > 120*time.Second {
		return false
	}
	running, ok := parseNumber(mapString(state, "running_pool_containers"))
	reporting, okReporting := parseNumber(mapString(state, "reporting_pool_containers"))
	// A direct pool endpoint describes one pool rather than an aggregate of
	// containers. Its native timestamp and router distinguish that response.
	if !ok && !okReporting && mapString(state, "generated_at_unix_ms") != "" && len(mapValue(state, "router")) > 0 {
		running, reporting, ok, okReporting = 1, 1, true, true
	}
	active, okActive := parseNumber(mapString(state, "active_connections"))
	authorized, okAuthorized := parseNumber(mapString(state, "authorized_connections"))
	if !ok || !okReporting || !okActive || !okAuthorized || running < 1 || running != math.Trunc(running) || reporting != running || active < 1 || active != math.Trunc(active) || authorized != active {
		return false
	}
	clients := listMaps(state["clients"])
	if float64(len(clients)) != active {
		return false
	}
	connected := make(map[string]bool, len(clients))
	for _, client := range clients {
		mac := normalizedMAC(mapString(client, "asic_mac"))
		if mac == "" || connected[mac] || mapString(client, "authorized") != "true" || !measured[mac] {
			return false
		}
		connected[mac] = true
	}
	return len(connected) == len(measured)
}

func hashpowerFresh(row map[string]any) bool {
	age, ok := parseNumber(mapString(row, "age_seconds"))
	return ok && age >= 0 && age <= 120
}

func hashpowerPoolEndpoint(value string) string {
	u, err := url.Parse(normalizeStratumURL(value))
	if err != nil || u.User != nil || u.Hostname() == "" || u.Port() == "" || strings.ContainsAny(u.Hostname(), "<>") {
		return ""
	}
	return strings.ToLower(u.Hostname()) + ":" + u.Port()
}

func hashpowerGH(value string) (float64, bool) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(value)))
	if len(fields) == 0 || len(fields) > 2 {
		return 0, false
	}
	rate, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, false
	}
	// Existing ASIC telemetry reports unitless values in GH/s.
	unit := "gh/s"
	if len(fields) == 2 {
		unit = fields[1]
	}
	switch unit {
	case "gh/s", "ghs":
	case "th/s", "ths":
		rate *= 1000
	case "mh/s", "mhs":
		rate /= 1000
	case "kh/s", "khs":
		rate /= 1000000
	default:
		return 0, false
	}
	return rate, !math.IsInf(rate, 0)
}
