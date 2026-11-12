package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultTrendRange = "1h"
const trendSmoothingWindow = 3
const trendLiveSampleSource = "chain-dag-live-sample"

var trendResolutionSeconds = []int{10, 30, 60, 300, 900, 3600}

type trendRangeConfig struct {
	Token         string
	Label         string
	Hours         int
	BucketSeconds int
}

type trendBucket struct {
	StartEpoch      int64
	UpdatedEpoch    int64
	BucketSeconds   int
	RequestedBlocks int
	FetchedBlocks   int
	Counts          map[string]float64
	Local           map[string]bool
	BestEffort      bool
	WindowComplete  bool
	Status          string
	Source          string
}

func trendRangeConfigFor(token string) trendRangeConfig {
	switch normalizeTrendRange(token) {
	case "4h":
		return trendRangeConfig{Token: "4h", Label: "4h", Hours: 4, BucketSeconds: 30}
	case "12h":
		return trendRangeConfig{Token: "12h", Label: "12h", Hours: 12, BucketSeconds: 60}
	case "24h":
		return trendRangeConfig{Token: "24h", Label: "24h", Hours: 24, BucketSeconds: 60}
	case "3d":
		return trendRangeConfig{Token: "3d", Label: "3d", Hours: 72, BucketSeconds: 300}
	case "7d":
		return trendRangeConfig{Token: "7d", Label: "7d", Hours: 168, BucketSeconds: 900}
	case "1month":
		return trendRangeConfig{Token: "1month", Label: "1 month", Hours: 720, BucketSeconds: 3600}
	default:
		return trendRangeConfig{Token: "1h", Label: "1h", Hours: 1, BucketSeconds: 10}
	}
}

func normalizeTrendRange(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	token = strings.ReplaceAll(token, " ", "")
	switch token {
	case "", "1", "1hr", "1hour", "1h":
		return "1h"
	case "4", "4hr", "4hour", "4hours", "4h":
		return "4h"
	case "12", "12hr", "12hour", "12hours", "12h":
		return "12h"
	case "24", "24hr", "24hour", "24hours", "1d", "24h":
		return "24h"
	case "3", "3day", "3days", "72h", "3d":
		return "3d"
	case "7", "7day", "7days", "168h", "week", "1week", "7d":
		return "7d"
	case "30d", "30day", "30days", "month", "1month", "720h":
		return "1month"
	default:
		return "1h"
	}
}

func (r *DashboardRuntime) trendChartJSON(ctx context.Context, token string, localProducers map[string]bool) (string, map[string]any, error) {
	cfg := trendRangeConfigFor(token)
	state, err := r.trendState(ctx)
	if err != nil {
		return "", trendMeta(cfg, nil, nil), err
	}
	if state == nil {
		state = map[string]any{}
	}
	if price, err := r.pricePayload(ctx); err == nil && price != nil {
		if usd, ok := priceUSDFromSnapshot(price); ok {
			state["price_usd"] = usd
			state["price_source"] = stringValue(price, "source")
			state["price_updated_at"] = stringValue(price, "updated_at")
		}
	}
	currentBuckets, _ := r.currentGlobalTrendBuckets(ctx, cfg)
	endEpoch := int64Value(state["last_indexed_timestamp_epoch"])
	if endEpoch <= 0 {
		endEpoch = time.Now().UTC().Unix()
	}
	for _, currentBucket := range currentBuckets {
		if currentBucket.StartEpoch > endEpoch {
			endEpoch = currentBucket.StartEpoch
		}
	}
	startEpoch := endEpoch - int64(cfg.Hours*3600)
	members, err := r.client.ZRangeByScore(ctx, trendIndexKey(cfg.BucketSeconds), &redis.ZRangeBy{
		Min: strconv.FormatInt(startEpoch, 10),
		Max: strconv.FormatInt(endEpoch, 10),
	}).Result()
	if err != nil {
		return "", trendMeta(cfg, state, nil), err
	}
	buckets := make([]trendBucket, 0, len(members))
	for _, member := range members {
		fields, err := r.client.HGetAll(ctx, trendBucketKey(cfg.BucketSeconds, member)).Result()
		if err != nil || len(fields) == 0 {
			continue
		}
		bucket := trendBucket{
			StartEpoch:    int64FromString(first(fields["bucket_start"], member)),
			BucketSeconds: intFromString(first(fields["bucket_seconds"], strconv.Itoa(cfg.BucketSeconds))),
			Counts:        map[string]float64{},
			Local:         map[string]bool{},
		}
		bucket.UpdatedEpoch = int64FromString(fields["updated_at_epoch"])
		bucket.RequestedBlocks = intFromString(fields["requested_blocks"])
		bucket.FetchedBlocks = intFromString(fields["fetched_blocks"])
		bucket.BestEffort = truthyString(fields["best_effort"])
		bucket.WindowComplete = truthyString(fields["window_complete"])
		bucket.Status = lower(fields["status"])
		bucket.Source = lower(fields["source"])
		if bucket.StartEpoch <= 0 {
			continue
		}
		for key, value := range fields {
			if strings.HasPrefix(key, "producer:") {
				producer := strings.TrimPrefix(key, "producer:")
				if producer != "" {
					bucket.Counts[producer] = number(value)
				}
			}
			if strings.HasPrefix(key, "local:") && truthyString(value) {
				producer := strings.TrimPrefix(key, "local:")
				if producer != "" {
					bucket.Local[producer] = true
				}
			}
		}
		buckets = append(buckets, bucket)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].StartEpoch < buckets[j].StartEpoch })
	if cfg.Token == "1h" {
		buckets = continuousTrendSuffix(buckets, cfg.BucketSeconds)
	}
	if liveBuckets, err := r.globalChartHistoryTrendBuckets(ctx, cfg, startEpoch, endEpoch); err == nil && len(liveBuckets) > 0 {
		buckets = mergeTrendBuckets(buckets, liveBuckets...)
	}
	if len(currentBuckets) > 0 {
		buckets = mergeTrendBuckets(buckets, currentBuckets...)
	}
	chart, meta := trendSeriesJSON(cfg, buckets, state, localProducers)
	return chart, meta, nil
}

func (r *DashboardRuntime) globalChartHistoryTrendBuckets(ctx context.Context, cfg trendRangeConfig, startEpoch, endEpoch int64) ([]trendBucket, error) {
	rawPoints, err := r.client.LRange(ctx, "bdag:global:chart_history", -defaultRedisHistoryLimit, -1).Result()
	if errorsIsRedisNil(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	points := make([]map[string]any, 0, len(rawPoints))
	for _, raw := range rawPoints {
		var point map[string]any
		if err := json.Unmarshal([]byte(raw), &point); err != nil {
			continue
		}
		points = append(points, point)
	}
	return trendBucketsFromGlobalChartHistory(points, cfg, startEpoch, endEpoch), nil
}

func trendBucketsFromGlobalChartHistory(points []map[string]any, cfg trendRangeConfig, startEpoch, endEpoch int64) []trendBucket {
	byStart := map[int64]trendBucket{}
	for _, point := range points {
		t := first(mapString(point, "t"), mapString(point, "updated_at"))
		if t == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			continue
		}
		epoch := parsed.UTC().Unix()
		if epoch < startEpoch || epoch > endEpoch+int64(cfg.BucketSeconds) {
			continue
		}
		bucketStart := epoch
		if cfg.BucketSeconds > 0 {
			bucketStart = epoch - epoch%int64(cfg.BucketSeconds)
		}
		windowSeconds := int(int64Value(point["window_seconds"]))
		if windowSeconds <= 0 {
			if hours := number(mapString(point, "scan_window_hours")); hours > 0 {
				windowSeconds = int(hours * 3600)
			}
		}
		if windowSeconds <= 0 {
			windowSeconds = cfg.BucketSeconds
		}
		counts := numberMap(point["counts"])
		if len(counts) == 0 {
			continue
		}
		local := boolMap(point["local_pools"])
		bucket := trendBucket{
			StartEpoch:    bucketStart,
			BucketSeconds: windowSeconds,
			Counts:        map[string]float64{},
			Local:         map[string]bool{},
		}
		for producer, count := range counts {
			producer = strings.ToLower(strings.TrimSpace(producer))
			if producer == "" || count <= 0 {
				continue
			}
			bucket.Counts[producer] = count
			if local[producer] {
				bucket.Local[producer] = true
			}
		}
		if len(bucket.Counts) > 0 {
			byStart[bucketStart] = bucket
		}
	}
	out := make([]trendBucket, 0, len(byStart))
	for _, bucket := range byStart {
		out = append(out, bucket)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartEpoch < out[j].StartEpoch })
	return out
}

func (r *DashboardRuntime) currentGlobalTrendBucket(ctx context.Context, cfg trendRangeConfig) (*trendBucket, error) {
	now := time.Now().UTC()
	return r.currentNativeDAGTrendBucket(ctx, cfg, now)
}

func (r *DashboardRuntime) currentGlobalTrendBuckets(ctx context.Context, cfg trendRangeConfig) ([]trendBucket, error) {
	now := time.Now().UTC()
	return r.currentNativeDAGTrendBuckets(ctx, cfg, now)
}

func (r *DashboardRuntime) currentNativeDAGTrendBucket(ctx context.Context, cfg trendRangeConfig, now time.Time) (*trendBucket, error) {
	raw, err := r.client.Get(ctx, nativeDAGLiveSnapshotKey).Result()
	if errorsIsRedisNil(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return trendBucketFromNativeDAGPayload(payload, cfg, now), nil
}

func (r *DashboardRuntime) currentNativeDAGTrendBuckets(ctx context.Context, cfg trendRangeConfig, now time.Time) ([]trendBucket, error) {
	raw, err := r.client.Get(ctx, nativeDAGLiveSnapshotKey).Result()
	if errorsIsRedisNil(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return trendBucketsFromNativeDAGPayload(payload, cfg, now), nil
}

func trendBucketFromGlobalSnapshot(payload map[string]any, cfg trendRangeConfig, now time.Time) *trendBucket {
	if !globalSnapshotUsableForBestEffortTrend(payload, now) {
		return nil
	}
	updatedEpoch := int64Value(payload["updated_at_epoch"])
	if updatedEpoch <= 0 {
		if parsed, err := time.Parse(time.RFC3339, mapString(payload, "updated_at")); err == nil {
			updatedEpoch = parsed.Unix()
		}
	}
	if updatedEpoch <= 0 || now.Sub(time.Unix(updatedEpoch, 0).UTC()) > 2*time.Minute {
		return nil
	}
	windowSeconds := int(int64Value(payload["scan_window_seconds"]))
	if windowSeconds <= 0 {
		if hours := number(mapString(payload, "scan_window_hours")); hours > 0 {
			windowSeconds = int(hours * 3600)
		}
	}
	if windowSeconds <= 0 {
		windowSeconds = cfg.BucketSeconds
	}
	startEpoch := updatedEpoch
	if cfg.BucketSeconds > 0 {
		startEpoch = updatedEpoch - updatedEpoch%int64(cfg.BucketSeconds)
	}
	bucket := &trendBucket{
		StartEpoch:      startEpoch,
		UpdatedEpoch:    updatedEpoch,
		BucketSeconds:   windowSeconds,
		RequestedBlocks: globalRequestedBlocks(payload),
		FetchedBlocks:   int(number(first(stringValue(payload, "fetched_blocks"), stringValue(payload, "window")))),
		Counts:          map[string]float64{},
		Local:           map[string]bool{},
		BestEffort:      !globalSnapshotUsableForCurrentTrend(payload),
		WindowComplete:  globalWindowComplete(payload),
		Status:          lower(first(stringValue(payload, "status"), "ok")),
	}
	for _, row := range producerRows(payload) {
		producer := strings.ToLower(producerKey(row))
		if producer == "" {
			continue
		}
		blocks := number(mapString(row, "blocks"))
		if blocks <= 0 {
			continue
		}
		bucket.Counts[producer] = blocks
		if boolValue(row, "local_pool") {
			bucket.Local[producer] = true
		}
	}
	if len(bucket.Counts) == 0 {
		return nil
	}
	return bucket
}

func trendBucketFromNativeDAGPayload(payload map[string]any, cfg trendRangeConfig, now time.Time) *trendBucket {
	global := nativeDAGGlobalPayload(payload, nil)
	if global == nil {
		return nil
	}
	bucket := trendBucketFromGlobalSnapshot(global, cfg, now)
	if bucket == nil {
		return nil
	}
	bucket.BestEffort = true
	bucket.Status = "chain-dag-live"
	bucket.Source = "chain-dag-live"
	return bucket
}

func trendBucketsFromNativeDAGPayload(payload map[string]any, cfg trendRangeConfig, now time.Time) []trendBucket {
	base := trendBucketFromNativeDAGPayload(payload, cfg, now)
	if base == nil {
		return nil
	}
	nodes := listMaps(payload["nodes"])
	if len(nodes) == 0 {
		return []trendBucket{*base}
	}
	windowStart := now.Add(-time.Duration(cfg.Hours) * time.Hour)
	byStart := map[int64]trendBucket{}
	for _, node := range nodes {
		producer := lower(mapString(node, "producer"))
		if producer == "" || boolValue(node, "producer_pending") {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, mapString(node, "timestamp"))
		if err != nil {
			continue
		}
		parsed = parsed.UTC()
		if parsed.Before(windowStart) || parsed.After(now.Add(time.Minute)) {
			continue
		}
		start := parsed.Unix()
		if cfg.BucketSeconds > 0 {
			start -= start % int64(cfg.BucketSeconds)
		}
		bucket := byStart[start]
		if bucket.Counts == nil {
			bucket = trendBucket{
				StartEpoch:      start,
				UpdatedEpoch:    base.UpdatedEpoch,
				BucketSeconds:   cfg.BucketSeconds,
				RequestedBlocks: base.RequestedBlocks,
				FetchedBlocks:   base.FetchedBlocks,
				Counts:          map[string]float64{},
				Local:           map[string]bool{},
				BestEffort:      true,
				WindowComplete:  base.WindowComplete,
				Status:          "chain-dag-live",
				Source:          "chain-dag-live",
			}
		}
		bucket.Counts[producer]++
		if boolValue(node, "local_pool") || base.Local[producer] {
			bucket.Local[producer] = true
		}
		byStart[start] = bucket
	}
	if len(byStart) == 0 {
		return []trendBucket{*base}
	}
	buckets := make([]trendBucket, 0, len(byStart))
	for _, bucket := range byStart {
		buckets = append(buckets, bucket)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].StartEpoch < buckets[j].StartEpoch })
	return buckets
}

func (r *DashboardRuntime) storeTrendBucket(ctx context.Context, stateKey string, bucket trendBucket, resolutionSeconds int, source string) error {
	if !r.Ready() {
		return errors.New("redis runtime unavailable")
	}
	if resolutionSeconds <= 0 {
		resolutionSeconds = trendRangeConfigFor(defaultTrendRange).BucketSeconds
	}
	if stateKey == "" {
		stateKey = trendCurrentStateKey
	}
	source = lower(first(source, bucket.Source, "live-sample"))
	start := bucket.StartEpoch
	if start <= 0 {
		return nil
	}
	start -= start % int64(resolutionSeconds)
	if len(bucket.Counts) == 0 {
		return nil
	}
	bucket.StartEpoch = start
	if bucket.BucketSeconds <= 0 {
		bucket.BucketSeconds = resolutionSeconds
	}
	if bucket.UpdatedEpoch <= 0 {
		bucket.UpdatedEpoch = time.Now().UTC().Unix()
	}
	if bucket.Status == "" {
		bucket.Status = source
	}
	fields := map[string]any{
		"bucket_start":           strconv.FormatInt(bucket.StartEpoch, 10),
		"bucket_seconds":         strconv.Itoa(bucket.BucketSeconds),
		"updated_at":             time.Unix(bucket.UpdatedEpoch, 0).UTC().Format(time.RFC3339),
		"updated_at_epoch":       strconv.FormatInt(bucket.UpdatedEpoch, 10),
		"requested_blocks":       strconv.Itoa(bucket.RequestedBlocks),
		"fetched_blocks":         strconv.Itoa(bucket.FetchedBlocks),
		"best_effort":            strconv.FormatBool(bucket.BestEffort),
		"window_complete":        strconv.FormatBool(bucket.WindowComplete),
		"status":                 lower(bucket.Status),
		"source":                 source,
		"live_sample_resolution": strconv.Itoa(resolutionSeconds),
	}
	producerFields := 0
	for producer, count := range bucket.Counts {
		producer = lower(producer)
		if producer == "" || count <= 0 {
			continue
		}
		producerFields++
		fields["producer:"+producer] = strconv.FormatFloat(count, 'f', -1, 64)
		if bucket.Local[producer] {
			fields["local:"+producer] = "1"
		}
	}
	if producerFields == 0 {
		return nil
	}
	member := strconv.FormatInt(bucket.StartEpoch, 10)
	state, _ := r.trendState(ctx)
	if state == nil {
		state = map[string]any{}
	}
	state["status"] = "ok"
	state["backfill_status"] = source
	state["source"] = source
	state["resolutions"] = trendResolutionSeconds
	state["updated_at"] = time.Unix(bucket.UpdatedEpoch, 0).UTC().Format(time.RFC3339)
	state["updated_at_epoch"] = bucket.UpdatedEpoch
	state["last_indexed_timestamp_epoch"] = bucket.StartEpoch
	state["last_indexed_at"] = time.Unix(bucket.StartEpoch, 0).UTC().Format(time.RFC3339)
	if raw, err := json.Marshal(state); err == nil {
		pipe := r.client.Pipeline()
		pipe.HSet(ctx, trendBucketKey(resolutionSeconds, member), fields)
		pipe.ZAdd(ctx, trendIndexKey(resolutionSeconds), redis.Z{Score: float64(bucket.StartEpoch), Member: member})
		pipe.Set(ctx, stateKey, raw, 0)
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	return r.trimTrendBuckets(ctx, resolutionSeconds, bucket.StartEpoch)
}

func (r *DashboardRuntime) trimTrendBuckets(ctx context.Context, resolutionSeconds int, latestEpoch int64) error {
	retention := trendRetentionSeconds(resolutionSeconds)
	if retention <= 0 || latestEpoch <= 0 {
		return nil
	}
	cutoff := latestEpoch - retention
	indexKey := trendIndexKey(resolutionSeconds)
	old, err := r.client.ZRangeByScore(ctx, indexKey, &redis.ZRangeBy{
		Min: "-inf",
		Max: strconv.FormatInt(cutoff, 10),
	}).Result()
	if err != nil {
		return err
	}
	if len(old) == 0 {
		return nil
	}
	pipe := r.client.Pipeline()
	for _, member := range old {
		pipe.Del(ctx, trendBucketKey(resolutionSeconds, member))
	}
	pipe.ZRemRangeByScore(ctx, indexKey, "-inf", strconv.FormatInt(cutoff, 10))
	_, err = pipe.Exec(ctx)
	return err
}

func trendRetentionSeconds(resolutionSeconds int) int64 {
	switch resolutionSeconds {
	case 10:
		return 2 * 3600
	case 30:
		return 6 * 3600
	case 60:
		return 30 * 3600
	case 300:
		return 4 * 24 * 3600
	case 900:
		return 8 * 24 * 3600
	case 3600:
		return 35 * 24 * 3600
	default:
		return 0
	}
}

func mergeTrendBuckets(buckets []trendBucket, additions ...trendBucket) []trendBucket {
	out := make([]trendBucket, 0, len(buckets)+len(additions))
	for _, bucket := range buckets {
		out = append(out, bucket)
	}
	for _, addition := range additions {
		replaced := false
		for i := range out {
			if out[i].StartEpoch == addition.StartEpoch {
				out[i] = addition
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, addition)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartEpoch < out[j].StartEpoch })
	return out
}

func (r *DashboardRuntime) trendState(ctx context.Context) (map[string]any, error) {
	raw, err := r.client.Get(ctx, "bdag:trend:v2:state").Result()
	if errorsIsRedisNil(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func trendSeriesJSON(cfg trendRangeConfig, buckets []trendBucket, state map[string]any, localProducers map[string]bool) (string, map[string]any) {
	type series struct {
		Label  string       `json:"label"`
		Color  string       `json:"color"`
		Local  bool         `json:"local"`
		Points []chartPoint `json:"points"`
	}
	totals := map[string]float64{}
	local := map[string]bool{}
	for _, bucket := range buckets {
		for producer, count := range bucket.Counts {
			totals[producer] += count
			if bucket.Local[producer] || localProducers[producer] {
				local[producer] = true
			}
		}
	}
	keys := make([]string, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if totals[keys[i]] == totals[keys[j]] {
			return keys[i] < keys[j]
		}
		return totals[keys[i]] > totals[keys[j]]
	})
	if len(keys) > 8 {
		keys = keys[:8]
		for producer := range localProducers {
			if totals[producer] > 0 && !stringIn(producer, keys) {
				keys[len(keys)-1] = producer
				break
			}
		}
	}
	selected := map[string]bool{}
	for _, key := range keys {
		selected[key] = true
	}
	out := make([]series, 0, len(keys))
	byProducer := map[string][]chartPoint{}
	rewardPerBlock := trendRewardPerBlockBDAG()
	priceUSD := trendPriceUSDFromState(state)
	for _, bucket := range buckets {
		bucketSeconds := bucket.BucketSeconds
		if bucketSeconds <= 0 {
			bucketSeconds = cfg.BucketSeconds
		}
		windowHours := float64(bucketSeconds) / 3600.0
		if windowHours <= 0 {
			windowHours = 1
		}
		t := time.Unix(bucket.StartEpoch, 0).UTC().Format(time.RFC3339)
		coverage := 0.0
		if bucket.RequestedBlocks > 0 && bucket.FetchedBlocks > 0 {
			coverage = math.Min(1, float64(bucket.FetchedBlocks)/float64(bucket.RequestedBlocks))
		}
		for producer, count := range bucket.Counts {
			if !selected[producer] || count <= 0 {
				continue
			}
			blocks := count / windowHours
			if blocks <= 0 || math.IsNaN(blocks) || math.IsInf(blocks, 0) {
				continue
			}
			point := chartPoint{
				T:               t,
				Blocks:          blocks,
				BDAG:            blocks * rewardPerBlock,
				USD:             blocks * rewardPerBlock * priceUSD,
				Coverage:        coverage,
				BestEffort:      bucket.BestEffort,
				RequestedBlocks: bucket.RequestedBlocks,
				FetchedBlocks:   bucket.FetchedBlocks,
				WindowComplete:  bucket.WindowComplete,
				Status:          bucket.Status,
			}
			byProducer[producer] = append(byProducer[producer], point)
		}
	}
	for _, key := range keys {
		points := fillChartPointGaps(byProducer[key], cfg.BucketSeconds)
		out = append(out, series{
			Label:  trendProducerLabel(key, local[key] || localProducers[key]),
			Color:  stableProducerColor(key),
			Local:  local[key] || localProducers[key],
			Points: smoothChartPoints(points, trendSmoothingWindow),
		})
	}
	raw, _ := json.Marshal(out)
	return string(raw), trendMeta(cfg, state, buckets)
}

func fillChartPointGaps(points []chartPoint, bucketSeconds int) []chartPoint {
	if len(points) == 1 {
		return seedSingleChartPoint(points, bucketSeconds)
	}
	if len(points) < 2 {
		return points
	}
	if bucketSeconds <= 0 {
		bucketSeconds = 60
	}
	sort.SliceStable(points, func(i, j int) bool { return points[i].T < points[j].T })
	out := make([]chartPoint, 0, len(points))
	maxInsertedPerGap := 120
	step := time.Duration(bucketSeconds) * time.Second
	for i, point := range points {
		if i == 0 {
			out = append(out, point)
			continue
		}
		prev := out[len(out)-1]
		prevT, prevErr := time.Parse(time.RFC3339, prev.T)
		nextT, nextErr := time.Parse(time.RFC3339, point.T)
		if prevErr == nil && nextErr == nil && nextT.After(prevT.Add(step+step/2)) {
			inserted := 0
			for fillT := prevT.Add(step); fillT.Before(nextT) && inserted < maxInsertedPerGap; fillT = fillT.Add(step) {
				fill := prev
				fill.T = fillT.UTC().Format(time.RFC3339)
				fill.BestEffort = true
				if fill.Status == "" {
					fill.Status = "lazy-fill"
				}
				out = append(out, fill)
				inserted++
			}
		}
		out = append(out, point)
	}
	if len(out) > 1600 {
		return out[len(out)-1600:]
	}
	return out
}

func seedSingleChartPoint(points []chartPoint, bucketSeconds int) []chartPoint {
	if len(points) != 1 {
		return points
	}
	if bucketSeconds <= 0 {
		bucketSeconds = 60
	}
	parsed, err := time.Parse(time.RFC3339, points[0].T)
	if err != nil {
		return points
	}
	seed := points[0]
	seed.T = parsed.Add(-time.Duration(bucketSeconds) * time.Second).UTC().Format(time.RFC3339)
	seed.BestEffort = true
	if seed.Status == "" {
		seed.Status = "live-seed"
	}
	return []chartPoint{seed, points[0]}
}

func trendPriceUSDFromState(state map[string]any) float64 {
	if value, ok := parseNumber(mapString(state, "price_usd")); ok && value > 0 {
		return value
	}
	if value, ok := configuredBDAGPriceUSD(); ok {
		return value
	}
	return 0
}

func smoothChartPoints(points []chartPoint, window int) []chartPoint {
	if window <= 1 || len(points) < 2 {
		return points
	}
	out := make([]chartPoint, len(points))
	for i := range points {
		start := i - window + 1
		if start < 0 {
			start = 0
		}
		var blocks, bdag, usd float64
		var count float64
		for j := start; j <= i; j++ {
			blocks += points[j].Blocks
			bdag += points[j].BDAG
			usd += points[j].USD
			count++
		}
		out[i] = points[i]
		out[i].Blocks = blocks / count
		out[i].BDAG = bdag / count
		out[i].USD = usd / count
	}
	return out
}

func continuousTrendSuffix(buckets []trendBucket, bucketSeconds int) []trendBucket {
	if len(buckets) < 2 {
		return buckets
	}
	if bucketSeconds <= 0 {
		bucketSeconds = 1
	}
	start := 0
	maxGap := int64(bucketSeconds * 2)
	for i := 1; i < len(buckets); i++ {
		if buckets[i].StartEpoch-buckets[i-1].StartEpoch > maxGap {
			start = i
		}
	}
	return buckets[start:]
}

func trendMeta(cfg trendRangeConfig, state map[string]any, buckets []trendBucket) map[string]any {
	meta := map[string]any{
		"range":          cfg.Token,
		"range_label":    cfg.Label,
		"hours":          cfg.Hours,
		"bucket_seconds": cfg.BucketSeconds,
		"bucket_count":   len(buckets),
	}
	if len(buckets) > 0 {
		first := buckets[0].StartEpoch
		last := buckets[len(buckets)-1].StartEpoch
		latest := buckets[len(buckets)-1]
		meta["coverage_hours"] = math.Max(0, float64(last-first)/3600.0)
		meta["first_bucket_at"] = time.Unix(first, 0).UTC().Format(time.RFC3339)
		meta["last_bucket_at"] = time.Unix(last, 0).UTC().Format(time.RFC3339)
		meta["live_best_effort"] = latest.BestEffort
		meta["live_window_complete"] = latest.WindowComplete
		meta["live_requested_blocks"] = latest.RequestedBlocks
		meta["live_fetched_blocks"] = latest.FetchedBlocks
		if latest.RequestedBlocks > 0 && latest.FetchedBlocks > 0 {
			meta["live_coverage"] = math.Min(1, float64(latest.FetchedBlocks)/float64(latest.RequestedBlocks))
		}
		if latest.Source != "" {
			meta["live_source"] = latest.Source
		}
	} else {
		meta["coverage_hours"] = 0
	}
	if state != nil {
		meta["last_indexed_height"] = stringValue(state, "last_indexed_height")
		meta["last_indexed_at"] = stringValue(state, "last_indexed_at")
		meta["backfill_status"] = stringValue(state, "backfill_status")
	}
	return meta
}

func trendProducerLabel(producer string, local bool) string {
	if local {
		return "Local pool (" + shortAddress(producer) + ")"
	}
	return shortAddress(producer)
}

func trendIndexKey(seconds int) string {
	return "bdag:trend:v2:index:" + strconv.Itoa(seconds)
}

func trendBucketKey(seconds int, bucketStart string) string {
	return "bdag:trend:v2:bucket:" + strconv.Itoa(seconds) + ":" + bucketStart
}

func truthyString(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func intFromString(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func int64FromString(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}

func errorsIsRedisNil(err error) bool {
	return err == redis.Nil
}
