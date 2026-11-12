package main

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
)

type GlobalProductionSnapshot struct {
	data map[string]any
}

func newGlobalProductionSnapshot(data map[string]any) GlobalProductionSnapshot {
	return GlobalProductionSnapshot{data: data}
}

func (s GlobalProductionSnapshot) ProducerRows() []map[string]any {
	if rows := listMaps(s.data["clusters"]); len(rows) > 0 {
		return rows
	}
	if rows := listMaps(s.data["chain_clusters"]); len(rows) > 0 {
		return rows
	}
	return nil
}

func (s GlobalProductionSnapshot) HasProducerRows() bool {
	return len(s.ProducerRows()) > 0
}

func (s GlobalProductionSnapshot) PoolEarningRows(producerColors map[string]string, localProducers map[string]bool) []map[string]string {
	rows := s.ProducerRows()
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		key := producerKey(row)
		local := boolValue(row, "local_pool") || localProducers[key]
		out = append(out, map[string]string{
			"pool":   first(mapString(row, "label"), mapString(row, "address_short"), mapString(row, "address"), "not known"),
			"key":    key,
			"local":  boolString(local),
			"blocks": first(mapString(row, "blocks"), "0"),
			"work":   percent(mapString(row, "share_percent")),
			"last":   humanTime(first(mapString(row, "last_seen_at"), "not known")),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return number(out[i]["blocks"]) > number(out[j]["blocks"]) })
	if len(out) > 20 {
		out = out[:20]
	}
	for i := range out {
		color := producerColors[out[i]["key"]]
		if color == "" {
			color = stableProducerColor(out[i]["key"])
		}
		out[i]["color"] = color
	}
	return out
}

func (s GlobalProductionSnapshot) ChartJSON() (string, map[string]string, map[string]bool) {
	type series struct {
		Label  string       `json:"label"`
		Color  string       `json:"color"`
		Local  bool         `json:"local"`
		Points []chartPoint `json:"points"`
	}
	if compact := listMaps(s.data["chart_history"]); len(compact) > 0 {
		return s.compactChartJSON(compact)
	}
	latest := listMaps(s.data["history"])
	if globalSnapshotUsableForCurrentTrend(s.data) {
		latest = append(latest, s.data)
	}
	rewardPerBlock := trendRewardPerBlockBDAG()
	priceUSD, _ := configuredBDAGPriceUSD()
	seriesMap := map[string][]chartPoint{}
	labels := map[string]string{}
	localProducers := map[string]bool{}
	for _, snap := range latest {
		t := first(mapString(snap, "generated_at"), mapString(snap, "updated_at"))
		window := number(mapString(snap, "scan_window_hours"))
		if window <= 0 {
			window = 1
		}
		for _, row := range newGlobalProductionSnapshot(snap).ProducerRows() {
			key := producerKey(row)
			if key == "" {
				continue
			}
			labels[key] = producerLabel(row, key)
			if boolValue(row, "local_pool") {
				localProducers[key] = true
			}
			blocks := number(mapString(row, "blocks")) / window
			bdag := firstNumber(row, "estimated_bdag_avg_hour", "estimated_bdag_recent_hour")
			usd := firstNumber(row, "estimated_usd_avg_hour", "estimated_usd_recent_hour")
			if bdag <= 0 {
				bdag = blocks * rewardPerBlock
			}
			if usd <= 0 && priceUSD > 0 {
				usd = bdag * priceUSD
			}
			if blocks <= 0 || math.IsNaN(blocks) || math.IsInf(blocks, 0) {
				continue
			}
			seriesMap[key] = append(seriesMap[key], chartPoint{T: t, Blocks: blocks, BDAG: bdag, USD: usd})
		}
	}
	keys := make([]string, 0, len(seriesMap))
	for key := range seriesMap {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return lastPoint(seriesMap[keys[i]]) > lastPoint(seriesMap[keys[j]])
	})
	if len(keys) > 8 {
		keys = keys[:8]
		for localKey := range localProducers {
			if _, ok := seriesMap[localKey]; ok && !stringIn(localKey, keys) {
				keys[len(keys)-1] = localKey
				break
			}
		}
	}
	colors := make(map[string]string, len(keys))
	out := make([]series, 0, len(keys))
	for _, key := range keys {
		points := seriesMap[key]
		if len(points) > 744 {
			points = points[len(points)-744:]
		}
		points = smoothChartPoints(points, trendSmoothingWindow)
		color := stableProducerColor(key)
		colors[key] = color
		out = append(out, series{Label: labels[key], Color: color, Local: localProducers[key], Points: points})
	}
	b, _ := json.Marshal(out)
	return string(b), colors, localProducers
}

func (s GlobalProductionSnapshot) compactChartJSON(history []map[string]any) (string, map[string]string, map[string]bool) {
	type series struct {
		Label  string       `json:"label"`
		Color  string       `json:"color"`
		Local  bool         `json:"local"`
		Points []chartPoint `json:"points"`
	}
	seriesMap := map[string][]chartPoint{}
	labels := map[string]string{}
	localProducers := map[string]bool{}
	seen := map[string]bool{}
	rewardPerBlock := trendRewardPerBlockBDAG()
	priceUSD, _ := configuredBDAGPriceUSD()
	for _, row := range s.ProducerRows() {
		key := strings.ToLower(producerKey(row))
		if key == "" {
			continue
		}
		labels[key] = producerLabel(row, key)
		if boolValue(row, "local_pool") {
			localProducers[key] = true
		}
	}
	for _, point := range history {
		t := first(mapString(point, "t"), mapString(point, "updated_at"))
		if t == "" {
			continue
		}
		windowHours := number(mapString(point, "scan_window_hours"))
		if windowHours <= 0 {
			if seconds := number(mapString(point, "window_seconds")); seconds > 0 {
				windowHours = seconds / 3600
			}
		}
		if windowHours <= 0 {
			windowHours = 1
		}
		pointLabels := stringMap(point["labels"])
		pointLocal := boolMap(point["local_pools"])
		for key, count := range numberMap(point["counts"]) {
			key = strings.ToLower(strings.TrimSpace(key))
			if key == "" {
				continue
			}
			if label := pointLabels[key]; label != "" {
				labels[key] = label
			}
			if pointLocal[key] {
				localProducers[key] = true
			}
			blocks := count / windowHours
			if blocks <= 0 || math.IsNaN(blocks) || math.IsInf(blocks, 0) {
				continue
			}
			seen[key+"\x00"+t] = true
			bdag := blocks * rewardPerBlock
			usd := 0.0
			if priceUSD > 0 {
				usd = bdag * priceUSD
			}
			seriesMap[key] = append(seriesMap[key], chartPoint{T: t, Blocks: blocks, BDAG: bdag, USD: usd})
		}
	}
	currentT := first(mapString(s.data, "updated_at"), mapString(s.data, "generated_at"))
	if currentT != "" && globalSnapshotUsableForCurrentTrend(s.data) {
		windowHours := number(mapString(s.data, "scan_window_hours"))
		if windowHours <= 0 {
			if seconds := number(mapString(s.data, "scan_window_seconds")); seconds > 0 {
				windowHours = seconds / 3600
			}
		}
		if windowHours <= 0 {
			windowHours = 1
		}
		for _, row := range s.ProducerRows() {
			key := strings.ToLower(producerKey(row))
			if key == "" || seen[key+"\x00"+currentT] {
				continue
			}
			labels[key] = first(labels[key], producerLabel(row, key))
			if boolValue(row, "local_pool") {
				localProducers[key] = true
			}
			blocks := number(mapString(row, "blocks")) / windowHours
			if blocks <= 0 || math.IsNaN(blocks) || math.IsInf(blocks, 0) {
				continue
			}
			bdag := blocks * rewardPerBlock
			usd := 0.0
			if priceUSD > 0 {
				usd = bdag * priceUSD
			}
			seriesMap[key] = append(seriesMap[key], chartPoint{T: currentT, Blocks: blocks, BDAG: bdag, USD: usd})
		}
	}
	keys := make([]string, 0, len(seriesMap))
	for key := range seriesMap {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return lastPoint(seriesMap[keys[i]]) > lastPoint(seriesMap[keys[j]])
	})
	if len(keys) > 8 {
		keys = keys[:8]
		for localKey := range localProducers {
			if _, ok := seriesMap[localKey]; ok && !stringIn(localKey, keys) {
				keys[len(keys)-1] = localKey
				break
			}
		}
	}
	colors := make(map[string]string, len(keys))
	out := make([]series, 0, len(keys))
	for _, key := range keys {
		points := seriesMap[key]
		if len(points) > defaultRedisHistoryLimit {
			points = points[len(points)-defaultRedisHistoryLimit:]
		}
		points = smoothChartPoints(points, trendSmoothingWindow)
		color := stableProducerColor(key)
		colors[key] = color
		out = append(out, series{Label: first(labels[key], shortAddress(key), key), Color: color, Local: localProducers[key], Points: points})
	}
	b, _ := json.Marshal(out)
	return string(b), colors, localProducers
}
