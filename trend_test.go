package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTrendRangeConfigUsesBoundedBucketResolutions(t *testing.T) {
	tests := []struct {
		token         string
		wantToken     string
		wantHours     int
		wantBucketSec int
	}{
		{"1h", "1h", 1, 10},
		{"4h", "4h", 4, 30},
		{"12h", "12h", 12, 60},
		{"24h", "24h", 24, 60},
		{"3d", "3d", 72, 300},
		{"7d", "7d", 168, 900},
		{"1month", "1month", 720, 3600},
		{"unknown", "1h", 1, 10},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			got := trendRangeConfigFor(tt.token)
			if got.Token != tt.wantToken || got.Hours != tt.wantHours || got.BucketSeconds != tt.wantBucketSec {
				t.Fatalf("trendRangeConfigFor(%q) = %#v", tt.token, got)
			}
		})
	}
}

func TestTrendSeriesJSONBuildsProducerSeriesFromBuckets(t *testing.T) {
	t.Setenv("BDAG_REWARD_PER_BLOCK", "260")
	t.Setenv("BDAG_PRICE_USD", "0.01")
	cfg := trendRangeConfigFor("1h")
	localProducer := "0xabc0000000000000000000000000000000000001"
	chart, meta := trendSeriesJSON(cfg, []trendBucket{
		{
			StartEpoch:    1760000000,
			BucketSeconds: 10,
			Counts: map[string]float64{
				localProducer: 2,
			},
			Local: map[string]bool{localProducer: true},
		},
		{
			StartEpoch:    1760000010,
			BucketSeconds: 10,
			Counts: map[string]float64{
				localProducer: 1,
			},
			Local: map[string]bool{localProducer: true},
		},
	}, map[string]any{"last_indexed_height": float64(123)}, map[string]bool{localProducer: true})

	var series []struct {
		Label  string       `json:"label"`
		Local  bool         `json:"local"`
		Points []chartPoint `json:"points"`
	}
	if err := json.Unmarshal([]byte(chart), &series); err != nil {
		t.Fatalf("trend chart json did not unmarshal: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("expected one producer series, got %d: %s", len(series), chart)
	}
	if !series[0].Local || !strings.Contains(series[0].Label, "Local pool") {
		t.Fatalf("expected local producer label, got %#v", series[0])
	}
	if got := len(series[0].Points); got != 2 {
		t.Fatalf("expected two trend points, got %d: %s", got, chart)
	}
	if got := series[0].Points[0].Blocks; got != 720 {
		t.Fatalf("first point blocks/hour = %v, want 720", got)
	}
	if got := series[0].Points[0].BDAG; got != 187200 {
		t.Fatalf("first point BDAG/hour = %v, want 187200", got)
	}
	if got := series[0].Points[0].USD; got != 1872 {
		t.Fatalf("first point USD/hour = %v, want 1872", got)
	}
	if got := meta["bucket_seconds"]; got != 10 {
		t.Fatalf("trend meta bucket_seconds = %#v, want 10", got)
	}
}

func TestFillChartPointGapsAddsBestEffortContinuityPoints(t *testing.T) {
	points := []chartPoint{
		{T: "2026-06-23T14:00:00Z", Blocks: 10, BDAG: 2600, USD: 26},
		{T: "2026-06-23T14:00:30Z", Blocks: 20, BDAG: 5200, USD: 52},
	}
	got := fillChartPointGaps(points, 10)
	if len(got) != 4 {
		t.Fatalf("filled points = %d, want 4: %#v", len(got), got)
	}
	if got[1].T != "2026-06-23T14:00:10Z" || !got[1].BestEffort || got[1].Status != "lazy-fill" {
		t.Fatalf("first fill point = %#v", got[1])
	}
	if got[2].T != "2026-06-23T14:00:20Z" || !got[2].BestEffort {
		t.Fatalf("second fill point = %#v", got[2])
	}
}

func TestFillChartPointGapsSeedsSingleLivePoint(t *testing.T) {
	points := []chartPoint{{T: "2026-06-23T14:00:30Z", Blocks: 20, BDAG: 5200, USD: 52}}
	got := fillChartPointGaps(points, 10)
	if len(got) != 2 {
		t.Fatalf("single live point fill = %d points, want 2: %#v", len(got), got)
	}
	if got[0].T != "2026-06-23T14:00:20Z" || !got[0].BestEffort || got[0].Status != "live-seed" {
		t.Fatalf("seed point = %#v", got[0])
	}
	if got[1].T != points[0].T || got[1].Blocks != points[0].Blocks {
		t.Fatalf("current point changed: %#v", got[1])
	}
}

func TestSmoothChartPointsUsesTrailingThreeReadings(t *testing.T) {
	points := []chartPoint{
		{T: "1", Blocks: 3, BDAG: 30, USD: 300},
		{T: "2", Blocks: 6, BDAG: 60, USD: 600},
		{T: "3", Blocks: 12, BDAG: 120, USD: 1200},
		{T: "4", Blocks: 15, BDAG: 150, USD: 1500},
	}
	got := smoothChartPoints(points, 3)
	expectedBlocks := []float64{3, 4.5, 7, 11}
	expectedBDAG := []float64{30, 45, 70, 110}
	expectedUSD := []float64{300, 450, 700, 1100}
	for i := range got {
		if got[i].T != points[i].T {
			t.Fatalf("point %d timestamp = %q, want %q", i, got[i].T, points[i].T)
		}
		if got[i].Blocks != expectedBlocks[i] || got[i].BDAG != expectedBDAG[i] || got[i].USD != expectedUSD[i] {
			t.Fatalf("point %d = %#v, want blocks=%v bdag=%v usd=%v", i, got[i], expectedBlocks[i], expectedBDAG[i], expectedUSD[i])
		}
	}
}

func TestTrendBucketFromGlobalSnapshotUsesFreshCurrentWindow(t *testing.T) {
	now := time.Unix(1_781_887_200, 0).UTC()
	cfg := trendRangeConfigFor("1h")
	payload := map[string]any{
		"updated_at_epoch":    float64(now.Unix()),
		"scan_window_seconds": float64(600),
		"requested_blocks":    float64(600),
		"fetched_blocks":      float64(600),
		"status":              "ok",
		"chain_clusters": []any{
			map[string]any{
				"address":    "0xabc0000000000000000000000000000000000001",
				"blocks":     float64(60),
				"local_pool": true,
			},
		},
	}
	bucket := trendBucketFromGlobalSnapshot(payload, cfg, now)
	if bucket == nil {
		t.Fatal("expected fresh global snapshot to produce a trend bucket")
	}
	if bucket.StartEpoch != now.Unix() {
		t.Fatalf("bucket start = %d, want %d", bucket.StartEpoch, now.Unix())
	}
	if bucket.BucketSeconds != 600 {
		t.Fatalf("bucket seconds = %d, want 600", bucket.BucketSeconds)
	}
	if got := bucket.Counts["0xabc0000000000000000000000000000000000001"]; got != 60 {
		t.Fatalf("producer count = %v, want 60", got)
	}
	if !bucket.Local["0xabc0000000000000000000000000000000000001"] {
		t.Fatal("local producer flag was not preserved")
	}
	if bucket.BestEffort {
		t.Fatal("complete canonical snapshot should not be marked best effort")
	}

	payload["updated_at_epoch"] = float64(now.Add(-3 * time.Minute).Unix())
	if stale := trendBucketFromGlobalSnapshot(payload, cfg, now); stale != nil {
		t.Fatalf("stale snapshot produced bucket %#v", stale)
	}
}

func TestTrendBucketFromGlobalSnapshotUsesFreshBestEffortWindow(t *testing.T) {
	now := time.Unix(1_781_887_200, 0).UTC()
	cfg := trendRangeConfigFor("1h")
	payload := map[string]any{
		"updated_at_epoch":    float64(now.Unix()),
		"scan_window_seconds": float64(180),
		"requested_blocks":    float64(600),
		"fetched_blocks":      float64(195),
		"status":              "degraded",
		"partial_scan":        true,
		"window_complete":     false,
		"chain_clusters": []any{
			map[string]any{
				"address": "0xabc0000000000000000000000000000000000001",
				"blocks":  float64(30),
			},
		},
	}
	bucket := trendBucketFromGlobalSnapshot(payload, cfg, now)
	if bucket == nil {
		t.Fatal("expected fresh partial Redis snapshot to produce a best-effort trend bucket")
	}
	if !bucket.BestEffort || bucket.WindowComplete {
		t.Fatalf("bucket best-effort flags = bestEffort:%v complete:%v", bucket.BestEffort, bucket.WindowComplete)
	}
	if bucket.RequestedBlocks != 600 || bucket.FetchedBlocks != 195 {
		t.Fatalf("bucket coverage = %d/%d, want 195/600", bucket.FetchedBlocks, bucket.RequestedBlocks)
	}

	chart, meta := trendSeriesJSON(cfg, []trendBucket{*bucket}, nil, nil)
	var series []struct {
		Points []chartPoint `json:"points"`
	}
	if err := json.Unmarshal([]byte(chart), &series); err != nil {
		t.Fatalf("trend chart json did not unmarshal: %v", err)
	}
	if len(series) != 1 || len(series[0].Points) != 2 {
		t.Fatalf("expected seeded best-effort chart points, got %s", chart)
	}
	point := series[0].Points[1]
	if !point.BestEffort || point.RequestedBlocks != 600 || point.FetchedBlocks != 195 {
		t.Fatalf("best-effort point metadata = %#v", point)
	}
	if got := meta["live_best_effort"]; got != true {
		t.Fatalf("live_best_effort meta = %#v, want true", got)
	}
}

func TestTrendBucketFromNativeDAGPayloadUsesFreshRedisDAGView(t *testing.T) {
	now := time.Unix(1_781_887_200, 0).UTC()
	cfg := trendRangeConfigFor("1h")
	localProducer := "0xd5f8df0a60bc1ff4636250b2a2dff319bf79b1f5"
	payload := map[string]any{
		"updated_at_epoch": float64(now.Unix()),
		"status":           "ok",
		"window": map[string]any{
			"requested": float64(120),
			"available": float64(3),
		},
		"nodes": []any{
			map[string]any{
				"hash":       "0x1",
				"producer":   localProducer,
				"local_pool": true,
				"timestamp":  now.Add(-20 * time.Second).Format(time.RFC3339),
			},
			map[string]any{
				"hash":      "0x2",
				"producer":  localProducer,
				"timestamp": now.Add(-10 * time.Second).Format(time.RFC3339),
			},
			map[string]any{
				"hash":       "0x3",
				"producer":   "0x1719e0ee598c15957448d5e568948101df78e7a0",
				"timestamp":  now.Format(time.RFC3339),
				"local_pool": false,
			},
		},
	}
	bucket := trendBucketFromNativeDAGPayload(payload, cfg, now)
	if bucket == nil {
		t.Fatal("expected native DAG live snapshot to produce a trend bucket")
	}
	if !bucket.BestEffort || bucket.Source != "chain-dag-live" {
		t.Fatalf("native DAG bucket source/effort = %#v", bucket)
	}
	if bucket.RequestedBlocks != defaultGlobalBlockWindow || bucket.FetchedBlocks != 3 {
		t.Fatalf("native DAG bucket coverage = %d/%d, want 3/%d", bucket.FetchedBlocks, bucket.RequestedBlocks, defaultGlobalBlockWindow)
	}
	if got := bucket.Counts[localProducer]; got != 2 {
		t.Fatalf("local producer count = %v, want 2", got)
	}
	if !bucket.Local[localProducer] {
		t.Fatal("local producer flag was not preserved from native DAG payload")
	}
	if bucket.BucketSeconds != 20 {
		t.Fatalf("bucket seconds = %d, want 20 from native timestamps", bucket.BucketSeconds)
	}
}

func TestTrendBucketsFromNativeDAGPayloadUsesNodeTimestamps(t *testing.T) {
	now := time.Unix(1_781_887_260, 0).UTC()
	cfg := trendRangeConfigFor("1h")
	localProducer := "0xd5f8df0a60bc1ff4636250b2a2dff319bf79b1f5"
	remoteProducer := "0x1719e0ee598c15957448d5e568948101df78e7a0"
	payload := map[string]any{
		"updated_at_epoch": float64(now.Unix()),
		"status":           "ok",
		"window": map[string]any{
			"requested": float64(120),
			"available": float64(4),
		},
		"nodes": []any{
			map[string]any{
				"hash":       "0x1",
				"producer":   localProducer,
				"local_pool": true,
				"timestamp":  now.Add(-31 * time.Second).Format(time.RFC3339),
			},
			map[string]any{
				"hash":      "0x2",
				"producer":  localProducer,
				"timestamp": now.Add(-29 * time.Second).Format(time.RFC3339),
			},
			map[string]any{
				"hash":      "0x3",
				"producer":  remoteProducer,
				"timestamp": now.Add(-11 * time.Second).Format(time.RFC3339),
			},
			map[string]any{
				"hash":      "0x4",
				"producer":  remoteProducer,
				"timestamp": now.Add(-1 * time.Second).Format(time.RFC3339),
			},
		},
	}
	buckets := trendBucketsFromNativeDAGPayload(payload, cfg, now)
	if len(buckets) < 3 {
		t.Fatalf("native DAG timestamp buckets = %d, want multiple buckets: %#v", len(buckets), buckets)
	}
	for _, bucket := range buckets {
		if bucket.BucketSeconds != 10 || bucket.Source != "chain-dag-live" || !bucket.BestEffort {
			t.Fatalf("bucket metadata = %#v", bucket)
		}
	}
	if !buckets[0].Local[localProducer] {
		t.Fatalf("local producer flag missing from first bucket: %#v", buckets[0])
	}
}

func TestTrendBucketFromGlobalSnapshotRejectsIrreparableCurrentWindows(t *testing.T) {
	now := time.Unix(1_781_887_200, 0).UTC()
	cfg := trendRangeConfigFor("1h")
	base := func() map[string]any {
		return map[string]any{
			"updated_at_epoch":    float64(now.Unix()),
			"scan_window_seconds": float64(600),
			"requested_blocks":    float64(600),
			"fetched_blocks":      float64(600),
			"status":              "ok",
			"chain_clusters": []any{
				map[string]any{
					"address": "0xabc0000000000000000000000000000000000001",
					"blocks":  float64(60),
				},
			},
		}
	}
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"error", func(p map[string]any) { p["status"] = "error" }},
		{"zero fetch", func(p map[string]any) { p["fetched_blocks"] = float64(0) }},
		{"no producers", func(p map[string]any) { p["chain_clusters"] = []any{} }},
		{"head only", func(p map[string]any) { p["head_only"] = true }},
		{"maintenance deferred", func(p map[string]any) { p["maintenance_deferred"] = true }},
		{"deferred scan", func(p map[string]any) { p["deferred_scan"] = true }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := base()
			tt.mutate(payload)
			if bucket := trendBucketFromGlobalSnapshot(payload, cfg, now); bucket != nil {
				t.Fatalf("non-canonical snapshot produced bucket %#v", bucket)
			}
		})
	}
}

func TestTrendBucketsFromGlobalChartHistoryKeepsLiveSeries(t *testing.T) {
	cfg := trendRangeConfigFor("1h")
	points := []map[string]any{
		{
			"t":              "2026-06-19T16:41:53Z",
			"window_seconds": float64(525),
			"counts": map[string]any{
				"0xabc0000000000000000000000000000000000001": float64(20),
			},
			"local_pools": map[string]any{
				"0xabc0000000000000000000000000000000000001": true,
			},
		},
		{
			"t":              "2026-06-19T16:42:04Z",
			"window_seconds": float64(526),
			"counts": map[string]any{
				"0xabc0000000000000000000000000000000000001": float64(22),
			},
		},
	}
	buckets := trendBucketsFromGlobalChartHistory(points, cfg, 1_781_887_200, 1_781_887_400)
	if len(buckets) != 2 {
		t.Fatalf("buckets = %d, want 2: %#v", len(buckets), buckets)
	}
	if buckets[0].BucketSeconds != 525 || buckets[1].BucketSeconds != 526 {
		t.Fatalf("bucket windows = %d,%d", buckets[0].BucketSeconds, buckets[1].BucketSeconds)
	}
	if got := buckets[1].Counts["0xabc0000000000000000000000000000000000001"]; got != 22 {
		t.Fatalf("latest count = %v, want 22", got)
	}
	if !buckets[0].Local["0xabc0000000000000000000000000000000000001"] {
		t.Fatal("local flag from chart history was not preserved")
	}
}

func TestContinuousTrendSuffixDropsBucketsBeforeLargeGap(t *testing.T) {
	buckets := []trendBucket{
		{StartEpoch: 100, BucketSeconds: 10, Counts: map[string]float64{"a": 1}},
		{StartEpoch: 110, BucketSeconds: 10, Counts: map[string]float64{"a": 1}},
		{StartEpoch: 1000, BucketSeconds: 10, Counts: map[string]float64{"a": 1}},
		{StartEpoch: 1010, BucketSeconds: 10, Counts: map[string]float64{"a": 1}},
	}
	got := continuousTrendSuffix(buckets, 10)
	if len(got) != 2 || got[0].StartEpoch != 1000 {
		t.Fatalf("continuous suffix = %#v, want buckets from latest continuous segment", got)
	}
}

func TestRedisFunctionsApplyPersistentTrendBuckets(t *testing.T) {
	for _, expected := range []string{
		`redis.register_function("bdag_apply_trend_sample"`,
		`bdag:trend:v2:sparse_done`,
		`SISMEMBER", done_key, sample_id`,
		`redis.register_function("bdag_apply_trend_headers"`,
		`bdag:trend:v2:state`,
		`bdag:trend:v2:bucket:`,
		`HINCRBY", bucket, "producer:" .. producer`,
		`state_key = as_string(payload.state_key)`,
		`allow_bootstrap`,
		`check_parent = as_bool(payload.check_parent, false)`,
		`if check_parent and last_height > 0`,
	} {
		if !strings.Contains(bdagRedisFunctionLibrary, expected) {
			t.Fatalf("Redis function library missing trend behavior %q", expected)
		}
	}
}

func TestRedisRuntimeDefaultsPersistTrendHistory(t *testing.T) {
	t.Setenv("BDAG_REDIS_CONFIG", "")
	t.Setenv("BDAG_REDIS_APPENDONLY", "")
	t.Setenv("BDAG_REDIS_APPENDFSYNC", "")
	t.Setenv("BDAG_REDIS_MAXMEMORY", "")
	t.Setenv("BDAG_REDIS_MAXMEMORY_POLICY", "")
	args := strings.Join(redisServerArgs("/tmp/redis.sock", "/tmp/redis-data"), " ")
	for _, expected := range []string{
		"--appendonly yes",
		"--appendfsync everysec",
		"--maxmemory 256mb",
		"--maxmemory-policy noeviction",
	} {
		if !strings.Contains(args, expected) {
			t.Fatalf("redis args missing %q: %s", expected, args)
		}
	}
}
