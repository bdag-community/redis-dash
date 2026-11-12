package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultEVMHTTPURL                    = "http://node:18545"
	defaultPoolMetricsURL                = "http://pool:9090/metrics"
	defaultPoolASICPerfURL               = "http://pool:9090/health/asic-performance"
	defaultPoolJobStateURL               = "http://pool:9090/health/job-state"
	defaultGlobalBlockWindow             = 600
	defaultTrendSettleDelay              = 5 * time.Second
	defaultLatestBackscan                = 16
	defaultLatestTagLagLimit             = 30
	defaultTrendBackfillDays             = 30
	defaultTrendChunkSize                = 128
	defaultTrendRPCWorkers               = 4
	defaultTrendMaxPass                  = 4096
	defaultTrendRecentBlocks             = 3600
	defaultTrendSamplesPass              = 24
	defaultTrendSampleMax                = 2048
	defaultTrendCatchupEvery             = 30 * time.Second
	defaultTrendChunkSleep               = 100 * time.Millisecond
	defaultEVMSyncBackoff                = 60 * time.Second
	defaultNativeDAGPoll                 = 1 * time.Second
	defaultNativeDAGMaxGap               = 240
	defaultNativeDAGMaxPass              = 24
	defaultNativeDAGStallFailures        = 3
	defaultNativeDAGRebuildCooldown      = 5 * time.Minute
	defaultNativeDAGStallRebuildCooldown = 5 * time.Minute
	defaultLowImpactCheck                = 5 * time.Second
	defaultASICARPTablePath              = "/proc/net/arp"
	defaultASICTelemetryRead             = 2 * time.Second
	defaultPricePollInterval             = 5 * time.Minute
	defaultCMCBDAGMarketsURL             = "https://api.coinmarketcap.com/data-api/v3/cryptocurrency/market-pairs/latest?slug=blockdag&start=1&limit=50&category=spot&centerType=all&sort=cmc_rank_advanced"

	trendCurrentStateKey  = "bdag:trend:v2:state"
	trendBackfillStateKey = "bdag:trend:v2:backfill_state"
	trendSparseDoneKey    = "bdag:trend:v2:sparse_done"
	priceSnapshotKey      = "bdag:price:snapshot"
)

type nativeDAGOrderRangeResult struct {
	applied           int
	skipped           int
	skippedOrders     []uint64
	lastAppliedOrder  uint64
	firstFailureOrder uint64
	firstErr          error
	terminalNotReady  bool
}

type liveWorkers struct {
	runtime                    *DashboardRuntime
	httpClient                 *http.Client
	rpcBudget                  *nodeRPCBudget
	evmHTTP                    string
	poolMetrics                string
	poolASICPerformance        string
	poolJobState               string
	priceEnabled               bool
	priceURL                   string
	pricePoll                  time.Duration
	priceTopMarkets            int
	asicTelemetryEnabled       bool
	asicTelemetryTimeout       time.Duration
	windowSize                 int
	stableDelay                time.Duration
	trendEnabled               bool
	trendBackfillDays          int
	trendChunkSize             int
	trendRPCWorkers            int
	trendMaxPassBlocks         int
	trendRecentBlocks          int
	trendSamplesPerPass        int
	trendSampleMaxBlocks       int
	trendCatchupEvery          time.Duration
	trendChunkSleep            time.Duration
	evmSyncBackoff             time.Duration
	nativeDAGEnabled           bool
	nativeDAGRPC               string
	nativeDAGWindow            int
	nativeDAGPoll              time.Duration
	nativeDAGMaxGap            uint64
	nativeDAGMaxPass           uint64
	nativeDAGSettleOrders      uint64
	lowImpact                  lowImpactConfig
	localProducers             map[string]bool
	mu                         sync.Mutex
	evmSyncBackoffUntil        time.Time
	lastEVMSyncBackoffLog      time.Time
	lastTrendErrorLog          time.Time
	lastTrendRebuild           time.Time
	trendRebuildActive         bool
	lastNativeDAGRepair        time.Time
	lastNativeDAGRepairAttempt time.Time
	lastNativeDAGStallRebuild  time.Time
	nativeDAGStalledOrder      uint64
	nativeDAGStalledFailures   int
	lowImpactCachedUntil       time.Time
	lowImpactCached            bool
	lowImpactReason            string
}

type evmRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *evmRPCError    `json:"error"`
}

type evmRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type evmBlockHeader struct {
	Number     string `json:"number"`
	Hash       string `json:"hash"`
	ParentHash string `json:"parentHash"`
	Timestamp  string `json:"timestamp"`
	Miner      string `json:"miner"`
	Author     string `json:"author"`
	Coinbase   string `json:"coinbase"`
}

type chainHeadEvent struct {
	Height           uint64 `json:"height"`
	Hash             string `json:"hash"`
	ParentHash       string `json:"parent_hash"`
	Timestamp        string `json:"timestamp"`
	TimestampEpoch   int64  `json:"timestamp_epoch"`
	Producer         string `json:"producer"`
	RPCSource        string `json:"rpc_source"`
	ReceivedAt       string `json:"received_at"`
	ReceivedAtEpoch  int64  `json:"received_at_epoch"`
	RequestedBlocks  int    `json:"requested_blocks"`
	ChainLatestBlock uint64 `json:"chain_latest_block"`
	LocalPool        bool   `json:"local_pool"`
}

type trendSparseSample struct {
	ID            string
	Tier          string
	Anchor        time.Time
	Window        time.Duration
	Resolutions   []int
	TotalPlanned  int
	ReferenceTime time.Time
	LatestHeight  uint64
}

type lowImpactConfig struct {
	Mode                 string
	LagBlocks            int64
	TrendRecentBlocks    int
	TrendChunkSize       int
	TrendRPCWorkers      int
	TrendSamplesPerPass  int
	TrendSampleMaxBlocks int
	TrendCatchupEvery    time.Duration
	TrendChunkSleep      time.Duration
	NativeDAGPoll        time.Duration
	NativeDAGMaxPass     uint64
	CheckEvery           time.Duration
}

type trendWorkerLimits struct {
	recentBlocks    int
	chunkSize       int
	rpcWorkers      int
	samplesPerPass  int
	sampleMaxBlocks int
	catchupEvery    time.Duration
	chunkSleep      time.Duration
}

func (r *DashboardRuntime) startWorkers(ctx context.Context) {
	if !r.Ready() {
		return
	}
	poolMetricsURL := env("BDAG_POOL_METRICS_URL", defaultPoolMetricsURL)
	workers := &liveWorkers{
		runtime:               r,
		httpClient:            newNodeHTTPClient(8 * time.Second),
		rpcBudget:             dashboardNodeRPCBudget,
		evmHTTP:               env("BDAG_EVM_HTTP_URL", defaultEVMHTTPURL),
		poolMetrics:           poolMetricsURL,
		poolASICPerformance:   env("BDAG_POOL_ASIC_PERFORMANCE_URL", poolASICPerformanceURLFromMetrics(poolMetricsURL)),
		poolJobState:          env("BDAG_POOL_JOB_STATE_URL", poolHealthURLFromMetrics(poolMetricsURL, "/health/job-state", defaultPoolJobStateURL)),
		priceEnabled:          envBool("BDAG_PRICE_INGEST_ENABLED", true),
		priceURL:              env("BDAG_PRICE_CMC_MARKETS_URL", defaultCMCBDAGMarketsURL),
		pricePoll:             durationEnv("BDAG_PRICE_POLL_SECONDS", defaultPricePollInterval),
		priceTopMarkets:       intEnv("BDAG_PRICE_TOP_MARKETS", 3),
		asicTelemetryEnabled:  envBool("BDAG_ASIC_TELEMETRY_ENABLED", true),
		asicTelemetryTimeout:  durationEnv("BDAG_ASIC_TELEMETRY_TIMEOUT_SECONDS", defaultASICTelemetryRead),
		windowSize:            intEnv("BDAG_GLOBAL_BLOCK_WINDOW", defaultGlobalBlockWindow),
		stableDelay:           durationEnv("BDAG_TREND_SETTLE_DELAY_SECONDS", defaultTrendSettleDelay),
		trendEnabled:          envBool("BDAG_TREND_HISTORY_ENABLED", true),
		trendBackfillDays:     intEnv("BDAG_TREND_BACKFILL_DAYS", defaultTrendBackfillDays),
		trendChunkSize:        intEnv("BDAG_TREND_BACKFILL_CHUNK_SIZE", defaultTrendChunkSize),
		trendRPCWorkers:       intEnv("BDAG_TREND_RPC_WORKERS", defaultTrendRPCWorkers),
		trendMaxPassBlocks:    intEnv("BDAG_TREND_BACKFILL_MAX_BLOCKS_PER_PASS", defaultTrendMaxPass),
		trendRecentBlocks:     intEnv("BDAG_TREND_RECENT_WARMUP_BLOCKS", defaultTrendRecentBlocks),
		trendSamplesPerPass:   intEnv("BDAG_TREND_SPARSE_SAMPLES_PER_PASS", defaultTrendSamplesPass),
		trendSampleMaxBlocks:  intEnv("BDAG_TREND_SPARSE_MAX_SAMPLE_BLOCKS", defaultTrendSampleMax),
		trendCatchupEvery:     durationEnv("BDAG_TREND_CATCHUP_INTERVAL_SECONDS", defaultTrendCatchupEvery),
		trendChunkSleep:       durationEnvMillis("BDAG_TREND_BACKFILL_SLEEP_MS", defaultTrendChunkSleep),
		evmSyncBackoff:        durationEnv("BDAG_EVM_SYNC_BACKOFF_SECONDS", defaultEVMSyncBackoff),
		nativeDAGEnabled:      envBool("BDAG_NATIVE_DAG_LIVE_ENABLED", true),
		nativeDAGRPC:          first(env("BDAG_NODE_RPC_URL", ""), env("NODE_RPC_URL", ""), env("BDAG_RPC_URL", ""), defaultNativeRPCURL),
		nativeDAGWindow:       intEnv("BDAG_NATIVE_DAG_LIVE_WINDOW", defaultNativeDAGWindowLimit),
		nativeDAGPoll:         durationEnvMillis("BDAG_NATIVE_DAG_POLL_MS", defaultNativeDAGPoll),
		nativeDAGMaxGap:       uint64(intEnv("BDAG_NATIVE_DAG_MAX_GAP", defaultNativeDAGMaxGap)),
		nativeDAGMaxPass:      uint64(intEnv("BDAG_NATIVE_DAG_MAX_BLOCKS_PER_PASS", defaultNativeDAGMaxPass)),
		nativeDAGSettleOrders: uint64(intEnv("BDAG_NATIVE_DAG_LIVE_SETTLE_ORDERS", 0)),
		lowImpact:             lowImpactConfigFromEnv(),
		localProducers:        localProducerSet(),
	}
	workers.nativeDAGWindow = chainDAGWindowLimit(workers.nativeDAGWindow)
	if workers.nativeDAGPoll <= 0 {
		workers.nativeDAGPoll = defaultNativeDAGPoll
	}
	if workers.nativeDAGMaxGap < 1 {
		workers.nativeDAGMaxGap = defaultNativeDAGMaxGap
	}
	if workers.nativeDAGMaxPass < 1 {
		workers.nativeDAGMaxPass = defaultNativeDAGMaxPass
	}
	if workers.trendBackfillDays < 1 {
		workers.trendBackfillDays = defaultTrendBackfillDays
	}
	if workers.trendChunkSize < 1 || workers.trendChunkSize > workers.windowSize {
		workers.trendChunkSize = defaultTrendChunkSize
	}
	if workers.trendRPCWorkers < 1 {
		workers.trendRPCWorkers = defaultTrendRPCWorkers
	}
	if workers.trendMaxPassBlocks < workers.trendChunkSize {
		workers.trendMaxPassBlocks = workers.trendChunkSize
	}
	if workers.trendRecentBlocks < workers.trendChunkSize {
		workers.trendRecentBlocks = workers.trendChunkSize
	}
	if workers.trendSamplesPerPass < 1 {
		workers.trendSamplesPerPass = defaultTrendSamplesPass
	}
	if workers.pricePoll <= 0 {
		workers.pricePoll = defaultPricePollInterval
	}
	if workers.priceTopMarkets < 1 {
		workers.priceTopMarkets = 3
	}
	if workers.trendSampleMaxBlocks < workers.trendChunkSize {
		workers.trendSampleMaxBlocks = workers.trendChunkSize
	}
	workers.applyForcedLowImpactStartupLimits()
	if workers.trendEnabled {
		go workers.recentTrendWarmupWorker(ctx)
		go workers.trendBackfillWorker(ctx)
		go workers.nativeDAGTrendSampleWorker(ctx)
	}
	go workers.nativeDAGIngestWorker(ctx)
	go workers.poolMetricsWorker(ctx)
	go workers.priceIngestWorker(ctx)
	go workers.hostMetricsWorker(ctx)
	go workers.asicStatusWorker(ctx)
	go workers.freshnessWorker(ctx)
}

func localProducerSet() map[string]bool {
	values := []string{
		os.Getenv("MINING_POOL_ADDRESS"),
		os.Getenv("POOL_COINBASE_ADDRESS"),
		os.Getenv("MINING_ADDRESS"),
	}
	out := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && value != "0x0000000000000000000000000000000000000000" {
			out[value] = true
		}
	}
	return out
}

func lowImpactConfigFromEnv() lowImpactConfig {
	cfg := lowImpactConfig{
		Mode:                 normalizeLowImpactMode(env("BDAG_DASHBOARD_LOW_IMPACT_MODE", "auto")),
		LagBlocks:            int64(intEnv("BDAG_DASHBOARD_LOW_IMPACT_LAG_BLOCKS", 10)),
		TrendRecentBlocks:    intEnv("BDAG_LOW_IMPACT_TREND_RECENT_BLOCKS", 240),
		TrendChunkSize:       intEnv("BDAG_LOW_IMPACT_TREND_CHUNK_SIZE", 16),
		TrendRPCWorkers:      intEnv("BDAG_LOW_IMPACT_TREND_RPC_WORKERS", 1),
		TrendSamplesPerPass:  intEnv("BDAG_LOW_IMPACT_TREND_SPARSE_SAMPLES_PER_PASS", 1),
		TrendSampleMaxBlocks: intEnv("BDAG_LOW_IMPACT_TREND_SPARSE_MAX_SAMPLE_BLOCKS", 128),
		TrendCatchupEvery:    durationEnv("BDAG_LOW_IMPACT_TREND_CATCHUP_INTERVAL_SECONDS", 5*time.Minute),
		TrendChunkSleep:      durationEnvMillis("BDAG_LOW_IMPACT_TREND_BACKFILL_SLEEP_MS", time.Second),
		NativeDAGPoll:        durationEnvMillis("BDAG_LOW_IMPACT_NATIVE_DAG_POLL_MS", 5*time.Second),
		NativeDAGMaxPass:     uint64(intEnv("BDAG_LOW_IMPACT_NATIVE_DAG_MAX_BLOCKS_PER_PASS", 4)),
		CheckEvery:           durationEnv("BDAG_LOW_IMPACT_CHECK_INTERVAL_SECONDS", defaultLowImpactCheck),
	}
	if cfg.LagBlocks < 1 {
		cfg.LagBlocks = 10
	}
	if cfg.TrendRecentBlocks < 1 {
		cfg.TrendRecentBlocks = 1
	}
	if cfg.TrendChunkSize < 1 {
		cfg.TrendChunkSize = 1
	}
	if cfg.TrendRPCWorkers < 1 {
		cfg.TrendRPCWorkers = 1
	}
	if cfg.TrendSamplesPerPass < 1 {
		cfg.TrendSamplesPerPass = 1
	}
	if cfg.TrendSampleMaxBlocks < cfg.TrendChunkSize {
		cfg.TrendSampleMaxBlocks = cfg.TrendChunkSize
	}
	if cfg.TrendCatchupEvery <= 0 {
		cfg.TrendCatchupEvery = 5 * time.Minute
	}
	if cfg.TrendChunkSleep < 0 {
		cfg.TrendChunkSleep = time.Second
	}
	if cfg.NativeDAGPoll <= 0 {
		cfg.NativeDAGPoll = 5 * time.Second
	}
	if cfg.NativeDAGMaxPass < 1 {
		cfg.NativeDAGMaxPass = 1
	}
	if cfg.CheckEvery <= 0 {
		cfg.CheckEvery = defaultLowImpactCheck
	}
	return cfg
}

func normalizeLowImpactMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on", "enabled", "force", "forced":
		return "on"
	case "0", "false", "no", "n", "off", "disabled":
		return "off"
	default:
		return "auto"
	}
}

func (w *liveWorkers) applyForcedLowImpactStartupLimits() {
	if w.lowImpact.Mode != "on" {
		return
	}
	if w.lowImpact.TrendRecentBlocks > 0 && w.trendRecentBlocks > w.lowImpact.TrendRecentBlocks {
		w.trendRecentBlocks = w.lowImpact.TrendRecentBlocks
	}
	if w.lowImpact.TrendChunkSize > 0 && w.trendChunkSize > w.lowImpact.TrendChunkSize {
		w.trendChunkSize = w.lowImpact.TrendChunkSize
	}
	if w.lowImpact.TrendRPCWorkers > 0 && w.trendRPCWorkers > w.lowImpact.TrendRPCWorkers {
		w.trendRPCWorkers = w.lowImpact.TrendRPCWorkers
	}
	if w.lowImpact.TrendSamplesPerPass > 0 && w.trendSamplesPerPass > w.lowImpact.TrendSamplesPerPass {
		w.trendSamplesPerPass = w.lowImpact.TrendSamplesPerPass
	}
	if w.lowImpact.TrendSampleMaxBlocks > 0 && w.trendSampleMaxBlocks > w.lowImpact.TrendSampleMaxBlocks {
		w.trendSampleMaxBlocks = w.lowImpact.TrendSampleMaxBlocks
	}
	if w.lowImpact.TrendCatchupEvery > w.trendCatchupEvery {
		w.trendCatchupEvery = w.lowImpact.TrendCatchupEvery
	}
	if w.lowImpact.TrendChunkSleep > w.trendChunkSleep {
		w.trendChunkSleep = w.lowImpact.TrendChunkSleep
	}
	if w.lowImpact.NativeDAGPoll > w.nativeDAGPoll {
		w.nativeDAGPoll = w.lowImpact.NativeDAGPoll
	}
	if w.lowImpact.NativeDAGMaxPass > 0 && w.nativeDAGMaxPass > w.lowImpact.NativeDAGMaxPass {
		w.nativeDAGMaxPass = w.lowImpact.NativeDAGMaxPass
	}
}

func (w *liveWorkers) lowImpactActive(ctx context.Context) (bool, string) {
	switch w.lowImpact.Mode {
	case "on":
		return true, "forced by BDAG_DASHBOARD_LOW_IMPACT_MODE"
	case "off":
		return false, ""
	}
	if w.evmSyncBackoffActive() {
		return true, "EVM RPC sync backoff is active"
	}
	threshold := w.lowImpact.LagBlocks
	if threshold < 1 {
		if w.nodeRPCBackoffActive() {
			return true, "node RPC busy backoff is active"
		}
		return false, ""
	}
	if w.nodeRPCBackoffActive() {
		return true, "node RPC busy backoff is active"
	}
	now := time.Now()
	w.mu.Lock()
	if !w.lowImpactCachedUntil.IsZero() && now.Before(w.lowImpactCachedUntil) {
		active, reason := w.lowImpactCached, w.lowImpactReason
		w.mu.Unlock()
		return active, reason
	}
	w.mu.Unlock()

	active, reason := w.redisLowImpactSignal(ctx, threshold)
	w.mu.Lock()
	w.lowImpactCached = active
	w.lowImpactReason = reason
	w.lowImpactCachedUntil = now.Add(w.lowImpact.CheckEvery)
	w.mu.Unlock()
	return active, reason
}

func (w *liveWorkers) redisLowImpactSignal(ctx context.Context, threshold int64) (bool, string) {
	if w.runtime == nil || !w.runtime.Ready() {
		return false, ""
	}
	for _, key := range []string{"bdag:chain:snapshot", nativeDAGLiveSnapshotKey} {
		payload, err := w.runtime.readJSONKey(ctx, key)
		if err != nil || payload == nil {
			continue
		}
		lag := int64(number(stringValue(payload, "stream_lag_blocks")))
		if lag >= threshold {
			return true, fmt.Sprintf("%s reports %d block stream lag", key, lag)
		}
		latest := int64(number(first(stringValue(payload, "latest_block"), stringValue(payload, "last_streamed_block"))))
		chainLatest := int64(number(stringValue(payload, "chain_latest_block")))
		if chainLatest > 0 && latest > 0 && chainLatest-latest >= threshold {
			return true, fmt.Sprintf("%s chain tip is %d blocks ahead of live snapshot", key, chainLatest-latest)
		}
	}
	pool, err := w.runtime.readJSONKey(ctx, "bdag:pool:snapshot")
	if err != nil || pool == nil {
		return false, ""
	}
	data := mapValue(pool, "data")
	peerLead := int64(number(mapString(data, "backend_peer_lead_blocks")))
	if peerLead >= threshold {
		return true, fmt.Sprintf("pool backend peer lead is %d blocks", peerLead)
	}
	return false, ""
}

func (w *liveWorkers) trendLimits(ctx context.Context) trendWorkerLimits {
	limits := trendWorkerLimits{
		recentBlocks:    w.trendRecentBlocks,
		chunkSize:       w.trendChunkSize,
		rpcWorkers:      w.trendRPCWorkers,
		samplesPerPass:  w.trendSamplesPerPass,
		sampleMaxBlocks: w.trendSampleMaxBlocks,
		catchupEvery:    w.trendCatchupEvery,
		chunkSleep:      w.trendChunkSleep,
	}
	if active, _ := w.lowImpactActive(ctx); !active {
		return limits
	}
	if w.lowImpact.TrendRecentBlocks > 0 && limits.recentBlocks > w.lowImpact.TrendRecentBlocks {
		limits.recentBlocks = w.lowImpact.TrendRecentBlocks
	}
	if w.lowImpact.TrendChunkSize > 0 && limits.chunkSize > w.lowImpact.TrendChunkSize {
		limits.chunkSize = w.lowImpact.TrendChunkSize
	}
	if w.lowImpact.TrendRPCWorkers > 0 && limits.rpcWorkers > w.lowImpact.TrendRPCWorkers {
		limits.rpcWorkers = w.lowImpact.TrendRPCWorkers
	}
	if w.lowImpact.TrendSamplesPerPass > 0 && limits.samplesPerPass > w.lowImpact.TrendSamplesPerPass {
		limits.samplesPerPass = w.lowImpact.TrendSamplesPerPass
	}
	if w.lowImpact.TrendSampleMaxBlocks > 0 && limits.sampleMaxBlocks > w.lowImpact.TrendSampleMaxBlocks {
		limits.sampleMaxBlocks = w.lowImpact.TrendSampleMaxBlocks
	}
	if w.lowImpact.TrendCatchupEvery > limits.catchupEvery {
		limits.catchupEvery = w.lowImpact.TrendCatchupEvery
	}
	if w.lowImpact.TrendChunkSleep > limits.chunkSleep {
		limits.chunkSleep = w.lowImpact.TrendChunkSleep
	}
	return limits.normalized()
}

func (limits trendWorkerLimits) normalized() trendWorkerLimits {
	if limits.recentBlocks < 1 {
		limits.recentBlocks = defaultTrendRecentBlocks
	}
	if limits.chunkSize < 1 {
		limits.chunkSize = defaultTrendChunkSize
	}
	if limits.rpcWorkers < 1 {
		limits.rpcWorkers = 1
	}
	if limits.samplesPerPass < 1 {
		limits.samplesPerPass = 1
	}
	if limits.sampleMaxBlocks < limits.chunkSize {
		limits.sampleMaxBlocks = limits.chunkSize
	}
	if limits.catchupEvery <= 0 {
		limits.catchupEvery = defaultTrendCatchupEvery
	}
	return limits
}

func (w *liveWorkers) nativeDAGLimits(ctx context.Context) (time.Duration, int, uint64, bool) {
	poll := w.nativeDAGPoll
	window := w.nativeDAGWindow
	maxPass := w.nativeDAGMaxPass
	active, _ := w.lowImpactActive(ctx)
	if active {
		if w.lowImpact.NativeDAGPoll > poll {
			poll = w.lowImpact.NativeDAGPoll
		}
		if w.lowImpact.NativeDAGMaxPass > 0 && maxPass > w.lowImpact.NativeDAGMaxPass {
			maxPass = w.lowImpact.NativeDAGMaxPass
		}
	}
	if poll <= 0 {
		poll = defaultNativeDAGPoll
	}
	if window < 1 {
		window = defaultNativeDAGWindowLimit
	}
	if maxPass < 1 {
		maxPass = 1
	}
	return poll, chainDAGWindowLimit(window), maxPass, active
}

func (w *liveWorkers) nativeDAGIngestWorker(ctx context.Context) {
	if !w.nativeDAGEnabled {
		return
	}
	rebuild := func(reason string, force bool) bool {
		_, window, _, active := w.nativeDAGLimits(ctx)
		if active {
			return false
		}
		requestCtx, cancel := context.WithTimeout(ctx, durationEnv("BDAG_NATIVE_DAG_RPC_TIMEOUT_SECONDS", 8*time.Second))
		defer cancel()
		rebuilt, err := w.tryNativeDAGRebuildLiveWindow(withLowPriorityNodeRPC(requestCtx), window, reason, force)
		if err != nil && ctx.Err() == nil && !isNodeRPCBusyError(err) {
			log.Printf("native DAG live window rebuild failed (%s): %v", reason, err)
			return false
		}
		return rebuilt
	}
	if rebuild("startup", true) {
		w.lastNativeDAGRepair = time.Now()
	}
	for {
		poll, _, _, _ := w.nativeDAGLimits(ctx)
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if remaining, ok := w.nodeRPCBackoffRemaining(); ok {
				sleepContext(ctx, remaining)
				continue
			}
			requestCtx, cancel := context.WithTimeout(ctx, durationEnv("BDAG_NATIVE_DAG_RPC_TIMEOUT_SECONDS", 8*time.Second))
			if err := w.applyNativeDAGLivePass(withLowPriorityNodeRPC(requestCtx)); err != nil && ctx.Err() == nil && !isNodeRPCBusyError(err) {
				log.Printf("native DAG live pass failed: %v", err)
			}
			cancel()
			repairEvery := durationEnv("BDAG_NATIVE_DAG_REPAIR_INTERVAL_SECONDS", 60*time.Second)
			if repairEvery > 0 && (w.lastNativeDAGRepairAttempt.IsZero() || time.Since(w.lastNativeDAGRepairAttempt) >= repairEvery) {
				if rebuild("periodic-heal", false) {
					w.lastNativeDAGRepair = time.Now()
				}
			}
		}
	}
}

func (w *liveWorkers) nativeDAGTargetOrder(ctx context.Context) (uint64, error) {
	target, err := nativeRPCNumber(ctx, w.nativeDAGRPC, "getBlockCount")
	if err != nil {
		return 0, err
	}
	if target > w.nativeDAGSettleOrders {
		target -= w.nativeDAGSettleOrders
	}
	return target, nil
}

func (w *liveWorkers) applyNativeDAGLivePass(ctx context.Context) error {
	if nodeRPCPriorityFromContext(ctx) == nodeRPCPriorityLow {
		if err := w.nodeRPCBackoffErr("native DAG live pass paused while node RPC is backing off"); err != nil {
			return err
		}
	}
	_, window, maxPass, lowImpact := w.nativeDAGLimits(ctx)
	target, err := w.nativeDAGTargetOrder(ctx)
	if err != nil {
		return err
	}
	if target == 0 {
		return nil
	}
	last, err := w.runtime.nativeDAGLiveLastOrder(ctx)
	if errors.Is(err, redis.Nil) || last == 0 {
		rebuilt, err := w.tryNativeDAGRebuildLiveWindow(ctx, window, "missing-live-state", false)
		if err == nil && rebuilt {
			w.clearNativeDAGFetchFailures(0)
		}
		return err
	}
	if err != nil {
		return err
	}
	if target < last {
		rebuilt, err := w.tryNativeDAGRebuildLiveWindow(ctx, window, "native-order-rollback", false)
		if err == nil && rebuilt {
			w.clearNativeDAGFetchFailures(0)
		}
		return err
	}
	if target == last {
		return w.runtime.nativeDAGTouchLiveSnapshot(ctx, window)
	}
	gap := target - last
	if nativeDAGNeedsRebuildForGap(gap, w.nativeDAGMaxGap) {
		rebuilt, err := w.tryNativeDAGRebuildLiveWindow(ctx, window, "gap-too-large", false)
		if err == nil && rebuilt {
			w.clearNativeDAGFetchFailures(0)
		}
		return err
	}
	end := target
	if gap > maxPass {
		end = last + maxPass
	}
	if lowImpact && end > last+maxPass {
		end = last + maxPass
	}
	result, err := w.applyNativeDAGOrderRange(
		ctx,
		window,
		last+1,
		end,
		nativeDAGBlockByOrder,
		w.runtime.nativeDAGApplyLiveBlock,
	)
	if err != nil {
		return err
	}
	if result.applied > 0 {
		if result.skipped > 0 {
			_ = w.runtime.nativeDAGAnnotateLiveForwardGaps(ctx, window, result.skippedOrders, result.firstErr)
		}
		return nil
	}
	if result.terminalNotReady && result.firstErr == nil {
		return nil
	}
	if result.firstErr == nil {
		return nil
	}
	failures := w.noteNativeDAGFetchFailure(result.firstFailureOrder)
	threshold := intEnv("BDAG_NATIVE_DAG_STALLED_ORDER_REBUILD_FAILURES", defaultNativeDAGStallFailures)
	if threshold < 1 {
		threshold = defaultNativeDAGStallFailures
	}
	cooldown := durationEnv("BDAG_NATIVE_DAG_STALLED_ORDER_REBUILD_COOLDOWN_SECONDS", defaultNativeDAGStallRebuildCooldown)
	if cooldown < 0 {
		cooldown = defaultNativeDAGStallRebuildCooldown
	}
	if failures >= threshold && (w.lastNativeDAGStallRebuild.IsZero() || cooldown == 0 || time.Since(w.lastNativeDAGStallRebuild) >= cooldown) {
		w.lastNativeDAGStallRebuild = time.Now()
		rebuilt, rebuildErr := w.tryNativeDAGRebuildLiveWindow(ctx, window, "stalled-order-rebase", true)
		if rebuildErr != nil {
			return fmt.Errorf("fetch native DAG order %d failed %d times: %w; stalled-cursor rebuild failed: %v", result.firstFailureOrder, failures, result.firstErr, rebuildErr)
		}
		if rebuilt {
			w.clearNativeDAGFetchFailures(result.firstFailureOrder)
		}
		return nil
	}
	return fmt.Errorf("fetch native DAG order %d: %w", result.firstFailureOrder, result.firstErr)
}

func (w *liveWorkers) applyNativeDAGOrderRange(
	ctx context.Context,
	window int,
	start uint64,
	end uint64,
	fetch func(context.Context, string, uint64) (nativeDAGBlock, error),
	apply func(context.Context, string, nativeDAGBlock, int) (map[string]any, error),
) (nativeDAGOrderRangeResult, error) {
	result := nativeDAGOrderRangeResult{}
	if end < start {
		return result, nil
	}
	for order := start; order <= end; order++ {
		if nodeRPCPriorityFromContext(ctx) == nodeRPCPriorityLow {
			if err := w.nodeRPCBackoffErr("native DAG order fetch paused while node RPC is backing off"); err != nil {
				if result.firstErr == nil {
					result.firstErr = err
					result.firstFailureOrder = order
				}
				return result, nil
			}
		}
		if err := ctx.Err(); err != nil {
			if result.firstErr == nil {
				result.firstErr = err
				result.firstFailureOrder = order
			}
			return result, nil
		}
		block, err := fetch(ctx, w.nativeDAGRPC, order)
		if err != nil {
			if nativeDAGOrderNotReady(err) {
				if order == end {
					result.terminalNotReady = true
				}
				continue
			}
			result.skipped++
			result.skippedOrders = append(result.skippedOrders, order)
			if result.firstErr == nil {
				result.firstErr = err
				result.firstFailureOrder = order
			}
			continue
		}
		if _, err := apply(ctx, w.nativeDAGRPC, block, window); err != nil {
			return result, fmt.Errorf("apply native DAG order %d: %w", order, err)
		}
		result.applied++
		result.lastAppliedOrder = order
		w.clearNativeDAGFetchFailures(order)
	}
	return result, nil
}

func (w *liveWorkers) tryNativeDAGRebuildLiveWindow(ctx context.Context, window int, reason string, force bool) (bool, error) {
	if err := w.nodeRPCBackoffErr("native DAG rebuild paused while node RPC is backing off"); err != nil {
		return false, err
	}
	now := time.Now()
	if !force {
		cooldown := durationEnv("BDAG_NATIVE_DAG_REBUILD_ATTEMPT_COOLDOWN_SECONDS", defaultNativeDAGRebuildCooldown)
		if cooldown < 0 {
			cooldown = defaultNativeDAGRebuildCooldown
		}
		if cooldown > 0 && !w.lastNativeDAGRepairAttempt.IsZero() && now.Sub(w.lastNativeDAGRepairAttempt) < cooldown {
			return false, nil
		}
	}
	w.lastNativeDAGRepairAttempt = now
	_, err := w.runtime.nativeDAGRebuildLiveWindow(ctx, w.nativeDAGRPC, window, reason)
	if err != nil {
		if !errors.Is(ctx.Err(), context.Canceled) {
			markCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = w.runtime.markNativeDAGLiveSourceStale(markCtx, window, reason, 0, 0, err)
			cancel()
		}
		return false, err
	}
	w.lastNativeDAGRepair = now
	return true, nil
}

func nativeDAGNeedsRebuildForGap(gap, maxGap uint64) bool {
	return maxGap > 0 && gap > maxGap
}

func (w *liveWorkers) noteNativeDAGFetchFailure(order uint64) int {
	if order == 0 {
		return 0
	}
	if w.nativeDAGStalledOrder != order {
		w.nativeDAGStalledOrder = order
		w.nativeDAGStalledFailures = 0
	}
	w.nativeDAGStalledFailures++
	return w.nativeDAGStalledFailures
}

func (w *liveWorkers) clearNativeDAGFetchFailures(order uint64) {
	if order != 0 && w.nativeDAGStalledOrder != order {
		return
	}
	w.nativeDAGStalledOrder = 0
	w.nativeDAGStalledFailures = 0
}

func nativeDAGOrderNotReady(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "order is too big")
}

func (w *liveWorkers) logTrendError(format string, args ...any) {
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	if now.Sub(w.lastTrendErrorLog) < 30*time.Second {
		return
	}
	w.lastTrendErrorLog = now
	log.Printf(format, args...)
}

func (w *liveWorkers) chainEventFromHeader(header *evmBlockHeader, chainLatest uint64) map[string]any {
	height := header.height()
	producer := strings.ToLower(first(header.Miner, header.Author, header.Coinbase))
	timestampEpoch := header.timestampEpoch()
	now := time.Now().UTC()
	return map[string]any{
		"height":             height,
		"hash":               strings.ToLower(header.Hash),
		"parent_hash":        strings.ToLower(header.ParentHash),
		"timestamp":          time.Unix(timestampEpoch, 0).UTC().Format(time.RFC3339),
		"timestamp_epoch":    timestampEpoch,
		"producer":           producer,
		"rpc_source":         "local-evm",
		"received_at":        now.Format(time.RFC3339),
		"received_at_epoch":  now.Unix(),
		"requested_blocks":   w.windowSize,
		"chain_latest_block": chainLatest,
		"local_pool":         w.localProducers[producer],
	}
}

func (w *liveWorkers) recentTrendWarmupWorker(ctx context.Context) {
	if err := w.rebuildRecentTrendWindow(ctx, "recent-warmup"); err != nil && ctx.Err() == nil {
		if w.noteEVMSyncBackoff(err) {
			w.logEVMSyncBackoff("recent trend warmup paused while EVM RPC is syncing: %v", err)
			return
		}
		w.logTrendError("recent trend warmup failed: %v", err)
	}
}

func (w *liveWorkers) nativeDAGTrendSampleWorker(ctx context.Context) {
	cfg := trendRangeConfigFor(defaultTrendRange)
	interval := time.Duration(cfg.BucketSeconds) * time.Second
	if interval <= 0 {
		interval = 10 * time.Second
	}
	sample := func() {
		if w.runtime == nil || !w.runtime.Ready() {
			return
		}
		requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		now := time.Now().UTC()
		bucket, err := w.runtime.currentNativeDAGTrendBucket(requestCtx, cfg, now)
		if err != nil || bucket == nil {
			if err != nil && ctx.Err() == nil {
				w.logTrendError("native DAG trend sample failed: %v", err)
			}
			return
		}
		start := now.Unix()
		start -= start % int64(cfg.BucketSeconds)
		bucket.StartEpoch = start
		bucket.UpdatedEpoch = now.Unix()
		bucket.BestEffort = true
		bucket.Status = trendLiveSampleSource
		bucket.Source = trendLiveSampleSource
		if err := w.runtime.storeTrendBucket(requestCtx, trendCurrentStateKey, *bucket, cfg.BucketSeconds, trendLiveSampleSource); err != nil && ctx.Err() == nil {
			w.logTrendError("store native DAG trend sample failed: %v", err)
		}
	}
	sample()
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			sample()
		}
	}
}

func (w *liveWorkers) requestRecentTrendRebuild(ctx context.Context, reason string) {
	if w.nodeRPCBackoffActive() {
		return
	}
	now := time.Now()
	w.mu.Lock()
	if w.trendRebuildActive || (!w.lastTrendRebuild.IsZero() && now.Sub(w.lastTrendRebuild) < time.Minute) {
		w.mu.Unlock()
		return
	}
	w.lastTrendRebuild = now
	w.trendRebuildActive = true
	w.mu.Unlock()
	go func() {
		defer func() {
			w.mu.Lock()
			w.trendRebuildActive = false
			w.mu.Unlock()
		}()
		if err := w.rebuildRecentTrendWindow(withLowPriorityNodeRPC(ctx), reason); err != nil && ctx.Err() == nil {
			w.logTrendError("recent trend rebuild failed after %s: %v", reason, err)
		}
	}()
}

func (w *liveWorkers) rebuildRecentTrendWindow(ctx context.Context, reason string) error {
	if nodeRPCPriorityFromContext(ctx) == nodeRPCPriorityLow && w.nodeRPCBackoffActive() {
		return nodeRPCBackoffError{reason: "node RPC busy during trend rebuild"}
	}
	limits := w.trendLimits(ctx)
	latest, err := w.latestStableRetrievableBlock(ctx)
	if err != nil || latest == nil {
		if err == nil {
			err = errors.New("latest retrievable block is nil")
		}
		return fmt.Errorf("find latest block: %w", err)
	}
	latestHeight := latest.height()
	start := windowStart(latestHeight, limits.recentBlocks)
	if err := w.resetRecentTrendHistory(ctx, latest, reason, limits.recentBlocks); err != nil {
		return fmt.Errorf("reset recent trend history: %w", err)
	}
	resetOnce := false
	for chunkStart := start; chunkStart <= latestHeight; {
		chunkEnd := chunkStart + uint64(limits.chunkSize) - 1
		if chunkEnd > latestHeight {
			chunkEnd = latestHeight
		}
		headers, err := w.fetchBlockRangeBounded(ctx, chunkStart, chunkEnd, limits.rpcWorkers, limits.chunkSize)
		if err != nil {
			return fmt.Errorf("fetch %d-%d: %w", chunkStart, chunkEnd, err)
		}
		if len(headers) == 0 {
			return nil
		}
		contiguous, err := contiguousHeaders(chunkStart, headers)
		if err != nil {
			return err
		}
		result, err := w.applyTrendHeaders(ctx, trendCurrentStateKey, contiguous, latestHeight, "recent-warmup", true)
		if err != nil {
			return fmt.Errorf("apply %d-%d: %w", chunkStart, chunkEnd, err)
		}
		if result.Status == "reorg" {
			if resetOnce {
				return fmt.Errorf("repeated reorg at %d", result.LatestBlock)
			}
			resetOnce = true
			if err := w.resetRecentTrendHistory(ctx, latest, "recent-reorg", limits.recentBlocks); err != nil {
				return fmt.Errorf("reset after reorg: %w", err)
			}
			chunkStart = start
			continue
		}
		if result.Status == "gap" {
			return fmt.Errorf("stopped at gap %d after %d", result.GapAt, result.LatestBlock)
		}
		if result.LatestBlock < chunkStart {
			return nil
		}
		chunkStart = result.LatestBlock + 1
		if limits.chunkSleep > 0 {
			sleepContext(ctx, limits.chunkSleep)
		}
	}
	return nil
}

func (w *liveWorkers) trendBackfillWorker(ctx context.Context) {
	for ctx.Err() == nil {
		limits := w.trendLimits(ctx)
		if w.evmSyncBackoffActive() {
			sleepContext(ctx, limits.catchupEvery)
			continue
		}
		if w.nodeRPCBackoffActive() {
			sleepContext(ctx, limits.catchupEvery)
			continue
		}
		if err := w.catchupTrendHistory(withLowPriorityNodeRPC(ctx)); err != nil && ctx.Err() == nil {
			if w.noteEVMSyncBackoff(err) {
				w.logEVMSyncBackoff("trend history catchup paused while EVM RPC is syncing: %v", err)
				sleepContext(ctx, limits.catchupEvery)
				continue
			}
			w.logTrendError("trend history catchup failed: %v", err)
		}
		sleepContext(ctx, limits.catchupEvery)
	}
}

func (w *liveWorkers) catchupTrendHistory(ctx context.Context) error {
	limits := w.trendLimits(ctx)
	latest, err := w.latestStableRetrievableBlock(ctx)
	if err != nil {
		return err
	}
	if latest == nil {
		return errors.New("latest retrievable block is nil")
	}
	plan := w.sparseTrendPlan(latest)
	if len(plan) == 0 {
		return nil
	}
	applied := 0
	pending := 0
	for _, sample := range plan {
		done, err := w.runtime.client.SIsMember(ctx, trendSparseDoneKey, sample.ID).Result()
		if err != nil {
			return err
		}
		if done {
			continue
		}
		pending++
		if applied >= limits.samplesPerPass {
			continue
		}
		if err := w.applySparseTrendSample(ctx, sample, limits); err != nil {
			w.logTrendError("sparse trend sample %s failed: %v", sample.ID, err)
			continue
		}
		applied++
		if limits.chunkSleep > 0 {
			sleepContext(ctx, limits.chunkSleep)
		}
	}
	if pending == 0 {
		return w.markSparseTrendComplete(ctx, latest, len(plan))
	}
	return nil
}

func (w *liveWorkers) sparseTrendPlan(latest *evmBlockHeader) []trendSparseSample {
	latestTime := time.Unix(latest.timestampEpoch(), 0).UTC()
	latestHeight := latest.height()
	recentAge := time.Duration(w.trendRecentBlocks*2) * time.Second
	if recentAge < time.Hour {
		recentAge = time.Hour
	}
	var plan []trendSparseSample
	plan = append(plan, w.sparseTrendTier(latestTime, latestHeight, "10m", recentAge, 24*time.Hour, 10*time.Minute, time.Minute, []int{30, 60, 300, 900, 3600})...)
	plan = append(plan, w.sparseTrendTier(latestTime, latestHeight, "1h", 24*time.Hour, 7*24*time.Hour, time.Hour, 6*time.Minute, []int{300, 900, 3600})...)
	plan = append(plan, w.sparseTrendTier(latestTime, latestHeight, "1d", 7*24*time.Hour, time.Duration(w.trendBackfillDays)*24*time.Hour, 24*time.Hour, 30*time.Minute, []int{3600})...)
	total := len(plan)
	for i := range plan {
		plan[i].TotalPlanned = total
	}
	return plan
}

func (w *liveWorkers) sparseTrendTier(latestTime time.Time, latestHeight uint64, tier string, newestAge, oldestAge, step, window time.Duration, resolutions []int) []trendSparseSample {
	if oldestAge <= newestAge || step <= 0 || window <= 0 {
		return nil
	}
	newest := latestTime.Add(-newestAge).Truncate(step)
	oldest := latestTime.Add(-oldestAge).Truncate(step)
	var out []trendSparseSample
	for anchor := newest; !anchor.Before(oldest); anchor = anchor.Add(-step) {
		out = append(out, trendSparseSample{
			ID:            fmt.Sprintf("%s:%d:%d", tier, anchor.Unix(), int(window.Seconds())),
			Tier:          tier,
			Anchor:        anchor,
			Window:        window,
			Resolutions:   resolutions,
			ReferenceTime: latestTime,
			LatestHeight:  latestHeight,
		})
	}
	return out
}

func (w *liveWorkers) applySparseTrendSample(ctx context.Context, sample trendSparseSample, limits trendWorkerLimits) error {
	startHeight, err := w.findHeightAtOrAfterTimestamp(ctx, sample.Anchor.Unix(), sample.LatestHeight)
	if err != nil {
		return fmt.Errorf("find sample start height: %w", err)
	}
	endAfter, err := w.findHeightAtOrAfterTimestamp(ctx, sample.Anchor.Add(sample.Window).Unix(), sample.LatestHeight)
	if err != nil {
		return fmt.Errorf("find sample end height: %w", err)
	}
	endHeight := endAfter
	if endHeight > startHeight {
		endHeight--
	}
	if endHeight < startHeight {
		endHeight = startHeight
	}
	limits = limits.normalized()
	if sampleMax := uint64(limits.sampleMaxBlocks); sampleMax > 0 && endHeight-startHeight+1 > sampleMax {
		endHeight = startHeight + sampleMax - 1
	}
	headers, err := w.fetchBlockRangeChunked(ctx, startHeight, endHeight, limits.rpcWorkers, limits.chunkSize, limits.sampleMaxBlocks)
	if err != nil {
		return err
	}
	if len(headers) == 0 {
		return errors.New("sample returned no headers")
	}
	now := time.Now().UTC()
	result, err := w.runtime.applyFunction(ctx, "bdag_apply_trend_sample", map[string]any{
		"sample_id":                 sample.ID,
		"done_key":                  trendSparseDoneKey,
		"state_key":                 trendBackfillStateKey,
		"headers":                   w.chainEventsFromHeaders(headers, sample.LatestHeight),
		"resolutions":               sample.Resolutions,
		"source":                    "sparse-sample",
		"backfill_status":           "sparse-backfill",
		"updated_at":                now.Format(time.RFC3339),
		"updated_at_epoch":          now.Unix(),
		"sample_anchor_at":          sample.Anchor.Format(time.RFC3339),
		"sample_anchor_epoch":       sample.Anchor.Unix(),
		"sample_window_seconds":     int(sample.Window.Seconds()),
		"reference_timestamp_epoch": sample.ReferenceTime.Unix(),
		"sparse_total_samples":      sample.TotalPlanned,
	})
	if err != nil {
		return err
	}
	if result.Status == "ignored" {
		return fmt.Errorf("sample ignored: %s", result.Reason)
	}
	return nil
}

func (w *liveWorkers) chainEventsFromHeaders(headers []*evmBlockHeader, chainLatest uint64) []map[string]any {
	events := make([]map[string]any, 0, len(headers))
	for _, header := range headers {
		if header == nil || header.height() == 0 {
			continue
		}
		events = append(events, w.chainEventFromHeader(header, chainLatest))
	}
	return events
}

func (w *liveWorkers) fetchBlockRangeChunked(ctx context.Context, start, end uint64, workers, chunkSize, maxBlocks int) ([]*evmBlockHeader, error) {
	if end < start {
		return nil, nil
	}
	if maxBlocks > 0 && end-start+1 > uint64(maxBlocks) {
		end = start + uint64(maxBlocks) - 1
	}
	if chunkSize < 1 {
		chunkSize = defaultTrendChunkSize
	}
	capacity := int(end - start + 1)
	if maxBlocks > 0 && capacity > maxBlocks {
		capacity = maxBlocks
	}
	headers := make([]*evmBlockHeader, 0, capacity)
	for chunkStart := start; chunkStart <= end; {
		chunkEnd := chunkStart + uint64(chunkSize) - 1
		if chunkEnd > end {
			chunkEnd = end
		}
		chunk, err := w.fetchBlockRangeBounded(ctx, chunkStart, chunkEnd, workers, chunkSize)
		if err != nil {
			return nil, err
		}
		headers = append(headers, chunk...)
		chunkStart = chunkEnd + 1
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].height() < headers[j].height() })
	return headers, nil
}

func (w *liveWorkers) markSparseTrendComplete(ctx context.Context, latest *evmBlockHeader, total int) error {
	state := map[string]any{}
	if raw, err := w.runtime.client.Get(ctx, trendBackfillStateKey).Result(); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &state)
	}
	now := time.Now().UTC()
	state["status"] = "ok"
	state["backfill_status"] = "complete"
	state["backfill_mode"] = "sparse"
	state["updated_at"] = now.Format(time.RFC3339)
	state["updated_at_epoch"] = now.Unix()
	state["sparse_total_samples"] = total
	state["sparse_done_samples"] = total
	state["reference_height"] = latest.height()
	state["reference_at"] = time.Unix(latest.timestampEpoch(), 0).UTC().Format(time.RFC3339)
	raw, _ := json.Marshal(state)
	return w.runtime.client.Set(ctx, trendBackfillStateKey, raw, 0).Err()
}

func (w *liveWorkers) applyTrendHeaders(ctx context.Context, stateKey string, headers []*evmBlockHeader, chainLatest uint64, source string, emitEvent bool) (redisFunctionResult, error) {
	var zero redisFunctionResult
	if len(headers) == 0 {
		return zero, nil
	}
	events := make([]map[string]any, 0, len(headers))
	for _, header := range headers {
		if header == nil || header.height() == 0 {
			continue
		}
		events = append(events, w.chainEventFromHeader(header, chainLatest))
	}
	if len(events) == 0 {
		return zero, nil
	}
	now := time.Now().UTC()
	return w.runtime.applyFunction(ctx, "bdag_apply_trend_headers", map[string]any{
		"headers":          events,
		"state_key":        stateKey,
		"source":           source,
		"backfill_status":  source,
		"updated_at":       now.Format(time.RFC3339),
		"updated_at_epoch": now.Unix(),
		"emit_event":       emitEvent,
		"allow_bootstrap":  source != "live",
		"check_parent":     false,
	})
}

func contiguousHeaders(start uint64, headers []*evmBlockHeader) ([]*evmBlockHeader, error) {
	expected := start
	contiguous := make([]*evmBlockHeader, 0, len(headers))
	for _, header := range headers {
		if header == nil {
			continue
		}
		height := header.height()
		if height != expected {
			return nil, fmt.Errorf("trend backfill missing height %d before fetched height %d", expected, height)
		}
		contiguous = append(contiguous, header)
		expected++
	}
	if len(contiguous) == 0 {
		return nil, fmt.Errorf("trend backfill found no contiguous headers at %d", start)
	}
	return contiguous, nil
}

func (w *liveWorkers) trendCheckpoint(ctx context.Context, stateKey string) (uint64, int64, error) {
	raw, err := w.runtime.client.Get(ctx, stateKey).Result()
	if errors.Is(err, redis.Nil) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return 0, 0, err
	}
	return uint64Value(payload["last_indexed_height"]), int64Value(payload["last_indexed_timestamp_epoch"]), nil
}

func (w *liveWorkers) findHeightAtOrAfterTimestamp(ctx context.Context, targetEpoch int64, latestHeight uint64) (uint64, error) {
	if latestHeight == 0 {
		return 0, errors.New("latest height is zero")
	}
	if targetEpoch <= 0 {
		return 1, nil
	}
	low, high := uint64(1), latestHeight
	best := latestHeight
	for low <= high {
		mid := low + (high-low)/2
		header, err := w.fetchBlockByNumber(ctx, mid)
		if err != nil {
			return 0, err
		}
		ts := header.timestampEpoch()
		if ts >= targetEpoch {
			best = mid
			if mid == 0 {
				break
			}
			if mid == 1 {
				break
			}
			high = mid - 1
		} else {
			low = mid + 1
		}
	}
	return best, nil
}

func (w *liveWorkers) resetRecentTrendHistory(ctx context.Context, latest *evmBlockHeader, reason string, recentBlocks int) error {
	cutoff := trendRecentCutoffEpoch(latest.timestampEpoch(), recentBlocks)
	for _, seconds := range trendResolutionSeconds {
		indexKey := trendIndexKey(seconds)
		members, err := w.runtime.client.ZRangeByScore(ctx, indexKey, &redis.ZRangeBy{
			Min: strconv.FormatInt(cutoff, 10),
			Max: "+inf",
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return err
		}
		for _, member := range members {
			if err := w.runtime.client.Del(ctx, trendBucketKey(seconds, member)).Err(); err != nil {
				return err
			}
		}
		if len(members) > 0 {
			if err := w.runtime.client.ZRemRangeByScore(ctx, indexKey, strconv.FormatInt(cutoff, 10), "+inf").Err(); err != nil {
				return err
			}
		}
	}
	if err := w.resetTrendCheckpoint(ctx, trendCurrentStateKey, reason); err != nil {
		return err
	}
	return nil
}

func (w *liveWorkers) resetTrendCheckpoint(ctx context.Context, stateKey, reason string) error {
	if err := w.runtime.client.Del(ctx, stateKey, "bdag:trend:v2:reorg_needed").Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	w.publishDashboardEvent(ctx, "global", map[string]any{
		"reason":     "trend-reset-" + reason,
		"updated_at": now.Format(time.RFC3339),
	})
	return nil
}

func trendRecentCutoffEpoch(latestEpoch int64, recentBlocks int) int64 {
	if latestEpoch <= 0 {
		latestEpoch = time.Now().UTC().Unix()
	}
	if recentBlocks < 1 {
		recentBlocks = defaultTrendRecentBlocks
	}
	return latestEpoch - int64(recentBlocks*2)
}

func windowStart(latestHeight uint64, windowSize int) uint64 {
	if windowSize <= 0 {
		return latestHeight
	}
	window := uint64(windowSize)
	if latestHeight+1 > window {
		return latestHeight - window + 1
	}
	return 0
}

func (w *liveWorkers) fetchBlockRangeBounded(ctx context.Context, start, end uint64, workers, maxRange int) ([]*evmBlockHeader, error) {
	if end < start {
		return nil, nil
	}
	if nodeRPCPriorityFromContext(ctx) == nodeRPCPriorityLow {
		if err := w.nodeRPCBackoffErr("block range fetch paused while node RPC is backing off"); err != nil {
			return nil, err
		}
	}
	type result struct {
		header *evmBlockHeader
		err    error
	}
	total := int(end - start + 1)
	if total > maxRange {
		return nil, fmt.Errorf("refusing unbounded block range %d-%d (%d blocks, max %d)", start, end, total, maxRange)
	}
	if workers < 1 {
		workers = 1
	}
	if workers > total {
		workers = total
	}
	jobs := make(chan uint64)
	results := make(chan result, total)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for height := range jobs {
				header, err := w.fetchBlockByNumber(ctx, height)
				results <- result{header: header, err: err}
			}
		}()
	}
	for height := start; height <= end; height++ {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			close(results)
			return nil, ctx.Err()
		case jobs <- height:
		}
	}
	close(jobs)
	wg.Wait()
	close(results)
	headers := make([]*evmBlockHeader, 0, total)
	var errs []string
	for res := range results {
		if res.err != nil {
			errs = append(errs, res.err.Error())
			continue
		}
		if res.header != nil {
			headers = append(headers, res.header)
		}
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].height() < headers[j].height() })
	if len(headers) == 0 && len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return headers, nil
}

func (w *liveWorkers) latestRetrievableBlock(ctx context.Context) (*evmBlockHeader, error) {
	header, latestErr := w.fetchBlockByTag(ctx, "latest")
	if isEVMSyncBackoffError(latestErr) {
		return nil, latestErr
	}
	latestOK := latestErr == nil && header != nil && header.Hash != ""
	latestTagHeight := uint64(0)
	if latestOK {
		latestTagHeight = header.height()
	}
	blockNumberHeight, heightErr := w.ethBlockNumber(ctx)
	if heightErr == nil && blockNumberHeight > 0 {
		lagLimitValue := intEnv("BDAG_EVM_LATEST_TAG_STALE_TOLERANCE_BLOCKS", defaultLatestTagLagLimit)
		if lagLimitValue < 0 {
			lagLimitValue = defaultLatestTagLagLimit
		}
		lagLimit := uint64(lagLimitValue)
		if latestOK && blockNumberHeight <= latestTagHeight+lagLimit {
			return header, nil
		}
		numericHeader, err := w.retrievableBlockAtOrBefore(ctx, blockNumberHeight, defaultLatestBackscan)
		if err == nil && numericHeader != nil {
			return numericHeader, nil
		}
		if latestOK && blockNumberHeight > latestTagHeight+lagLimit {
			return nil, fmt.Errorf("eth_getBlockByNumber(latest) height %d is more than %d blocks behind eth_blockNumber %d; numeric lookup failed: %w", latestTagHeight, lagLimit, blockNumberHeight, err)
		}
		if latestOK {
			return header, nil
		}
		heightErr = err
	}
	if latestOK {
		return header, nil
	}
	if heightErr != nil && latestErr != nil {
		return nil, fmt.Errorf("%v; eth_getBlockByNumber(latest) fallback failed: %w", heightErr, latestErr)
	}
	if heightErr != nil {
		return nil, heightErr
	}
	return nil, latestErr
}

func isEVMSyncBackoffError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "syncing but no active peers") ||
		strings.Contains(message, "syncing but no relay peer") ||
		strings.Contains(message, "no active relay") ||
		(strings.Contains(message, "503 service unavailable") && strings.Contains(message, "sync"))
}

func (w *liveWorkers) noteEVMSyncBackoff(err error) bool {
	if !isEVMSyncBackoffError(err) {
		return false
	}
	if w.rpcBudget != nil {
		w.rpcBudget.noteError(err)
	}
	if w.evmSyncBackoff <= 0 {
		return true
	}
	now := time.Now()
	until := now.Add(w.evmSyncBackoff)
	w.mu.Lock()
	if until.After(w.evmSyncBackoffUntil) {
		w.evmSyncBackoffUntil = until
	}
	w.mu.Unlock()
	return true
}

func (w *liveWorkers) evmSyncBackoffActive() bool {
	_, ok := w.evmSyncBackoffRemaining()
	return ok
}

func (w *liveWorkers) evmSyncBackoffRemaining() (time.Duration, bool) {
	now := time.Now()
	w.mu.Lock()
	until := w.evmSyncBackoffUntil
	w.mu.Unlock()
	if until.IsZero() || !until.After(now) {
		return 0, false
	}
	return until.Sub(now), true
}

func (w *liveWorkers) nodeRPCBackoffActive() bool {
	return w.nodeRPCBudget().backoffActive()
}

func (w *liveWorkers) nodeRPCBackoffRemaining() (time.Duration, bool) {
	return w.nodeRPCBudget().backoffRemaining()
}

func (w *liveWorkers) nodeRPCBackoffErr(reason string) error {
	until, budgetReason, ok := w.nodeRPCBudget().backoffState()
	if !ok {
		return nil
	}
	reason = strings.TrimSpace(reason)
	budgetReason = strings.TrimSpace(budgetReason)
	if reason == "" {
		reason = budgetReason
	} else if budgetReason != "" {
		reason += ": " + budgetReason
	}
	return nodeRPCBackoffError{until: until, reason: reason}
}

func (w *liveWorkers) nodeRPCBudget() *nodeRPCBudget {
	if w != nil && w.rpcBudget != nil {
		return w.rpcBudget
	}
	return dashboardNodeRPCBudget
}

func (w *liveWorkers) logEVMSyncBackoff(format string, args ...any) {
	now := time.Now()
	interval := w.evmSyncBackoff
	if interval <= 0 {
		interval = 30 * time.Second
	}
	w.mu.Lock()
	if now.Sub(w.lastEVMSyncBackoffLog) < interval {
		w.mu.Unlock()
		return
	}
	w.lastEVMSyncBackoffLog = now
	w.mu.Unlock()
	log.Printf(format, args...)
}

func (w *liveWorkers) latestStableRetrievableBlock(ctx context.Context) (*evmBlockHeader, error) {
	latest, err := w.latestRetrievableBlock(ctx)
	if err != nil || latest == nil || w.stableDelay <= 0 {
		return latest, err
	}
	if latest.timestampEpoch() <= time.Now().Add(-w.stableDelay).Unix() {
		return latest, nil
	}
	latestHeight := latest.height()
	cutoff := time.Now().Add(-w.stableDelay).Unix()
	for scanned := uint64(1); scanned < defaultLatestBackscan && latestHeight >= scanned; scanned++ {
		height := latestHeight - scanned
		if height == 0 {
			break
		}
		header, err := w.fetchBlockByNumber(ctx, height)
		if err != nil || header == nil || header.Hash == "" {
			continue
		}
		if header.timestampEpoch() <= cutoff {
			return header, nil
		}
	}
	return latest, nil
}

func (w *liveWorkers) retrievableBlockAtOrBefore(ctx context.Context, latestHeight uint64, maxBackscan uint64) (*evmBlockHeader, error) {
	var lastErr error
	if maxBackscan == 0 {
		maxBackscan = defaultLatestBackscan
	}
	for scanned := uint64(0); scanned < maxBackscan && latestHeight >= scanned; scanned++ {
		height := latestHeight - scanned
		if height == 0 {
			break
		}
		header, err := w.fetchBlockByNumber(ctx, height)
		if err == nil && header != nil && header.Hash != "" {
			return header, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("no retrievable EVM block at or below eth_blockNumber %d within %d blocks: %w", latestHeight, maxBackscan, lastErr)
	}
	return nil, fmt.Errorf("no retrievable EVM block at or below eth_blockNumber %d within %d blocks", latestHeight, maxBackscan)
}

func (w *liveWorkers) ethBlockNumber(ctx context.Context) (uint64, error) {
	var raw string
	if err := w.evmRPC(ctx, "eth_blockNumber", []any{}, &raw); err != nil {
		return 0, err
	}
	return parseHexUint(raw)
}

func (w *liveWorkers) fetchBlockByNumber(ctx context.Context, height uint64) (*evmBlockHeader, error) {
	return w.fetchBlockByTag(ctx, fmt.Sprintf("0x%x", height))
}

func (w *liveWorkers) fetchBlockByTag(ctx context.Context, tag string) (*evmBlockHeader, error) {
	var header *evmBlockHeader
	if err := w.evmRPC(ctx, "eth_getBlockByNumber", []any{tag, false}, &header); err != nil {
		return nil, err
	}
	if header == nil {
		return nil, fmt.Errorf("eth_getBlockByNumber(%s) returned null", tag)
	}
	return header, nil
}

func (w *liveWorkers) evmRPC(ctx context.Context, method string, params []any, out any) error {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.evmHTTP, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if user, pass := env("NODE_RPC_USER", ""), env("NODE_RPC_PASS", ""); user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	client := w.httpClient
	if client == nil {
		client = newNodeHTTPClient(8 * time.Second)
		w.httpClient = client
	}
	resp, err := w.nodeRPCBudget().do(ctx, client, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		err := fmt.Errorf("EVM RPC %s returned %s: %s", method, resp.Status, string(raw))
		w.nodeRPCBudget().noteError(err)
		return err
	}
	var rpcResp evmRPCResponse
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return err
	}
	if rpcResp.Error != nil {
		err := fmt.Errorf("EVM RPC %s error %d: %s", method, rpcResp.Error.Code, rpcResp.Error.Message)
		w.nodeRPCBudget().noteError(err)
		return err
	}
	if string(rpcResp.Result) == "null" || len(rpcResp.Result) == 0 {
		return nil
	}
	return json.Unmarshal(rpcResp.Result, out)
}

func (h *evmBlockHeader) height() uint64 {
	if h == nil {
		return 0
	}
	value, _ := parseHexUint(h.Number)
	return value
}

func (h *evmBlockHeader) timestampEpoch() int64 {
	if h == nil {
		return time.Now().Unix()
	}
	value, err := parseHexUint(h.Timestamp)
	if err != nil || value == 0 {
		return time.Now().Unix()
	}
	return int64(value)
}

func parseHexUint(value string) (uint64, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "0x")
	if value == "" {
		return 0, errors.New("empty hex value")
	}
	return strconv.ParseUint(value, 16, 64)
}

func (w *liveWorkers) poolMetricsWorker(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(intEnv("BDAG_POOL_METRICS_SECONDS", 5)) * time.Second)
	defer ticker.Stop()
	for {
		if err := w.refreshPoolMetrics(ctx); err != nil && ctx.Err() == nil {
			log.Printf("pool metrics refresh failed: %v", err)
		}
		if err := w.refreshPoolJobState(ctx); err != nil && ctx.Err() == nil {
			log.Printf("pool job-state refresh failed: %v", err)
		}
		if err := w.refreshASICPerformance(ctx); err != nil && ctx.Err() == nil {
			log.Printf("pool ASIC performance refresh failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *liveWorkers) refreshPoolMetrics(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.poolMetrics, nil)
	if err != nil {
		return err
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("pool metrics returned %s", resp.Status)
	}
	metrics := parsePrometheusMetrics(resp.Body)
	now := time.Now().UTC()
	data := poolMetricsDataFromSamples(metrics)
	if raw, err := w.runtime.client.Get(ctx, "bdag:pool:snapshot").Result(); err == nil {
		var previous map[string]any
		if json.Unmarshal([]byte(raw), &previous) == nil {
			applyPoolMetricDeltas(previous, data, now)
		}
	}
	payload := map[string]any{
		"status":              "ok",
		"updated_at":          now.Format(time.RFC3339),
		"updated_at_epoch":    now.Unix(),
		"age_seconds":         0,
		"stale_after_seconds": 15,
		"degraded_reasons":    []string{},
		"source":              "redis-live",
		"data_version":        1,
		"data":                data,
	}
	raw, _ := json.Marshal(payload)
	if err := w.runtime.client.Set(ctx, "bdag:pool:snapshot", raw, 0).Err(); err != nil {
		return err
	}
	_, _ = w.runtime.applyFunction(ctx, "bdag_apply_pool_share", map[string]any{
		"updated_at": now.Format(time.RFC3339),
		"accepted":   data["shares_accepted_total"],
		"rejected":   data["shares_rejected_total"],
	})
	return nil
}

func poolMetricsDataFromSamples(metrics []prometheusSample) map[string]any {
	data := map[string]any{
		"active_connections":         metricValue(metrics, "pool_active_connections"),
		"shares_accepted_total":      metricValue(metrics, "pool_shares_accepted_total"),
		"shares_rejected_total":      metricValue(metrics, "pool_shares_rejected_total"),
		"blocks_found_total":         metricValue(metrics, "pool_blocks_found_total"),
		"accepted_block_submissions": metricValueWithLabels(metrics, "pool_block_submit_outcomes_total", map[string]string{"outcome": "accepted"}),
		"rejected_block_submissions": sumMetricWithName(metrics, "pool_blocks_rejected_by_node_total"),
		"authorized_miners":          metricValue(metrics, "pool_job_health_authorized_miners"),
		"ready_miners":               metricValue(metrics, "pool_job_health_ready_miners"),
		"stale_miners":               metricValue(metrics, "pool_job_health_current_job_stale_miners"),
		"invalidated_miners":         metricValue(metrics, "pool_job_health_current_job_invalidated_miners"),
		"expired_miners":             metricValue(metrics, "pool_job_health_current_job_expired_miners"),
		"backend_mineable":           metricValue(metrics, "pool_rpc_backend_node_health_mineable"),
		"backend_submit_ready":       metricValue(metrics, "pool_rpc_backend_node_health_submit_ready"),
		"backend_p2p_mining_fresh":   metricValue(metrics, "pool_rpc_backend_node_health_p2p_mining_fresh"),
		"backend_peer_lead_blocks":   metricValue(metrics, "pool_rpc_backend_node_health_p2p_best_peer_lead_blocks"),
		"backend_main_order":         metricValue(metrics, "pool_rpc_backend_node_health_main_order"),
		"template_age_seconds":       metricValue(metrics, "pool_rpc_backend_node_health_last_template_build_age_seconds"),
	}
	setMetricIfPresent(data, metrics, "backend_template_coinbase_valid", "pool_rpc_backend_node_health_template_coinbase_valid")
	setMetricIfPresent(data, metrics, "template_coinbase_reject_total", "pool_template_coinbase_rejects_total")
	setMetricIfPresent(data, metrics, "template_conversion_failure_ratio_percent", "pool_template_conversion_stall_failure_ratio")
	if metricSamplesPresent(metrics, "pool_template_conversion_stall_window_candidates") {
		accepted := metricValueWithLabels(metrics, "pool_template_conversion_stall_window_candidates", map[string]string{"kind": "accepted"})
		failed := metricValueWithLabels(metrics, "pool_template_conversion_stall_window_candidates", map[string]string{"kind": "failed"})
		stale := metricValueWithLabels(metrics, "pool_template_conversion_stall_window_candidates", map[string]string{"kind": "stale"})
		total := metricValueWithLabels(metrics, "pool_template_conversion_stall_window_candidates", map[string]string{"kind": "total"})
		data["template_conversion_accepted_candidates"] = accepted
		data["template_conversion_failed_candidates"] = failed
		data["template_conversion_stale_candidates"] = stale
		data["template_conversion_total_candidates"] = total
		if total > 0 {
			data["template_local_stale_ratio"] = stale / total
		}
	}
	return data
}

func setMetricIfPresent(data map[string]any, samples []prometheusSample, key string, metric string) {
	if metricSamplesPresent(samples, metric) {
		data[key] = metricValue(samples, metric)
	}
}

func metricSamplesPresent(samples []prometheusSample, name string) bool {
	for _, sample := range samples {
		if sample.Name == name {
			return true
		}
	}
	return false
}

func poolASICPerformanceURLFromMetrics(metricsURL string) string {
	return poolHealthURLFromMetrics(metricsURL, "/health/asic-performance", defaultPoolASICPerfURL)
}

func poolHealthURLFromMetrics(metricsURL string, path string, fallback string) string {
	if strings.TrimSpace(metricsURL) == "" {
		return fallback
	}
	parsed, err := url.Parse(metricsURL)
	if err != nil {
		return fallback
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func (w *liveWorkers) refreshPoolJobState(ctx context.Context) error {
	if w.runtime == nil || w.runtime.client == nil {
		return nil
	}
	url := strings.TrimSpace(w.poolJobState)
	if url == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("pool job-state returned %s", resp.Status)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	now := time.Now().UTC()
	payload["updated_at"] = now.Format(time.RFC3339)
	payload["updated_at_epoch"] = now.Unix()
	payload["age_seconds"] = 0
	payload["source"] = "pool-health-job-state"
	raw, _ := json.Marshal(payload)
	return w.runtime.client.Set(ctx, "bdag:pool:job_state", raw, 0).Err()
}

func (w *liveWorkers) refreshASICPerformance(ctx context.Context) error {
	if w.runtime == nil || w.runtime.client == nil {
		return nil
	}
	url := strings.TrimSpace(w.poolASICPerformance)
	if url == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("pool ASIC performance returned %s", resp.Status)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	now := time.Now().UTC()
	payload["updated_at"] = now.Format(time.RFC3339)
	payload["updated_at_epoch"] = now.Unix()
	payload["age_seconds"] = 0
	payload["source"] = "pool-health-asic-performance"
	raw, _ := json.Marshal(payload)
	return w.runtime.client.Set(ctx, "bdag:asic:performance", raw, 0).Err()
}

type cmcMarketPairsResponse struct {
	Data struct {
		MarketPairs []cmcMarketPair `json:"marketPairs"`
	} `json:"data"`
	Status struct {
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	} `json:"status"`
}

type cmcMarketPair struct {
	Rank               int     `json:"rank"`
	ExchangeName       string  `json:"exchangeName"`
	ExchangeSlug       string  `json:"exchangeSlug"`
	MarketPair         string  `json:"marketPair"`
	Category           string  `json:"category"`
	Price              float64 `json:"price"`
	VolumeUSD          float64 `json:"volumeUsd"`
	VolumePercent      float64 `json:"volumePercent"`
	LastUpdated        string  `json:"lastUpdated"`
	PriceExcluded      int     `json:"priceExcluded"`
	VolumeExcluded     int     `json:"volumeExcluded"`
	OutlierDetected    int     `json:"outlierDetected"`
	OutlierDisplay     int     `json:"outlierDisp"`
	PriceExDisplay     int     `json:"priceExDisp"`
	VolumeExDisplay    int     `json:"volExDisp"`
	IsVerified         int     `json:"isVerified"`
	QuoteSymbol        string  `json:"quoteSymbol"`
	BaseSymbol         string  `json:"baseSymbol"`
	EffectiveLiquidity float64 `json:"effectiveLiquidity"`
}

func (w *liveWorkers) priceIngestWorker(ctx context.Context) {
	if !w.priceEnabled || w.runtime == nil || w.runtime.client == nil {
		return
	}
	if err := w.refreshBDAGPrice(ctx); err != nil {
		log.Printf("price ingest failed: %v", err)
	}
	ticker := time.NewTicker(w.pricePoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.refreshBDAGPrice(ctx); err != nil {
				log.Printf("price ingest failed: %v", err)
			}
		}
	}
}

func (w *liveWorkers) refreshBDAGPrice(ctx context.Context) error {
	markets, err := w.fetchCMCBDAGMarkets(ctx)
	if err != nil {
		return err
	}
	selected := selectTopPriceMarkets(markets, w.priceTopMarkets)
	if len(selected) == 0 {
		return errors.New("CoinMarketCap returned no eligible BDAG markets")
	}
	now := time.Now().UTC()
	weighted, arithmetic, totalVolume := averageBDAGPrice(selected)
	if weighted <= 0 {
		return errors.New("CoinMarketCap eligible BDAG markets had no usable price")
	}
	marketRows := make([]map[string]any, 0, len(selected))
	for _, market := range selected {
		marketRows = append(marketRows, map[string]any{
			"rank":                market.Rank,
			"exchange":            market.ExchangeName,
			"exchange_slug":       market.ExchangeSlug,
			"market_pair":         market.MarketPair,
			"price_usd":           market.Price,
			"volume_usd_24h":      market.VolumeUSD,
			"volume_percent":      market.VolumePercent,
			"last_updated":        market.LastUpdated,
			"effective_liquidity": market.EffectiveLiquidity,
			"is_verified":         market.IsVerified == 1,
		})
	}
	staleAfter := int(w.pricePoll.Seconds()) * 3
	if staleAfter < 900 {
		staleAfter = 900
	}
	payload := map[string]any{
		"status":              "ok",
		"updated_at":          now.Format(time.RFC3339),
		"updated_at_epoch":    now.Unix(),
		"age_seconds":         0,
		"stale_after_seconds": staleAfter,
		"source":              "coinmarketcap-market-pairs",
		"data_version":        1,
		"symbol":              "BDAG",
		"currency":            "USD",
		"usd":                 weighted,
		"price_usd":           weighted,
		"volume_weighted_usd": weighted,
		"arithmetic_usd":      arithmetic,
		"volume_usd_24h":      totalVolume,
		"top_market_count":    len(selected),
		"markets":             marketRows,
	}
	raw, _ := json.Marshal(payload)
	if err := w.runtime.client.Set(ctx, priceSnapshotKey, raw, 0).Err(); err != nil {
		return err
	}
	w.publishDashboardEvent(ctx, "price", map[string]any{
		"updated_at": now.Format(time.RFC3339),
		"usd":        strconv.FormatFloat(weighted, 'f', -1, 64),
		"markets":    strconv.Itoa(len(selected)),
	})
	return nil
}

func (w *liveWorkers) fetchCMCBDAGMarkets(ctx context.Context) ([]cmcMarketPair, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.priceURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "redis-dash/1.0 (+https://github.com/BlockdagEngineering/redis-dash)")
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("CoinMarketCap returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var payload cmcMarketPairsResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload.Status.ErrorCode != "" && payload.Status.ErrorCode != "0" {
		return nil, fmt.Errorf("CoinMarketCap error %s: %s", payload.Status.ErrorCode, payload.Status.ErrorMessage)
	}
	return payload.Data.MarketPairs, nil
}

func selectTopPriceMarkets(markets []cmcMarketPair, limit int) []cmcMarketPair {
	if limit <= 0 {
		limit = 3
	}
	eligible := make([]cmcMarketPair, 0, len(markets))
	for _, market := range markets {
		if !strings.EqualFold(strings.TrimSpace(market.Category), "spot") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(market.BaseSymbol), "BDAG") {
			continue
		}
		if market.Price <= 0 || market.VolumeUSD <= 0 {
			continue
		}
		if market.PriceExcluded != 0 || market.VolumeExcluded != 0 || market.OutlierDetected != 0 || market.OutlierDisplay != 0 || market.PriceExDisplay != 0 || market.VolumeExDisplay != 0 {
			continue
		}
		eligible = append(eligible, market)
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].VolumeUSD == eligible[j].VolumeUSD {
			return eligible[i].ExchangeName < eligible[j].ExchangeName
		}
		return eligible[i].VolumeUSD > eligible[j].VolumeUSD
	})
	if len(eligible) > limit {
		eligible = eligible[:limit]
	}
	return eligible
}

func averageBDAGPrice(markets []cmcMarketPair) (weighted float64, arithmetic float64, totalVolume float64) {
	if len(markets) == 0 {
		return 0, 0, 0
	}
	for _, market := range markets {
		arithmetic += market.Price
		totalVolume += market.VolumeUSD
		weighted += market.Price * market.VolumeUSD
	}
	arithmetic /= float64(len(markets))
	if totalVolume > 0 {
		weighted /= totalVolume
	} else {
		weighted = arithmetic
	}
	return weighted, arithmetic, totalVolume
}

func applyPoolMetricDeltas(previous map[string]any, data map[string]any, now time.Time) {
	data["accepted_block_submissions_delta"] = 0.0
	data["accepted_block_submissions_rate_per_second"] = 0.0
	data["accepted_block_recent"] = false
	data["last_accepted_block_submission_age_seconds"] = nil
	previousData := mapValue(previous, "data")
	if previousData == nil {
		return
	}
	previousEpoch := number(mapString(previous, "updated_at_epoch"))
	if previousEpoch <= 0 {
		return
	}
	elapsed := now.Unix() - int64(previousEpoch)
	if elapsed <= 0 || elapsed > 60 {
		return
	}
	currentAccepted := number(mapString(data, "accepted_block_submissions"))
	previousAccepted := number(mapString(previousData, "accepted_block_submissions"))
	if currentAccepted < previousAccepted {
		return
	}
	delta := currentAccepted - previousAccepted
	data["accepted_block_submissions_delta"] = delta
	lastAcceptedEpoch := int64(number(mapString(previousData, "last_accepted_block_submission_at_epoch")))
	if delta > 0 {
		data["accepted_block_recent"] = true
		data["accepted_block_submissions_rate_per_second"] = delta / float64(elapsed)
		lastAcceptedEpoch = now.Unix()
	} else if lastAcceptedEpoch <= 0 && previousData["accepted_block_recent"] == true {
		lastAcceptedEpoch = int64(previousEpoch)
	}
	if lastAcceptedEpoch > 0 {
		age := now.Unix() - lastAcceptedEpoch
		if age >= 0 {
			data["last_accepted_block_submission_at_epoch"] = lastAcceptedEpoch
			data["last_accepted_block_submission_age_seconds"] = age
			if age <= acceptedBlockRecentWindowSeconds {
				data["accepted_block_recent"] = true
			}
		}
	}
}

func (w *liveWorkers) publishDashboardEvent(ctx context.Context, eventType string, values map[string]any) {
	if values == nil {
		values = map[string]any{}
	}
	values["type"] = eventType
	_ = w.runtime.client.XAdd(ctx, &redis.XAddArgs{
		Stream: "bdag:dashboard:events",
		MaxLen: 1000,
		Approx: true,
		Values: values,
	}).Err()
}

type prometheusSample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

func parsePrometheusMetrics(reader io.Reader) []prometheusSample {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var samples []prometheusSample
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, value, ok := parsePrometheusLine(line)
		if ok {
			samples = append(samples, prometheusSample{Name: name, Labels: labels, Value: value})
		}
	}
	return samples
}

func parsePrometheusLine(line string) (string, map[string]string, float64, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", nil, 0, false
	}
	metric := fields[0]
	value, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return "", nil, 0, false
	}
	labels := map[string]string{}
	name := metric
	if idx := strings.Index(metric, "{"); idx >= 0 && strings.HasSuffix(metric, "}") {
		name = metric[:idx]
		labelText := strings.TrimSuffix(metric[idx+1:], "}")
		for _, pair := range splitPrometheusLabels(labelText) {
			key, val, ok := strings.Cut(pair, "=")
			if !ok {
				continue
			}
			labels[key] = strings.Trim(strings.TrimSpace(val), `"`)
		}
	}
	return name, labels, value, true
}

func splitPrometheusLabels(value string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	escaped := false
	for _, r := range value {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\':
			current.WriteRune(r)
			escaped = true
		case r == '"':
			current.WriteRune(r)
			inQuotes = !inQuotes
		case r == ',' && !inQuotes:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func metricValue(samples []prometheusSample, name string) float64 {
	return sumMetricWithName(samples, name)
}

func sumMetricWithName(samples []prometheusSample, name string) float64 {
	var total float64
	for _, sample := range samples {
		if sample.Name == name {
			total += sample.Value
		}
	}
	return total
}

func metricValueWithLabels(samples []prometheusSample, name string, labels map[string]string) float64 {
	var total float64
	for _, sample := range samples {
		if sample.Name != name {
			continue
		}
		match := true
		for key, value := range labels {
			if sample.Labels[key] != value {
				match = false
				break
			}
		}
		if match {
			total += sample.Value
		}
	}
	return total
}

func (w *liveWorkers) hostMetricsWorker(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		w.refreshHostMetrics(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *liveWorkers) refreshHostMetrics(ctx context.Context) {
	now := time.Now().UTC()
	event := map[string]any{
		"updated_at":       now.Format(time.RFC3339),
		"updated_at_epoch": now.Unix(),
		"loadavg":          readTrimmedFile("/proc/loadavg"),
		"meminfo":          readMemInfo(),
	}
	_, _ = w.runtime.applyFunction(ctx, "bdag_apply_host_metric", event)
}

func (w *liveWorkers) asicStatusWorker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		w.refreshASICStatus(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *liveWorkers) refreshASICStatus(ctx context.Context) {
	now := time.Now().UTC()
	for _, event := range asicStatusEventsFromEnvAndARP(
		now,
		env("POOL_ASIC_MAC_OVERRIDES", ""),
		env("POOL_ASIC_MAC_ALLOWLIST", ""),
		asicARPTablePath(),
	) {
		_, _ = w.runtime.applyFunction(ctx, "bdag_apply_asic_status", event)
		w.refreshASICTelemetry(ctx, event)
	}
}

func (w *liveWorkers) refreshASICTelemetry(ctx context.Context, event map[string]any) {
	if !w.asicTelemetryEnabled || w.runtime == nil || w.runtime.client == nil {
		return
	}
	ip := strings.TrimSpace(mapString(event, "ip"))
	mac := normalizedMAC(mapString(event, "mac"))
	if ip == "" || mac == "" || !privateIPv4(ip) {
		return
	}
	timeout := w.asicTelemetryTimeout
	if timeout <= 0 {
		timeout = defaultASICTelemetryRead
	}
	now := time.Now().UTC()
	snapshot := map[string]any{
		"status":              "ok",
		"source":              "asic-port-80",
		"mac":                 mac,
		"ip":                  ip,
		"updated_at":          now.Format(time.RFC3339),
		"updated_at_epoch":    now.Unix(),
		"age_seconds":         0,
		"stale_after_seconds": 90,
		"errors":              map[string]any{},
	}
	errorsByEndpoint := map[string]any{}
	statusPayload, err := w.fetchASICJSON(ctx, ip, "/mcb/status", timeout)
	if err != nil {
		errorsByEndpoint["status"] = err.Error()
	} else {
		snapshot["controller_status"] = redactASICSecrets(statusPayload)
	}
	poolsPayload, err := w.fetchASICJSON(ctx, ip, "/mcb/pools", timeout)
	if err != nil {
		errorsByEndpoint["pools"] = err.Error()
	} else {
		snapshot["pool_config"] = sanitizeASICPools(poolsPayload)
	}
	settingsPayload, err := w.fetchASICJSON(ctx, ip, "/mcb/setting", timeout)
	if err != nil {
		errorsByEndpoint["settings"] = err.Error()
	} else {
		snapshot["settings"] = redactASICSecrets(settingsPayload)
	}
	devsPayload, err := w.fetchASICJSON(ctx, ip, "/mcb/cgminer?cgminercmd=devs", timeout)
	if err != nil {
		errorsByEndpoint["cgminer_devs"] = err.Error()
	} else {
		snapshot["cgminer_devs"] = redactASICSecrets(devsPayload)
	}
	if len(errorsByEndpoint) > 0 {
		snapshot["status"] = "degraded"
		snapshot["errors"] = errorsByEndpoint
	}
	snapshot["summary"] = asicTelemetrySummary(snapshot)
	raw, _ := json.Marshal(snapshot)
	_ = w.runtime.client.HSet(ctx, "bdag:asic:telemetry", mac, raw).Err()
	w.appendASICHashrateHistory(ctx, mac, snapshot, now)
}

func (w *liveWorkers) appendASICHashrateHistory(ctx context.Context, mac string, snapshot map[string]any, now time.Time) {
	if w == nil || w.runtime == nil || w.runtime.client == nil {
		return
	}
	key := asicHashrateHistoryKey(mac)
	if key == "" {
		return
	}
	point := asicHashrateHistoryPoint(snapshot, now)
	if len(point) == 0 {
		return
	}
	raw, err := json.Marshal(point)
	if err != nil {
		return
	}
	pipe := w.runtime.client.Pipeline()
	pipe.RPush(ctx, key, raw)
	pipe.LTrim(ctx, key, -defaultASICHashrateHistoryLimit, -1)
	_, _ = pipe.Exec(ctx)
}

func asicHashrateHistoryPoint(snapshot map[string]any, now time.Time) map[string]any {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	summary := mapValue(snapshot, "summary")
	hashrate := parseASICHashrateGH(first(mapString(summary, "hashrate"), mapString(summary, "avg_hashrate")))
	avgHashrate := parseASICHashrateGH(mapString(summary, "avg_hashrate"))
	return map[string]any{
		"t":            now.UTC().Format(time.RFC3339),
		"t_ms":         now.UTC().UnixMilli(),
		"status":       first(mapString(snapshot, "status"), "unknown"),
		"hashrate":     hashrate,
		"avg_hashrate": avgHashrate,
		"accepted":     number(mapString(summary, "accepted")),
		"rejected":     number(mapString(summary, "rejected")),
		"hw_errors":    number(mapString(summary, "hw_errors")),
		"temperature":  number(mapString(summary, "temperature")),
		"source":       "asic-cgminer-devs",
	}
}

func parseASICHashrateGH(value string) float64 {
	raw := strings.ToLower(strings.TrimSpace(value))
	if raw == "" || raw == "unknown" || raw == "n/a" {
		return 0
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return 0
	}
	numeric, ok := parseNumber(fields[0])
	if !ok {
		numeric = number(fields[0])
	}
	switch {
	case strings.Contains(raw, "th/s") || strings.Contains(raw, "ths"):
		return numeric * 1000
	case strings.Contains(raw, "mh/s") || strings.Contains(raw, "mhs"):
		return numeric / 1000
	case strings.Contains(raw, "kh/s") || strings.Contains(raw, "khs"):
		return numeric / 1_000_000
	default:
		return numeric
	}
}

func (w *liveWorkers) fetchASICJSON(ctx context.Context, ip string, path string, timeout time.Duration) (any, error) {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, "http://"+ip+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1_000_000))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("empty response")
	}
	var payload any
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func privateIPv4(value string) bool {
	ip := net.ParseIP(strings.TrimSpace(value))
	return ip != nil && ip.To4() != nil && ip.IsPrivate() && !ip.IsLoopback() && !ip.IsMulticast() && !ip.IsUnspecified()
}

func sanitizeASICPools(value any) []map[string]any {
	pools := listMaps(value)
	out := make([]map[string]any, 0, len(pools))
	for _, pool := range pools {
		row := map[string]any{}
		for key, raw := range pool {
			if sensitiveASICKey(key) {
				if strings.TrimSpace(stringify(raw)) != "" {
					row["pass_present"] = true
				}
				continue
			}
			row[key] = redactASICSecrets(raw)
		}
		out = append(out, row)
	}
	return out
}

func redactASICSecrets(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, raw := range typed {
			if sensitiveASICKey(key) {
				if strings.TrimSpace(stringify(raw)) != "" {
					out[key+"_present"] = true
				}
				continue
			}
			out[key] = redactASICSecrets(raw)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactASICSecrets(item))
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if redacted, ok := redactASICSecrets(item).(map[string]any); ok {
				out = append(out, redacted)
			}
		}
		return out
	default:
		return value
	}
}

func sensitiveASICKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return key == "pass" || key == "password" || strings.Contains(key, "token") || strings.Contains(key, "authorization")
}

func asicTelemetrySummary(snapshot map[string]any) map[string]any {
	controller := mapValue(snapshot, "controller_status")
	settings := mapValue(snapshot, "settings")
	pools := listMaps(snapshot["pool_config"])
	cgminer := mapValue(snapshot, "cgminer_devs")
	dev := firstASICDevice(cgminer)
	activePool := firstActiveASICPool(pools)
	powerPlan := first(mapString(settings, "manualPowerplan"), mapString(dev, "powerplan"), selectedASICPowerPlan(settings))
	clock, voltage := parseASICPowerPlan(powerPlan)
	return map[string]any{
		"model":             first(mapString(controller, "model"), mapString(controller, "hardware"), mapString(settings, "name"), "unknown"),
		"firmware":          first(mapString(controller, "firmware"), mapString(controller, "mcbversion"), mapString(settings, "version"), "unknown"),
		"manual":            first(mapString(settings, "manual"), "unknown"),
		"manual_powerplan":  first(powerPlan, "unknown"),
		"clock_mhz":         first(clock, "unknown"),
		"voltage_v":         first(voltage, "unknown"),
		"active_pool_url":   first(mapString(activePool, "url"), "unknown"),
		"active_pool_user":  shortAddress(mapString(activePool, "user")),
		"active_pool_legal": first(mapString(activePool, "legal"), "unknown"),
		"pool_pass_present": truthyValue(activePool["pass_present"]),
		"chip_count":        first(mapString(dev, "valid"), "unknown"),
		"miner_status":      first(mapString(dev, "minerstatus"), mapString(cgminer, "status"), "unknown"),
		"hashrate":          first(mapString(dev, "hashrate"), "unknown"),
		"avg_hashrate":      first(mapString(dev, "av_hashrate"), "unknown"),
		"temperature":       first(mapString(dev, "temp"), "unknown"),
		"fanspeed":          first(mapString(dev, "fanspeed"), "unknown"),
		"accepted":          first(mapString(dev, "accepted"), "0"),
		"rejected":          first(mapString(dev, "rejected"), "0"),
		"hw_errors":         first(mapString(dev, "hwerrors"), "0"),
		"nonces":            first(mapString(dev, "nonces"), "0"),
	}
}

func firstASICDevice(cgminer map[string]any) map[string]any {
	devices := listMaps(cgminer["data"])
	if len(devices) == 0 {
		devices = listMaps(cgminer["DEVS"])
	}
	if len(devices) == 0 {
		return map[string]any{}
	}
	return devices[0]
}

func firstActiveASICPool(pools []map[string]any) map[string]any {
	for _, pool := range pools {
		if truthyValue(pool["active"]) {
			return pool
		}
	}
	if len(pools) > 0 {
		return pools[0]
	}
	return map[string]any{}
}

func selectedASICPowerPlan(settings map[string]any) string {
	selected := strings.TrimSpace(mapString(settings, "select"))
	for _, plan := range listMaps(settings["powerplans"]) {
		if selected != "" && selected != mapString(plan, "level") {
			continue
		}
		if info := mapString(plan, "info"); info != "" {
			return info
		}
	}
	return ""
}

func parseASICPowerPlan(value string) (string, string) {
	fields := strings.Fields(value)
	var clock, voltage string
	for i, field := range fields {
		unit := strings.ToLower(strings.Trim(field, " ,;"))
		if unit == "mhz" && i > 0 {
			clock = strings.Trim(fields[i-1], " ,;") + " MHz"
		}
		if unit == "v" && i > 0 {
			voltage = strings.Trim(fields[i-1], " ,;") + " V"
		}
	}
	return clock, voltage
}

type asicARPEntry struct {
	ip  string
	mac string
}

func asicStatusEventsFromEnvAndARP(now time.Time, rawOverrides, rawAllowlist, arpPath string) []map[string]any {
	overrideIPs, overrides := parseASICMACOverrides(rawOverrides)
	allowlist := parseASICMACSet(rawAllowlist)
	arpEntries := readASICARPTable(arpPath)
	arpByIP := make(map[string]string, len(arpEntries))
	arpByMAC := make(map[string]string, len(arpEntries))
	for _, entry := range arpEntries {
		if !asicMACAllowed(entry.mac, allowlist) {
			continue
		}
		arpByIP[entry.ip] = entry.mac
		arpByMAC[entry.mac] = entry.ip
	}

	emittedMACs := make(map[string]bool)
	events := make([]map[string]any, 0, len(overrides)+len(arpByMAC))
	emit := func(ip, mac, status, source, configuredIP string) {
		if ip == "" || mac == "" || emittedMACs[mac] {
			return
		}
		event := map[string]any{
			"ip":               ip,
			"mac":              mac,
			"status":           status,
			"updated_at":       now.Format(time.RFC3339),
			"updated_at_epoch": now.Unix(),
			"source":           source,
		}
		if configuredIP != "" && configuredIP != ip {
			event["configured_ip"] = configuredIP
		}
		events = append(events, event)
		emittedMACs[mac] = true
	}

	for _, ip := range overrideIPs {
		overrideMAC := overrides[ip]
		if liveMAC := arpByIP[ip]; liveMAC != "" && (liveMAC == overrideMAC || len(allowlist) > 0) {
			emit(ip, liveMAC, "connected", "host-arp", "")
			continue
		}
		if liveIP := arpByMAC[overrideMAC]; liveIP != "" && liveIP != ip {
			emit(liveIP, overrideMAC, "connected", "host-arp-reconciled", ip)
			continue
		}
		emit(ip, overrideMAC, "configured", "env-overrides", "")
	}

	sort.SliceStable(arpEntries, func(i, j int) bool { return arpEntries[i].ip < arpEntries[j].ip })
	if len(allowlist) > 0 {
		for _, entry := range arpEntries {
			if !asicMACAllowed(entry.mac, allowlist) {
				continue
			}
			emit(entry.ip, entry.mac, "connected", "host-arp", "")
		}
	}
	return events
}

func parseASICMACOverrides(raw string) ([]string, map[string]string) {
	seen := make(map[string]bool)
	ips := []string{}
	result := make(map[string]string)
	for _, entry := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	}) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		ip, mac, ok := strings.Cut(entry, "=")
		if !ok {
			ip, mac, ok = strings.Cut(entry, "|")
		}
		if !ok {
			continue
		}
		ip = strings.TrimSpace(ip)
		mac = normalizedMAC(mac)
		if ip == "" || mac == "" {
			continue
		}
		if !seen[ip] {
			ips = append(ips, ip)
			seen[ip] = true
		}
		result[ip] = mac
	}
	return ips, result
}

func parseASICMACSet(raw string) map[string]bool {
	result := make(map[string]bool)
	for _, entry := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	}) {
		if mac := normalizedMAC(entry); mac != "" {
			result[mac] = true
		}
	}
	return result
}

func asicMACAllowed(mac string, allowlist map[string]bool) bool {
	return len(allowlist) == 0 || allowlist[mac]
}

func readASICARPTable(path string) []asicARPEntry {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var entries []asicARPEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[0] == "IP" {
			continue
		}
		if len(fields) >= 3 && !arpFlagsComplete(fields[2]) {
			continue
		}
		mac := normalizedMAC(fields[3])
		if mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		entries = append(entries, asicARPEntry{ip: fields[0], mac: mac})
	}
	return entries
}

func arpFlagsComplete(raw string) bool {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 0, 64)
	return err == nil && value&0x2 != 0
}

func asicARPTablePath() string {
	if path := strings.TrimSpace(env("POOL_ASIC_ARP_TABLE_PATH", "")); path != "" {
		return path
	}
	return defaultASICARPTablePath
}

func (w *liveWorkers) freshnessWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		_, _ = w.runtime.client.FCall(ctx, "bdag_mark_stale", []string{}, time.Now().Unix()).Result()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func readTrimmedFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func readMemInfo() map[string]string {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil
	}
	out := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		key, val, ok := strings.Cut(scanner.Text(), ":")
		if ok {
			out[key] = strings.TrimSpace(val)
		}
	}
	return out
}

func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := intEnv(key, int(fallback/time.Second))
	if value < 0 {
		return fallback
	}
	return time.Duration(value) * time.Second
}

func durationEnvMillis(key string, fallback time.Duration) time.Duration {
	value := intEnv(key, int(fallback/time.Millisecond))
	if value < 0 {
		return fallback
	}
	return time.Duration(value) * time.Millisecond
}

func uint64Value(value any) uint64 {
	switch typed := value.(type) {
	case uint64:
		return typed
	case uint:
		return uint64(typed)
	case int:
		if typed > 0 {
			return uint64(typed)
		}
	case int64:
		if typed > 0 {
			return uint64(typed)
		}
	case float64:
		if typed > 0 {
			return uint64(typed)
		}
	case string:
		parsed, _ := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		return parsed
	}
	return 0
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case uint64:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	}
	return 0
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
