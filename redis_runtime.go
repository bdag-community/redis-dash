package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisSocket               = "/run/dashboard-redis/redis.sock"
	defaultRedisDataDir              = "/var/lib/dashboard-redis"
	defaultRedisConfig               = "/etc/redis/redis.conf"
	defaultRedisMaxMemory            = "256mb"
	defaultRedisHistoryLimit         = 744
	defaultNativeRPCURL              = "http://node:38131"
	defaultNativeDAGVisibleWindow    = 90
	defaultNativeDAGWindowLimit      = 600
	maxNativeDAGWindowLimit          = 600
	acceptedBlockRecentWindowSeconds = 60

	nativeDAGLiveSnapshotKey  = "bdag:chain:dag:snapshot"
	nativeDAGLiveWindowKey    = "bdag:chain:dag:window"
	nativeDAGLiveLastOrderKey = "bdag:chain:dag:last_order"
	nativeDAGLiveBlockPrefix  = "bdag:chain:dag:block:"
	nativeDAGTipBlockCacheKey = "bdag:chain:dag:tip_block_by_hash"
	dashboardEventStreamKey   = "bdag:dashboard:events"
	asicHashrateHistoryPrefix = "bdag:asic:hashrate:"
	nativeDAGLiveSnapshotTTL  = 10 * time.Minute
	nativeDAGTipBlockCacheTTL = 10 * time.Minute

	defaultASICHashrateHistoryLimit = 2880
)

var nativeSyncProgressCache = struct {
	mu        sync.Mutex
	key       string
	payload   map[string]any
	fetchedAt time.Time
	expires   time.Time
}{}

type DashboardRuntime struct {
	client    *redis.Client
	cmd       *exec.Cmd
	managed   bool
	socket    string
	dataDir   string
	disabled  bool
	sseActive atomic.Int64
}

type redisFunctionResult struct {
	Status        string `json:"status"`
	Reorg         bool   `json:"reorg"`
	LatestBlock   uint64 `json:"latest_block"`
	FetchedBlocks int    `json:"fetched_blocks"`
	Reason        string `json:"reason"`
	Applied       int    `json:"applied"`
	Skipped       int    `json:"skipped"`
	GapAt         uint64 `json:"gap_at"`
}

type nativeRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type nativePeerInfo struct {
	Active     bool           `json:"active"`
	State      bool           `json:"state"`
	Network    string         `json:"network"`
	Genesis    string         `json:"genesis"`
	Graphstate map[string]any `json:"graphstate"`
}

type nativeDAGBlock struct {
	Hash            string         `json:"hash"`
	Height          int64          `json:"height"`
	Order           int64          `json:"order,omitempty"`
	Time            string         `json:"timestamp"`
	ParentRoot      string         `json:"parentroot"`
	Parents         []string       `json:"parents"`
	Children        []string       `json:"children"`
	Producer        string         `json:"producer"`
	Miner           string         `json:"miner"`
	Author          string         `json:"author"`
	CoinbaseAddress string         `json:"coinbase_address"`
	Coinbase        string         `json:"coinbase"`
	Pow             map[string]any `json:"pow"`
}

type nativeDAGTipsResult struct {
	Count   int            `json:"count"`
	Valid   []nativeDAGTip `json:"valid"`
	Invalid []nativeDAGTip `json:"invalid"`
}

type nativeDAGTip struct {
	ID          int64  `json:"id"`
	Hash        string `json:"hash"`
	Height      int64  `json:"height"`
	PruneHeight int64  `json:"pruneheight"`
	PruneCD     int64  `json:"prunecd"`
	Status      string `json:"status"`
}

func startDashboardRuntime(ctx context.Context) (*DashboardRuntime, error) {
	if envBool("BDAG_DASHBOARD_REDIS_ENABLED", true) == false {
		return &DashboardRuntime{disabled: true}, nil
	}
	if envBool("BDAG_DASHBOARD_REDIS_MANAGED", true) == false {
		client, err := redisClientFromEnv()
		if err != nil {
			return nil, err
		}
		rt := &DashboardRuntime{client: client}
		if err := rt.waitForRedis(ctx, 10*time.Second); err != nil {
			_ = client.Close()
			return nil, err
		}
		if err := rt.loadFunctions(ctx); err != nil {
			_ = client.Close()
			return nil, err
		}
		return rt, nil
	}

	socket := env("BDAG_REDIS_SOCKET", defaultRedisSocket)
	dataDir := env("BDAG_REDIS_DATA_DIR", defaultRedisDataDir)
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return nil, fmt.Errorf("create redis socket directory: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create redis data directory: %w", err)
	}
	_ = os.Remove(socket)

	binary := env("BDAG_REDIS_SERVER_BIN", "redis-server")
	if resolved, err := exec.LookPath(binary); err == nil {
		binary = resolved
	}
	args := redisServerArgs(socket, dataDir)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start redis-server: %w", err)
	}

	client := redis.NewClient(&redis.Options{
		Network: "unix",
		Addr:    socket,
	})
	rt := &DashboardRuntime{
		client:  client,
		cmd:     cmd,
		managed: true,
		socket:  socket,
		dataDir: dataDir,
	}
	if err := rt.waitForRedis(ctx, 15*time.Second); err != nil {
		rt.Close()
		return nil, err
	}
	if err := rt.loadFunctions(ctx); err != nil {
		rt.Close()
		return nil, err
	}
	go func() {
		err := cmd.Wait()
		if ctx.Err() == nil && err != nil {
			log.Printf("redis-server exited: %v", err)
		}
	}()
	return rt, nil
}

func redisClientFromEnv() (*redis.Client, error) {
	if socket := env("BDAG_REDIS_SOCKET", ""); socket != "" {
		return redis.NewClient(&redis.Options{Network: "unix", Addr: socket}), nil
	}
	url := env("BDAG_REDIS_URL", "")
	if url == "" {
		return nil, errors.New("BDAG_REDIS_URL or BDAG_REDIS_SOCKET is required when managed Redis is disabled")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opt), nil
}

func redisServerArgs(socket, dataDir string) []string {
	args := []string{}
	if config := env("BDAG_REDIS_CONFIG", defaultRedisConfig); config != "" {
		if _, err := os.Stat(config); err == nil {
			args = append(args, config)
		}
	}
	args = append(args,
		"--port", "0",
		"--bind", "127.0.0.1",
		"--protected-mode", "yes",
		"--unixsocket", socket,
		"--unixsocketperm", "700",
		"--dir", dataDir,
		"--save", env("BDAG_REDIS_SAVE", ""),
		"--appendonly", env("BDAG_REDIS_APPENDONLY", "yes"),
		"--appendfsync", env("BDAG_REDIS_APPENDFSYNC", "everysec"),
		"--maxmemory", env("BDAG_REDIS_MAXMEMORY", defaultRedisMaxMemory),
		"--maxmemory-policy", env("BDAG_REDIS_MAXMEMORY_POLICY", "noeviction"),
	)
	return args
}

func (r *DashboardRuntime) waitForRedis(ctx context.Context, timeout time.Duration) error {
	if r == nil || r.client == nil {
		return errors.New("redis client is not configured")
	}
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if err := r.client.Ping(ctx).Err(); err == nil {
			return nil
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("redis did not become ready: %w", last)
}

func (r *DashboardRuntime) loadFunctions(ctx context.Context) error {
	if r == nil || r.client == nil {
		return nil
	}
	if err := r.client.FunctionLoadReplace(ctx, bdagRedisFunctionLibrary).Err(); err != nil {
		return fmt.Errorf("load Redis functions: %w", err)
	}
	return nil
}

func (r *DashboardRuntime) Close() {
	if r == nil {
		return
	}
	if r.client != nil {
		if r.managed {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = r.client.Do(shutdownCtx, "SHUTDOWN", "NOSAVE").Err()
			cancel()
		}
		_ = r.client.Close()
	}
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
}

func (r *DashboardRuntime) Ready() bool {
	return r != nil && !r.disabled && r.client != nil
}

func (r *DashboardRuntime) applyFunction(ctx context.Context, name string, payload any) (redisFunctionResult, error) {
	var result redisFunctionResult
	if !r.Ready() {
		return result, errors.New("redis runtime unavailable")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	reply, err := r.client.FCall(ctx, name, []string{}, string(raw)).Result()
	if err != nil {
		return result, err
	}
	text := stringifyRedisReply(reply)
	if text == "" {
		return result, nil
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return result, err
	}
	return result, nil
}

func stringifyRedisReply(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func (r *DashboardRuntime) readJSONKey(ctx context.Context, key string) (map[string]any, error) {
	if !r.Ready() {
		return nil, errors.New("redis runtime unavailable")
	}
	raw, err := r.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	updateSnapshotAge(payload)
	return payload, nil
}

func (r *DashboardRuntime) poolPayload(ctx context.Context) (map[string]any, error) {
	return r.readJSONKey(ctx, "bdag:pool:snapshot")
}

func (r *DashboardRuntime) poolJobStatePayload(ctx context.Context) (map[string]any, error) {
	return r.readJSONKey(ctx, "bdag:pool:job_state")
}

func (r *DashboardRuntime) pricePayload(ctx context.Context) (map[string]any, error) {
	payload, err := r.readJSONKey(ctx, priceSnapshotKey)
	if err != nil {
		return nil, err
	}
	updateSnapshotAge(payload)
	return payload, nil
}

func (r *DashboardRuntime) asicPerformancePayload(ctx context.Context) (map[string]any, error) {
	payload, err := r.readJSONKey(ctx, "bdag:asic:performance")
	if err != nil || payload == nil {
		return payload, err
	}
	r.attachASICHashrateHistory(ctx, payload)
	return payload, nil
}

func (r *DashboardRuntime) attachASICHashrateHistory(ctx context.Context, payload map[string]any) {
	if r == nil || r.client == nil || payload == nil {
		return
	}
	lanes := listMaps(payload["lanes"])
	if len(lanes) == 0 {
		return
	}
	for _, lane := range lanes {
		mac := normalizedMAC(first(mapString(lane, "asic_mac"), mapString(lane, "mac")))
		if mac == "" {
			continue
		}
		key := asicHashrateHistoryKey(mac)
		if key == "" {
			continue
		}
		rawPoints, err := r.client.LRange(ctx, key, -defaultASICHashrateHistoryLimit, -1).Result()
		if err != nil || len(rawPoints) == 0 {
			continue
		}
		points := make([]map[string]any, 0, len(rawPoints))
		for _, raw := range rawPoints {
			var point map[string]any
			if err := json.Unmarshal([]byte(raw), &point); err != nil {
				continue
			}
			points = append(points, point)
		}
		if len(points) > 0 {
			lane["hashrate_points"] = points
		}
	}
	payload["lanes"] = lanes
}

func asicHashrateHistoryKey(mac string) string {
	normalized := normalizedMAC(mac)
	if normalized == "" {
		return ""
	}
	return asicHashrateHistoryPrefix + strings.ReplaceAll(normalized, ":", "")
}

func (r *DashboardRuntime) asicTelemetryPayload(ctx context.Context) (map[string]any, error) {
	if !r.Ready() {
		return nil, errors.New("redis runtime unavailable")
	}
	raw, err := r.client.HGetAll(ctx, "bdag:asic:telemetry").Result()
	if err != nil || len(raw) == 0 {
		return nil, err
	}
	devices := make([]map[string]any, 0, len(raw))
	var latestEpoch int64
	for _, value := range raw {
		var device map[string]any
		if err := json.Unmarshal([]byte(value), &device); err != nil {
			continue
		}
		updateSnapshotAge(device)
		if epoch := int64(number(mapString(device, "updated_at_epoch"))); epoch > latestEpoch {
			latestEpoch = epoch
		}
		devices = append(devices, device)
	}
	if len(devices) == 0 {
		return nil, redis.Nil
	}
	sort.SliceStable(devices, func(i, j int) bool {
		return first(mapString(devices[i], "ip"), mapString(devices[i], "mac")) < first(mapString(devices[j], "ip"), mapString(devices[j], "mac"))
	})
	payload := map[string]any{
		"status":     "ok",
		"source":     "redis-asic-telemetry",
		"source_key": "bdag:asic:telemetry",
		"devices":    devices,
	}
	if latestEpoch > 0 {
		updatedAt := time.Unix(latestEpoch, 0).UTC()
		payload["updated_at"] = updatedAt.Format(time.RFC3339)
		payload["updated_at_epoch"] = latestEpoch
	}
	updateSnapshotAge(payload)
	return payload, nil
}

func (r *DashboardRuntime) chainPayload(ctx context.Context) (map[string]any, error) {
	return r.readJSONKey(ctx, "bdag:chain:snapshot")
}

func (r *DashboardRuntime) chainWindowPayload(ctx context.Context, limit int) (map[string]any, error) {
	var nativePayload map[string]any
	if payload, err := r.nativeDAGLivePayload(ctx, limit); err == nil && payload != nil {
		nativePayload = payload
		if chainDAGNativePayloadSafeForDisplay(payload) {
			if dashboardNodeRPCBudget.backoffActive() {
				payload["best_effort"] = true
				warnings := stringSlice(payload["warnings"])
				warnings = appendUniqueWarning(warnings, "Serving Redis native DAG snapshot while node RPC backoff is active; live data may lag.")
				payload["warnings"] = warnings
			}
			return payload, nil
		}
	}
	if nativePayload != nil {
		return nativePayload, nil
	}
	now := time.Now().UTC()
	return map[string]any{
		"status":              "warming",
		"mode":                "native-dag-live",
		"source":              "redis-native-dag-live",
		"live_pipeline":       "redis-incremental",
		"updated_at":          now.Format(time.RFC3339),
		"updated_at_epoch":    now.Unix(),
		"age_seconds":         0,
		"stale_after_seconds": 15,
		"best_effort":         true,
		"window": map[string]any{
			"requested": chainDAGWindowLimit(limit),
			"available": 0,
			"truncated": false,
		},
		"coverage": map[string]any{
			"requested": chainDAGWindowLimit(limit),
			"available": 0,
			"complete":  false,
			"failures":  0,
			"estimated": 0,
		},
		"nodes": []map[string]any{},
		"edges": []map[string]any{},
		"warnings": []string{
			"Native DAG live Redis window is warming. Waiting for the ingest worker instead of using stale linear fallbacks.",
		},
	}, nil
}

func chainDAGPayloadHasNodes(payload map[string]any) bool {
	return payload != nil && len(listMaps(payload["nodes"])) > 0
}

func chainDAGNativePayloadSafeForDisplay(payload map[string]any) bool {
	if !chainDAGPayloadHasNodes(payload) {
		return false
	}
	if nativeDAGLiveSnapshotSourceDegraded(payload) {
		return false
	}
	if strings.TrimSpace(stringValue(payload, "source_error")) != "" {
		return false
	}
	for _, warning := range stringSlice(payload["warnings"]) {
		text := lower(warning)
		if strings.Contains(text, "native dag tips are unavailable") ||
			strings.Contains(text, "node rpc backoff active") ||
			strings.Contains(text, "too many requests") ||
			strings.Contains(text, "rate limit") ||
			(strings.Contains(text, "skipped ") && strings.Contains(text, "native dag block rpc results")) {
			return false
		}
	}
	return true
}

func (r *DashboardRuntime) nativeDAGLivePayload(ctx context.Context, limit int) (map[string]any, error) {
	if !r.Ready() {
		return nil, errors.New("redis runtime unavailable")
	}
	limit = chainDAGWindowLimit(limit)
	raw, err := r.client.Get(ctx, nativeDAGLiveSnapshotKey).Result()
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	trimNativeDAGPayloadWindow(payload, limit)
	updateSnapshotAge(payload)
	return payload, nil
}

func (r *DashboardRuntime) nativeChainWindowPayload(ctx context.Context, limit int) (map[string]any, error) {
	if !r.Ready() {
		return nil, errors.New("redis runtime unavailable")
	}
	limit = chainDAGWindowLimit(limit)
	cacheSeconds := intEnv("BDAG_CHAIN_DAG_CACHE_SECONDS", 5)
	if cacheSeconds < 1 {
		cacheSeconds = 1
	}
	cacheKey := "bdag:chain:native_dag_window:" + strconv.Itoa(limit)
	if raw, err := r.client.Get(ctx, cacheKey).Result(); err == nil && strings.TrimSpace(raw) != "" {
		var cached map[string]any
		if json.Unmarshal([]byte(raw), &cached) == nil {
			updateSnapshotAge(cached)
			return cached, nil
		}
	}
	url := first(env("BDAG_NODE_RPC_URL", ""), env("NODE_RPC_URL", ""), env("BDAG_RPC_URL", ""), defaultNativeRPCURL)
	timeout := durationEnv("BDAG_CHAIN_DAG_RPC_TIMEOUT_SECONDS", 5*time.Second)
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	blockCount, err := nativeRPCNumber(requestCtx, url, "getBlockCount")
	if err != nil {
		return nil, err
	}
	settleOrders := intEnv("BDAG_CHAIN_DAG_SETTLE_ORDERS", 2)
	if settleOrders < 0 {
		settleOrders = 0
	}
	if blockCount > uint64(settleOrders) {
		blockCount -= uint64(settleOrders)
	}
	start := uint64(1)
	if blockCount >= uint64(limit) {
		start = blockCount - uint64(limit) + 1
	}
	blocks, failures := nativeDAGBlocksByOrder(requestCtx, url, start, blockCount)
	tips, tipErr := nativeDAGTips(requestCtx, url)
	tipBlocks, tipFailures := nativeDAGTipBlocks(requestCtx, url, tips, blocks)
	if len(tipBlocks) > 0 {
		blocks = append(blocks, tipBlocks...)
	}
	failures += tipFailures
	coinbaseLookups, coinbaseFailures := r.enrichNativeDAGCoinbase(requestCtx, url, blocks)
	payload := nativeDAGPayload(blocks, limit, failures, tips)
	payload["coinbase_lookups"] = coinbaseLookups
	if coinbaseFailures > 0 {
		warnings := stringSlice(payload["warnings"])
		warnings = append(warnings, fmt.Sprintf("Skipped %d native DAG coinbase attribution RPC results while coloring producers.", coinbaseFailures))
		payload["warnings"] = warnings
	}
	if tipErr != nil {
		warnings := stringSlice(payload["warnings"])
		warnings = append(warnings, "Native DAG tips are unavailable: "+tipErr.Error())
		payload["warnings"] = warnings
	}
	raw, _ := json.Marshal(payload)
	_ = r.client.Set(ctx, cacheKey, raw, time.Duration(cacheSeconds)*time.Second).Err()
	return payload, nil
}

func (r *DashboardRuntime) nativeDAGRebuildLiveWindow(ctx context.Context, url string, limit int, reason string) (map[string]any, error) {
	if !r.Ready() {
		return nil, errors.New("redis runtime unavailable")
	}
	limit = chainDAGWindowLimit(limit)
	blockCount, err := nativeRPCNumber(ctx, url, "getBlockCount")
	if err != nil {
		return nil, err
	}
	start := uint64(1)
	if blockCount >= uint64(limit) {
		start = blockCount - uint64(limit) + 1
	}
	blocks, failures := nativeDAGBlocksByOrder(ctx, url, start, blockCount)
	_, coinbaseFailures := r.enrichNativeDAGCoinbase(ctx, url, blocks)
	failures += coinbaseFailures
	if len(blocks) == 0 {
		if until, busyReason, ok := dashboardNodeRPCBudget.backoffState(); ok {
			err := nodeRPCBackoffError{until: until, reason: "native DAG rebuild paused while node RPC is backing off: " + first(busyReason, "node RPC is busy")}
			_ = r.markNativeDAGLiveSourceStale(ctx, limit, reason, blockCount, failures, err)
			return nil, err
		}
	}
	if nativeDAGRebuildHasNoUsableBlocks(blocks, failures) {
		err := fmt.Errorf("native DAG rebuild fetched no usable blocks for orders %d-%d (%d failures)", start, blockCount, failures)
		_ = r.markNativeDAGLiveSourceStale(ctx, limit, reason, blockCount, failures, err)
		return nil, err
	}
	if failures > 0 {
		if err := r.mergeNativeDAGLiveBlocks(ctx, blocks, limit); err != nil {
			return nil, err
		}
	} else {
		if err := r.replaceNativeDAGLiveBlocks(ctx, blocks, limit); err != nil {
			return nil, err
		}
	}
	payload, err := r.nativeDAGBuildLiveSnapshot(ctx, url, limit, failures)
	if err != nil {
		return nil, err
	}
	payload["live_pipeline"] = "redis-incremental"
	payload["rebuild_reason"] = reason
	raw, _ := json.Marshal(payload)
	if err := r.client.Set(ctx, nativeDAGLiveSnapshotKey, raw, nativeDAGLiveSnapshotTTL).Err(); err != nil {
		return nil, err
	}
	tipHash := ""
	if tip, ok := payload["tip"].(map[string]any); ok {
		tipHash = mapString(tip, "hash")
	}
	_ = r.emitDashboardEvent(ctx, "chain-dag", map[string]any{
		"reason":     first(reason, "rebuild"),
		"full":       "true",
		"latest":     tipHash,
		"node_count": strconv.Itoa(len(listMaps(payload["nodes"]))),
	})
	return payload, nil
}

func nativeDAGRebuildHasNoUsableBlocks(blocks []nativeDAGBlock, failures int) bool {
	return failures > 0 && len(blocks) == 0
}

func (r *DashboardRuntime) markNativeDAGLiveSourceStale(ctx context.Context, limit int, reason string, targetOrder uint64, failures int, sourceErr error) error {
	if !r.Ready() {
		return errors.New("redis runtime unavailable")
	}
	limit = chainDAGWindowLimit(limit)
	now := time.Now().UTC()
	payload := map[string]any{}
	if raw, err := r.client.Get(ctx, nativeDAGLiveSnapshotKey).Result(); err == nil && strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &payload)
	}
	if len(payload) == 0 {
		payload = map[string]any{
			"mode":                "native-dag-live",
			"source":              "native-rpc-live",
			"live_pipeline":       "redis-incremental",
			"updated_at":          now.Format(time.RFC3339),
			"updated_at_epoch":    now.Unix(),
			"age_seconds":         0,
			"stale_after_seconds": 15,
			"nodes":               []map[string]any{},
			"edges":               []map[string]any{},
			"window": map[string]any{
				"requested": limit,
				"available": 0,
				"truncated": false,
			},
			"coverage": map[string]any{
				"requested": limit,
				"available": 0,
				"complete":  false,
				"failures":  failures,
				"estimated": 0,
			},
		}
	}
	payload["status"] = "stale"
	payload["best_effort"] = true
	payload["live_pipeline_status"] = "source-degraded"
	payload["last_checked_at"] = now.Format(time.RFC3339)
	payload["last_checked_at_epoch"] = now.Unix()
	payload["last_rebuild_reason"] = first(reason, "native-dag-source-stale")
	targetOrder, failures = nativeDAGPreserveStaleDiagnostics(payload, targetOrder, failures)
	payload["last_rebuild_target_order"] = targetOrder
	payload["last_rebuild_failures"] = failures
	if sourceErr != nil {
		payload["source_error"] = sourceErr.Error()
	}
	window := mapValue(payload, "window")
	if window == nil {
		window = map[string]any{}
	}
	window["requested"] = limit
	payload["window"] = window
	coverage := mapValue(payload, "coverage")
	if coverage == nil {
		coverage = map[string]any{}
	}
	coverage["requested"] = limit
	coverage["complete"] = false
	coverage["failures"] = failures
	payload["coverage"] = coverage
	warnings := stringSlice(payload["warnings"])
	message := "Native DAG live source is stale: rebuild fetched no usable blocks for the current topology window."
	warnings = appendUniqueWarning(warnings, message)
	if len(warnings) > 8 {
		warnings = warnings[len(warnings)-8:]
	}
	payload["warnings"] = warnings
	raw, _ := json.Marshal(payload)
	if err := r.client.Set(ctx, nativeDAGLiveSnapshotKey, raw, nativeDAGLiveSnapshotTTL).Err(); err != nil {
		return err
	}
	_ = r.emitDashboardEvent(ctx, "chain-dag", map[string]any{
		"reason":       first(reason, "native-dag-source-stale"),
		"source_error": stringValue(payload, "source_error"),
		"full":         "true",
	})
	return nil
}

func appendUniqueWarning(warnings []string, message string) []string {
	message = strings.TrimSpace(message)
	if message == "" {
		return warnings
	}
	for _, warning := range warnings {
		if strings.TrimSpace(warning) == message {
			return warnings
		}
	}
	return append(warnings, message)
}

func nativeDAGPreserveStaleDiagnostics(payload map[string]any, targetOrder uint64, failures int) (uint64, int) {
	if targetOrder == 0 {
		targetOrder = uint64Value(payload["last_rebuild_target_order"])
	}
	if failures == 0 {
		failures = int(int64Value(payload["last_rebuild_failures"]))
	}
	if failures < 0 {
		failures = 0
	}
	return targetOrder, failures
}

func (r *DashboardRuntime) nativeDAGApplyLiveBlock(ctx context.Context, url string, block nativeDAGBlock, limit int) (map[string]any, error) {
	if !r.Ready() {
		return nil, errors.New("redis runtime unavailable")
	}
	limit = chainDAGWindowLimit(limit)
	if strings.TrimSpace(block.Hash) == "" || block.Order <= 0 {
		return nil, errors.New("native DAG live block is missing hash or order")
	}
	blocks := []nativeDAGBlock{block}
	_, coinbaseFailures := r.enrichNativeDAGCoinbase(ctx, url, blocks)
	block = blocks[0]
	if err := r.storeNativeDAGLiveBlock(ctx, block, limit); err != nil {
		return nil, err
	}
	payload, err := r.nativeDAGBuildLiveSnapshot(ctx, url, limit, coinbaseFailures)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(payload)
	if err := r.client.Set(ctx, nativeDAGLiveSnapshotKey, raw, nativeDAGLiveSnapshotTTL).Err(); err != nil {
		return nil, err
	}
	patchRaw, _ := json.Marshal(nativeDAGPatchFromPayload(payload, block.Hash, limit))
	_ = r.emitDashboardEvent(ctx, "chain-dag", map[string]any{
		"reason": "block-applied",
		"hash":   strings.ToLower(strings.TrimSpace(block.Hash)),
		"height": strconv.FormatInt(block.Height, 10),
		"order":  strconv.FormatInt(block.Order, 10),
		"patch":  string(patchRaw),
	})
	return payload, nil
}

func (r *DashboardRuntime) nativeDAGAnnotateLiveForwardGaps(ctx context.Context, limit int, skippedOrders []uint64, sourceErr error) error {
	if !r.Ready() || len(skippedOrders) == 0 {
		return nil
	}
	limit = chainDAGWindowLimit(limit)
	raw, err := r.client.Get(ctx, nativeDAGLiveSnapshotKey).Result()
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return err
	}
	now := time.Now().UTC()
	payload["status"] = first(stringValue(payload, "status"), "ok")
	payload["best_effort"] = true
	payload["live_pipeline"] = first(stringValue(payload, "live_pipeline"), "redis-incremental")
	payload["live_pipeline_status"] = "best-effort-forward"
	payload["last_checked_at"] = now.Format(time.RFC3339)
	payload["last_checked_at_epoch"] = now.Unix()
	payload["last_forward_gap_at"] = now.Format(time.RFC3339)
	payload["last_forward_gap_count"] = len(skippedOrders)
	payload["last_forward_gap_orders"] = boundedUint64List(skippedOrders, 8)
	if sourceErr != nil {
		payload["source_error"] = sourceErr.Error()
	}
	coverage := mapValue(payload, "coverage")
	if coverage == nil {
		coverage = map[string]any{}
	}
	failures := int(int64Value(coverage["failures"]))
	failures += len(skippedOrders)
	coverage["requested"] = limit
	coverage["failures"] = failures
	coverage["complete"] = false
	payload["coverage"] = coverage
	warnings := stringSlice(payload["warnings"])
	warnings = appendUniqueWarning(warnings, fmt.Sprintf(
		"Native DAG skipped %d unavailable order(s) while keeping the live topology current.",
		len(skippedOrders),
	))
	if len(warnings) > 8 {
		warnings = warnings[len(warnings)-8:]
	}
	payload["warnings"] = warnings
	trimNativeDAGPayloadWindow(payload, limit)
	encoded, _ := json.Marshal(payload)
	if err := r.client.Set(ctx, nativeDAGLiveSnapshotKey, encoded, nativeDAGLiveSnapshotTTL).Err(); err != nil {
		return err
	}
	_ = r.emitDashboardEvent(ctx, "chain-dag", map[string]any{
		"reason":        "best-effort-forward-gap",
		"skipped_count": strconv.Itoa(len(skippedOrders)),
		"full":          "true",
	})
	return nil
}

func boundedUint64List(values []uint64, limit int) []uint64 {
	if limit < 1 || len(values) <= limit {
		return append([]uint64(nil), values...)
	}
	return append([]uint64(nil), values[len(values)-limit:]...)
}

func (r *DashboardRuntime) replaceNativeDAGLiveBlocks(ctx context.Context, blocks []nativeDAGBlock, limit int) error {
	limit = chainDAGWindowLimit(limit)
	old, _ := r.client.ZRange(ctx, nativeDAGLiveWindowKey, 0, -1).Result()
	pipe := r.client.Pipeline()
	for _, hash := range old {
		pipe.Del(ctx, nativeDAGLiveBlockPrefix+strings.ToLower(strings.TrimSpace(hash)))
	}
	pipe.Del(ctx, nativeDAGLiveWindowKey)
	pipe.Del(ctx, nativeDAGLiveLastOrderKey)
	var latestOrder int64
	for _, block := range blocks {
		hash := strings.ToLower(strings.TrimSpace(block.Hash))
		if hash == "" || block.Order <= 0 {
			continue
		}
		raw, _ := json.Marshal(block)
		pipe.Set(ctx, nativeDAGLiveBlockPrefix+hash, raw, 0)
		pipe.ZAdd(ctx, nativeDAGLiveWindowKey, redis.Z{Score: float64(block.Order), Member: hash})
		if block.Order > latestOrder {
			latestOrder = block.Order
		}
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	if err := r.trimNativeDAGLiveWindow(ctx, limit); err != nil {
		return err
	}
	if latestOrder > 0 {
		return r.client.Set(ctx, nativeDAGLiveLastOrderKey, strconv.FormatInt(latestOrder, 10), 0).Err()
	}
	return nil
}

func (r *DashboardRuntime) mergeNativeDAGLiveBlocks(ctx context.Context, blocks []nativeDAGBlock, limit int) error {
	limit = chainDAGWindowLimit(limit)
	var latestOrder int64
	pipe := r.client.Pipeline()
	for _, block := range blocks {
		hash := strings.ToLower(strings.TrimSpace(block.Hash))
		if hash == "" || block.Order <= 0 {
			continue
		}
		raw, _ := json.Marshal(block)
		pipe.Set(ctx, nativeDAGLiveBlockPrefix+hash, raw, 0)
		pipe.ZAdd(ctx, nativeDAGLiveWindowKey, redis.Z{Score: float64(block.Order), Member: hash})
		if block.Order > latestOrder {
			latestOrder = block.Order
		}
	}
	if latestOrder > 0 {
		pipe.Set(ctx, nativeDAGLiveLastOrderKey, strconv.FormatInt(latestOrder, 10), 0)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	return r.trimNativeDAGLiveWindow(ctx, limit)
}

func (r *DashboardRuntime) storeNativeDAGLiveBlock(ctx context.Context, block nativeDAGBlock, limit int) error {
	hash := strings.ToLower(strings.TrimSpace(block.Hash))
	if hash == "" || block.Order <= 0 {
		return nil
	}
	raw, _ := json.Marshal(block)
	pipe := r.client.Pipeline()
	pipe.Set(ctx, nativeDAGLiveBlockPrefix+hash, raw, 0)
	pipe.ZAdd(ctx, nativeDAGLiveWindowKey, redis.Z{Score: float64(block.Order), Member: hash})
	pipe.Set(ctx, nativeDAGLiveLastOrderKey, strconv.FormatInt(block.Order, 10), 0)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	return r.trimNativeDAGLiveWindow(ctx, limit)
}

func (r *DashboardRuntime) trimNativeDAGLiveWindow(ctx context.Context, limit int) error {
	excess := r.client.ZCard(ctx, nativeDAGLiveWindowKey).Val() - int64(limit)
	if excess <= 0 {
		return nil
	}
	old, err := r.client.ZRange(ctx, nativeDAGLiveWindowKey, 0, excess-1).Result()
	if err != nil {
		return err
	}
	pipe := r.client.Pipeline()
	for _, hash := range old {
		hash = strings.ToLower(strings.TrimSpace(hash))
		if hash == "" {
			continue
		}
		pipe.Del(ctx, nativeDAGLiveBlockPrefix+hash)
		pipe.ZRem(ctx, nativeDAGLiveWindowKey, hash)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (r *DashboardRuntime) nativeDAGLiveLastOrder(ctx context.Context) (uint64, error) {
	raw, err := r.client.Get(ctx, nativeDAGLiveLastOrderKey).Result()
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func (r *DashboardRuntime) nativeDAGTouchLiveSnapshot(ctx context.Context, limit int) error {
	if !r.Ready() {
		return errors.New("redis runtime unavailable")
	}
	raw, err := r.client.Get(ctx, nativeDAGLiveSnapshotKey).Result()
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return err
	}
	if nativeDAGLiveSnapshotSourceDegraded(payload) {
		now := time.Now().UTC()
		payload["last_checked_at"] = now.Format(time.RFC3339)
		payload["last_checked_at_epoch"] = now.Unix()
		trimNativeDAGPayloadWindow(payload, limit)
		updateSnapshotAge(payload)
		encoded, _ := json.Marshal(payload)
		return r.client.Set(ctx, nativeDAGLiveSnapshotKey, encoded, nativeDAGLiveSnapshotTTL).Err()
	}
	now := time.Now().UTC()
	payload["updated_at"] = now.Format(time.RFC3339)
	payload["updated_at_epoch"] = now.Unix()
	payload["age_seconds"] = 0
	payload["stale_after_seconds"] = 15
	payload["live_pipeline"] = first(stringValue(payload, "live_pipeline"), "redis-incremental")
	trimNativeDAGPayloadWindow(payload, limit)
	encoded, _ := json.Marshal(payload)
	return r.client.Set(ctx, nativeDAGLiveSnapshotKey, encoded, nativeDAGLiveSnapshotTTL).Err()
}

func nativeDAGLiveSnapshotSourceDegraded(payload map[string]any) bool {
	status := strings.ToLower(strings.TrimSpace(stringValue(payload, "status")))
	if status == "stale" || status == "error" || status == "degraded" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(stringValue(payload, "live_pipeline_status")), "source-degraded") {
		return true
	}
	coverage := mapValue(payload, "coverage")
	requested := number(first(mapString(coverage, "requested"), mapString(payload, "requested")))
	failures := number(first(mapString(coverage, "failures"), mapString(payload, "failures")))
	return requested > 0 && failures >= requested
}

func (r *DashboardRuntime) nativeDAGLiveBlocks(ctx context.Context, limit int) ([]nativeDAGBlock, error) {
	hashes, err := r.client.ZRevRange(ctx, nativeDAGLiveWindowKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		hash = strings.ToLower(strings.TrimSpace(hash))
		if hash == "" {
			continue
		}
		keys = append(keys, nativeDAGLiveBlockPrefix+hash)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	blocks := make([]nativeDAGBlock, 0, len(hashes))
	for _, value := range values {
		raw := strings.TrimSpace(fmt.Sprint(value))
		if raw == "" || raw == "<nil>" {
			continue
		}
		var block nativeDAGBlock
		if json.Unmarshal([]byte(raw), &block) == nil && strings.TrimSpace(block.Hash) != "" {
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

func (r *DashboardRuntime) nativeDAGBuildLiveSnapshot(ctx context.Context, url string, limit int, failures int) (map[string]any, error) {
	blocks, err := r.nativeDAGLiveBlocks(ctx, limit)
	if err != nil {
		return nil, err
	}
	tips, tipErr := nativeDAGTips(ctx, url)
	if tipErr != nil {
		tips = nativeDAGTipsResult{}
	}
	tipBlocks, tipFailures := r.nativeDAGTipBlocksCached(ctx, url, tips, blocks)
	failures += tipFailures
	if len(tipBlocks) > 0 {
		blocks = append(blocks, tipBlocks...)
	}
	payload := nativeDAGPayload(blocks, limit, failures, tips)
	payload["source"] = "native-rpc-live"
	payload["live_pipeline"] = "redis-incremental"
	if tipErr != nil {
		warnings := stringSlice(payload["warnings"])
		warnings = append(warnings, "Native DAG tips are unavailable: "+tipErr.Error())
		payload["warnings"] = warnings
	}
	return payload, nil
}

func (r *DashboardRuntime) nativeDAGTipBlocksCached(ctx context.Context, url string, tips nativeDAGTipsResult, existing []nativeDAGBlock) ([]nativeDAGBlock, int) {
	maxTipBlocks := intEnv("BDAG_CHAIN_DAG_TIP_BLOCKS", 32)
	if maxTipBlocks < 1 {
		return nil, 0
	}
	if maxTipBlocks > 96 {
		maxTipBlocks = 96
	}
	known := make(map[string]bool, len(existing))
	for _, block := range existing {
		if hash := strings.ToLower(strings.TrimSpace(block.Hash)); hash != "" {
			known[hash] = true
		}
	}
	orderedTips := make([]nativeDAGTip, 0, len(tips.Valid)+len(tips.Invalid))
	orderedTips = append(orderedTips, tips.Valid...)
	orderedTips = append(orderedTips, tips.Invalid...)
	sort.SliceStable(orderedTips, func(i, j int) bool {
		if orderedTips[i].Status == orderedTips[j].Status {
			return orderedTips[i].Height > orderedTips[j].Height
		}
		return orderedTips[i].Status == "valid"
	})
	selected := make([]string, 0, maxTipBlocks)
	for _, tip := range orderedTips {
		if len(selected) >= maxTipBlocks {
			break
		}
		hash := strings.ToLower(strings.TrimSpace(tip.Hash))
		if hash == "" || known[hash] {
			continue
		}
		selected = append(selected, hash)
	}
	out := make([]nativeDAGBlock, 0, len(selected))
	failures := 0
	missing := make([]string, 0, len(selected))
	if len(selected) > 0 {
		if cached, err := r.client.HMGet(ctx, nativeDAGTipBlockCacheKey, selected...).Result(); err == nil {
			for idx, value := range cached {
				raw := strings.TrimSpace(fmt.Sprint(value))
				if raw == "" || raw == "<nil>" {
					missing = append(missing, selected[idx])
					continue
				}
				var block nativeDAGBlock
				if json.Unmarshal([]byte(raw), &block) == nil && strings.TrimSpace(block.Hash) != "" {
					out = append(out, block)
					known[selected[idx]] = true
				} else {
					missing = append(missing, selected[idx])
				}
			}
		} else {
			missing = selected
		}
	}
	cacheUpdates := map[string]any{}
	for _, hash := range missing {
		if known[hash] {
			continue
		}
		var block nativeDAGBlock
		if err := nativeRPC(ctx, url, "getBlock", []any{hash, true, false, false}, &block); err != nil {
			failures++
			continue
		}
		if strings.TrimSpace(block.Hash) == "" {
			failures++
			continue
		}
		blocks := []nativeDAGBlock{block}
		_, coinbaseFailures := r.enrichNativeDAGCoinbase(ctx, url, blocks)
		block = blocks[0]
		failures += coinbaseFailures
		raw, _ := json.Marshal(block)
		cacheUpdates[hash] = string(raw)
		out = append(out, block)
		known[hash] = true
	}
	if len(cacheUpdates) > 0 {
		_ = r.client.HSet(ctx, nativeDAGTipBlockCacheKey, cacheUpdates).Err()
		_ = r.client.Expire(ctx, nativeDAGTipBlockCacheKey, nativeDAGTipBlockCacheTTL).Err()
	}
	return out, failures
}

func nativeDAGPatchFromPayload(payload map[string]any, hash string, limit int) map[string]any {
	hash = strings.ToLower(strings.TrimSpace(hash))
	nodes := []map[string]any{}
	for _, node := range listMaps(payload["nodes"]) {
		if strings.EqualFold(mapString(node, "hash"), hash) {
			nodes = append(nodes, node)
			break
		}
	}
	edges := []map[string]any{}
	for _, edge := range listMaps(payload["edges"]) {
		if strings.EqualFold(mapString(edge, "child"), hash) || strings.EqualFold(mapString(edge, "parent"), hash) {
			edges = append(edges, edge)
		}
	}
	return map[string]any{
		"mode":         stringValue(payload, "mode"),
		"source":       stringValue(payload, "source"),
		"best_effort":  truthyValue(payload["best_effort"]),
		"coverage":     mapValue(payload, "coverage"),
		"window_limit": chainDAGWindowLimit(limit),
		"nodes":        nodes,
		"edges":        edges,
	}
}

func trimNativeDAGPayloadWindow(payload map[string]any, limit int) {
	limit = chainDAGWindowLimit(limit)
	nodes := listMaps(payload["nodes"])
	if len(nodes) > limit {
		nodes = append([]map[string]any(nil), nodes...)
		sort.SliceStable(nodes, func(i, j int) bool {
			left, _ := strconv.ParseFloat(first(mapString(nodes[i], "layout_order"), mapString(nodes[i], "order"), mapString(nodes[i], "height")), 64)
			right, _ := strconv.ParseFloat(first(mapString(nodes[j], "layout_order"), mapString(nodes[j], "order"), mapString(nodes[j], "height")), 64)
			return left > right
		})
		nodes = nodes[:limit]
	}
	kept := map[string]bool{}
	for _, node := range nodes {
		if hash := strings.ToLower(strings.TrimSpace(first(mapString(node, "hash"), mapString(node, "id")))); hash != "" {
			kept[hash] = true
		}
	}
	edges := make([]map[string]any, 0, len(listMaps(payload["edges"])))
	for _, edge := range listMaps(payload["edges"]) {
		child := strings.ToLower(strings.TrimSpace(mapString(edge, "child")))
		parent := strings.ToLower(strings.TrimSpace(mapString(edge, "parent")))
		if !kept[child] {
			continue
		}
		if parent != "" && !kept[parent] && truthyValue(edge["known"]) {
			continue
		}
		edges = append(edges, edge)
	}
	payload["nodes"] = nodes
	payload["edges"] = edges
	payload["mode"] = first(stringValue(payload, "mode"), "native-dag-live")
	payload["source"] = first(stringValue(payload, "source"), "redis-native-dag-live")
	payload["live_pipeline"] = first(stringValue(payload, "live_pipeline"), "redis-incremental")
	window := mapValue(payload, "window")
	if window == nil {
		window = map[string]any{}
	}
	window["requested"] = limit
	window["available"] = len(nodes)
	window["truncated"] = len(nodes) >= limit
	payload["window"] = window
	coverage := mapValue(payload, "coverage")
	if coverage == nil {
		coverage = map[string]any{}
	}
	coverage["requested"] = limit
	coverage["available"] = len(nodes)
	coverage["complete"] = len(nodes) >= limit && !truthyValue(payload["best_effort"])
	payload["coverage"] = coverage
	if len(nodes) > 0 {
		payload["tip"] = nodes[0]
	}
}

func (r *DashboardRuntime) emitDashboardEvent(ctx context.Context, eventType string, fields map[string]any) error {
	if !r.Ready() {
		return errors.New("redis runtime unavailable")
	}
	values := map[string]any{"type": eventType}
	for key, value := range fields {
		values[key] = value
	}
	if _, ok := values["updated_at"]; !ok {
		values["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	return r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: dashboardEventStreamKey,
		MaxLen: 1000,
		Approx: true,
		Values: values,
	}).Err()
}

func chainDAGWindowLimit(limit int) int {
	if limit <= 0 {
		limit = defaultNativeDAGWindowLimit
	}
	if limit > maxNativeDAGWindowLimit {
		limit = maxNativeDAGWindowLimit
	}
	return limit
}

func nativeDAGBlocksByOrder(ctx context.Context, url string, start uint64, end uint64) ([]nativeDAGBlock, int) {
	if end < start {
		return nil, 0
	}
	total := int(end - start + 1)
	blocks := make([]nativeDAGBlock, total)
	ok := make([]bool, total)
	workers := intEnv("BDAG_CHAIN_DAG_RPC_WORKERS", 8)
	if workers < 1 {
		workers = 1
	}
	if workers > 16 {
		workers = 16
	}
	if nodeRPCPriorityFromContext(ctx) == nodeRPCPriorityLow && workers > 1 {
		workers = 1
	}
	if workers > total {
		workers = total
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				order := start + uint64(idx)
				var block nativeDAGBlock
				if err := nativeRPC(ctx, url, "getBlockByOrder", []any{order, true, false, false}, &block); err != nil {
					continue
				}
				if strings.TrimSpace(block.Hash) == "" {
					continue
				}
				if block.Order == 0 {
					block.Order = int64(order)
				}
				blocks[idx] = block
				ok[idx] = true
			}
		}()
	}
	for idx := range blocks {
		if nodeRPCPriorityFromContext(ctx) == nodeRPCPriorityLow && dashboardNodeRPCBudget.backoffActive() {
			break
		}
		jobs <- idx
	}
	close(jobs)
	wg.Wait()
	out := make([]nativeDAGBlock, 0, len(blocks))
	failures := 0
	for idx, block := range blocks {
		if !ok[idx] {
			failures++
			continue
		}
		out = append(out, block)
	}
	return out, failures
}

func nativeDAGBlockByOrder(ctx context.Context, url string, order uint64) (nativeDAGBlock, error) {
	var block nativeDAGBlock
	if err := nativeRPC(ctx, url, "getBlockByOrder", []any{order, true, false, false}, &block); err != nil {
		return nativeDAGBlock{}, err
	}
	if strings.TrimSpace(block.Hash) == "" {
		return nativeDAGBlock{}, fmt.Errorf("getBlockByOrder(%d) returned empty hash", order)
	}
	if block.Order == 0 {
		block.Order = int64(order)
	}
	return block, nil
}

func nativeDAGTips(ctx context.Context, url string) (nativeDAGTipsResult, error) {
	var tips nativeDAGTipsResult
	if err := nativeRPC(ctx, url, "tips", []any{}, &tips); err != nil {
		return nativeDAGTipsResult{}, err
	}
	for i := range tips.Valid {
		tips.Valid[i].Status = "valid"
	}
	for i := range tips.Invalid {
		tips.Invalid[i].Status = "invalid"
	}
	return tips, nil
}

func nativeDAGTipBlocks(ctx context.Context, url string, tips nativeDAGTipsResult, existing []nativeDAGBlock) ([]nativeDAGBlock, int) {
	maxTipBlocks := intEnv("BDAG_CHAIN_DAG_TIP_BLOCKS", 32)
	if maxTipBlocks < 1 {
		return nil, 0
	}
	if maxTipBlocks > 96 {
		maxTipBlocks = 96
	}
	known := make(map[string]bool, len(existing))
	for _, block := range existing {
		if hash := strings.ToLower(strings.TrimSpace(block.Hash)); hash != "" {
			known[hash] = true
		}
	}
	orderedTips := make([]nativeDAGTip, 0, len(tips.Valid)+len(tips.Invalid))
	orderedTips = append(orderedTips, tips.Valid...)
	orderedTips = append(orderedTips, tips.Invalid...)
	sort.SliceStable(orderedTips, func(i, j int) bool {
		if orderedTips[i].Status == orderedTips[j].Status {
			return orderedTips[i].Height > orderedTips[j].Height
		}
		return orderedTips[i].Status == "valid"
	})
	out := make([]nativeDAGBlock, 0, maxTipBlocks)
	failures := 0
	for _, tip := range orderedTips {
		if len(out) >= maxTipBlocks {
			break
		}
		hash := strings.ToLower(strings.TrimSpace(tip.Hash))
		if hash == "" || known[hash] {
			continue
		}
		var block nativeDAGBlock
		if err := nativeRPC(ctx, url, "getBlock", []any{tip.Hash, true, false, false}, &block); err != nil {
			failures++
			continue
		}
		if strings.TrimSpace(block.Hash) == "" {
			failures++
			continue
		}
		if block.Height == 0 {
			block.Height = tip.Height
		}
		out = append(out, block)
		known[hash] = true
	}
	return out, failures
}

func (r *DashboardRuntime) enrichNativeDAGCoinbase(ctx context.Context, url string, blocks []nativeDAGBlock) (int, int) {
	if !envBool("BDAG_CHAIN_DAG_COINBASE_ENABLED", true) || !r.Ready() || len(blocks) == 0 {
		return 0, 0
	}
	cacheKey := "bdag:chain:coinbase_by_hash"
	hashes := make([]string, 0, len(blocks))
	indexByHash := make(map[string][]int, len(blocks))
	for idx, block := range blocks {
		hash := strings.ToLower(strings.TrimSpace(block.Hash))
		if hash == "" {
			continue
		}
		if first(block.Producer, block.Miner, block.Author, block.CoinbaseAddress, block.Coinbase) != "" {
			continue
		}
		if _, exists := indexByHash[hash]; !exists {
			hashes = append(hashes, hash)
		}
		indexByHash[hash] = append(indexByHash[hash], idx)
	}
	if len(hashes) == 0 {
		return 0, 0
	}
	if cached, err := r.client.HMGet(ctx, cacheKey, hashes...).Result(); err == nil {
		for idx, value := range cached {
			coinbase := strings.TrimSpace(fmt.Sprint(value))
			if coinbase == "" || coinbase == "<nil>" {
				continue
			}
			for _, blockIdx := range indexByHash[hashes[idx]] {
				blocks[blockIdx].CoinbaseAddress = coinbase
			}
		}
	}
	missing := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		known := false
		for _, blockIdx := range indexByHash[hash] {
			if strings.TrimSpace(blocks[blockIdx].CoinbaseAddress) != "" {
				known = true
				break
			}
		}
		if !known {
			missing = append(missing, hash)
		}
	}
	maxLookups := intEnv("BDAG_CHAIN_DAG_COINBASE_LOOKUPS", 48)
	if maxLookups < 0 {
		maxLookups = 0
	}
	if maxLookups > 160 {
		maxLookups = 160
	}
	if len(missing) > maxLookups {
		missing = missing[:maxLookups]
	}
	if len(missing) == 0 {
		return 0, 0
	}
	workers := intEnv("BDAG_CHAIN_DAG_COINBASE_WORKERS", 4)
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	if workers > len(missing) {
		workers = len(missing)
	}
	type lookupResult struct {
		hash     string
		coinbase string
		err      error
	}
	jobs := make(chan string)
	results := make(chan lookupResult, len(missing))
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for hash := range jobs {
				var coinbase string
				err := nativeRPC(ctx, url, "getCoinbaseAddress", []any{hash}, &coinbase)
				results <- lookupResult{hash: hash, coinbase: strings.TrimSpace(coinbase), err: err}
			}
		}()
	}
	for _, hash := range missing {
		jobs <- hash
	}
	close(jobs)
	wg.Wait()
	close(results)
	cacheUpdates := map[string]any{}
	lookups := 0
	failures := 0
	for result := range results {
		if result.err != nil || result.coinbase == "" {
			failures++
			continue
		}
		lookups++
		cacheUpdates[result.hash] = result.coinbase
		for _, blockIdx := range indexByHash[result.hash] {
			blocks[blockIdx].CoinbaseAddress = result.coinbase
		}
	}
	if len(cacheUpdates) > 0 {
		_ = r.client.HSet(ctx, cacheKey, cacheUpdates).Err()
		_ = r.client.Expire(ctx, cacheKey, 24*time.Hour).Err()
	}
	return lookups, failures
}

func nativeDAGPayload(blocks []nativeDAGBlock, requested int, failures int, tips nativeDAGTipsResult) map[string]any {
	now := time.Now().UTC()
	nodes := make([]map[string]any, 0, len(blocks))
	edges := make([]map[string]any, 0, len(blocks)*2)
	knownHashes := make(map[string]bool, len(blocks))
	tipStatusByHash := make(map[string]string, len(tips.Valid)+len(tips.Invalid))
	for _, tip := range tips.Valid {
		if hash := strings.ToLower(strings.TrimSpace(tip.Hash)); hash != "" {
			tipStatusByHash[hash] = "valid"
		}
	}
	for _, tip := range tips.Invalid {
		if hash := strings.ToLower(strings.TrimSpace(tip.Hash)); hash != "" {
			tipStatusByHash[hash] = "invalid"
		}
	}
	localProducers := localProducerSet()
	for _, block := range blocks {
		if block.Hash != "" {
			knownHashes[strings.ToLower(block.Hash)] = true
		}
	}
	orderOffset, hasOrderOffset := nativeDAGOrderOffset(blocks)
	estimatedOrders := 0
	sort.SliceStable(blocks, func(i, j int) bool {
		left, _ := nativeDAGLayoutOrder(blocks[i], orderOffset, hasOrderOffset)
		right, _ := nativeDAGLayoutOrder(blocks[j], orderOffset, hasOrderOffset)
		return left > right
	})
	for _, block := range blocks {
		hash := strings.TrimSpace(block.Hash)
		parents := normalizedHashList(block.Parents)
		children := normalizedHashList(block.Children)
		layoutOrder, orderEstimated := nativeDAGLayoutOrder(block, orderOffset, hasOrderOffset)
		if orderEstimated {
			estimatedOrders++
		}
		knownParents := 0
		for _, parent := range parents {
			if knownHashes[strings.ToLower(parent)] {
				knownParents++
			}
			edges = append(edges, map[string]any{
				"child":         hash,
				"parent":        parent,
				"type":          "dag_parent",
				"known":         knownHashes[strings.ToLower(parent)],
				"out_of_window": !knownHashes[strings.ToLower(parent)],
			})
		}
		producer, producerSource := nativeDAGProducer(block)
		producerPending := producer == ""
		tipStatus := tipStatusByHash[strings.ToLower(hash)]
		dagSet := "settled"
		if tipStatus == "valid" {
			dagSet = "tip-valid"
		} else if tipStatus == "invalid" {
			dagSet = "tip-invalid"
		}
		nodes = append(nodes, map[string]any{
			"id":                   hash,
			"hash":                 hash,
			"short_hash":           shortHash(hash),
			"height":               block.Height,
			"order":                block.Order,
			"layout_order":         layoutOrder,
			"order_estimated":      orderEstimated,
			"timestamp":            block.Time,
			"producer":             producer,
			"producer_short":       shortAddress(producer),
			"producer_color":       stableProducerColor(producer),
			"producer_source":      producerSource,
			"producer_pending":     producerPending,
			"local_pool":           localProducers[producer],
			"tip":                  tipStatus != "",
			"tip_status":           tipStatus,
			"dag_set":              dagSet,
			"parent_count":         len(parents),
			"known_parent_count":   knownParents,
			"missing_parent_count": len(parents) - knownParents,
			"main_parent_hash":     firstString(parents),
			"parents":              parents,
			"children":             children,
			"child_count":          len(children),
			"linear_path":          true,
			"multi_parent":         len(parents) > 1,
		})
	}
	for i := 0; i+1 < len(nodes); i++ {
		childHash := stringValue(nodes[i], "hash")
		parentHash := stringValue(nodes[i+1], "hash")
		if childHash == "" || parentHash == "" {
			continue
		}
		edges = append(edges, map[string]any{
			"child":  childHash,
			"parent": parentHash,
			"type":   "linear_path",
			"known":  true,
			"glow":   true,
		})
	}
	status := "ok"
	if len(nodes) == 0 {
		status = "warming"
	} else if failures > 0 {
		status = "degraded"
	}
	warnings := []string{}
	if failures > 0 {
		warnings = append(warnings, fmt.Sprintf("Skipped %d native DAG block RPC results while building the topology window.", failures))
	}
	if estimatedOrders > 0 {
		warnings = append(warnings, fmt.Sprintf("Estimated layout order for %d native DAG tip blocks whose RPC payload omitted order.", estimatedOrders))
	}
	bestEffort := failures > 0 || estimatedOrders > 0
	payload := map[string]any{
		"status":              status,
		"mode":                "native-dag-linear-spine",
		"source":              "native-rpc",
		"updated_at":          now.Format(time.RFC3339),
		"updated_at_epoch":    now.Unix(),
		"age_seconds":         0,
		"stale_after_seconds": 15,
		"window": map[string]any{
			"requested": requested,
			"available": len(nodes),
			"truncated": len(nodes) >= requested,
		},
		"tips": map[string]any{
			"valid":   len(tips.Valid),
			"invalid": len(tips.Invalid),
		},
		"order_estimated_count": estimatedOrders,
		"best_effort":           bestEffort,
		"coverage": map[string]any{
			"requested": requested,
			"available": len(nodes),
			"complete":  len(nodes) >= requested && failures == 0,
			"failures":  failures,
			"estimated": estimatedOrders,
		},
		"nodes":    nodes,
		"edges":    edges,
		"warnings": warnings,
	}
	if len(nodes) > 0 {
		payload["tip"] = nodes[0]
	}
	return payload
}

func nativeDAGStatusOrder(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if tip := mapValue(payload, "tip"); tip != nil {
		return first(mapString(tip, "order"), mapString(tip, "layout_order"), mapString(tip, "height"))
	}
	return first(stringValue(payload, "latest_order"), stringValue(payload, "latest_block"))
}

func nativeDAGOrderOffset(blocks []nativeDAGBlock) (int64, bool) {
	offsets := make([]int64, 0, len(blocks))
	for _, block := range blocks {
		if block.Order <= 0 || block.Height <= 0 {
			continue
		}
		offsets = append(offsets, block.Order-block.Height)
	}
	if len(offsets) == 0 {
		return 0, false
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	return offsets[len(offsets)/2], true
}

func nativeDAGLayoutOrder(block nativeDAGBlock, offset int64, hasOffset bool) (int64, bool) {
	if block.Order > 0 {
		return block.Order, false
	}
	if block.Height > 0 && hasOffset {
		return block.Height + offset, true
	}
	if block.Height > 0 {
		return block.Height, true
	}
	return 0, true
}

func nativeDAGProducer(block nativeDAGBlock) (string, string) {
	for _, candidate := range []struct {
		value  string
		source string
	}{
		{block.Producer, "native-block"},
		{block.Miner, "native-block"},
		{block.Author, "native-block"},
		{block.CoinbaseAddress, "coinbase-reward"},
		{block.Coinbase, "coinbase-reward"},
	} {
		producer := strings.ToLower(strings.TrimSpace(candidate.value))
		if producer != "" {
			return producer, candidate.source
		}
	}
	return "", "pending-reward-attribution"
}

func normalizedHashList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, "null") || value == "0x0000000000000000000000000000000000000000000000000000000000000000" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func shortHash(hash string) string {
	hash = strings.TrimSpace(hash)
	if len(hash) <= 14 {
		return hash
	}
	return hash[:8] + "..." + hash[len(hash)-6:]
}

func (r *DashboardRuntime) hostPayload(ctx context.Context) (map[string]any, error) {
	return r.readJSONKey(ctx, "bdag:host:snapshot")
}

func (r *DashboardRuntime) statusPayload(ctx context.Context) (map[string]any, error) {
	if !r.Ready() {
		return nil, errors.New("redis runtime unavailable")
	}
	global, _ := r.nativeDAGLivePayload(ctx, defaultGlobalBlockWindow)
	pool, _ := r.poolPayload(ctx)
	poolJobState, _ := r.poolJobStatePayload(ctx)
	chain, _ := r.chainPayload(ctx)
	asicPerformance, _ := r.asicPerformancePayload(ctx)
	asicTelemetry, _ := r.asicTelemetryPayload(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	payload := map[string]any{
		"status":     "ok",
		"overall":    "degraded",
		"mode":       "waiting_for_live_data",
		"updated_at": now,
		"source":     "redis-live",
		"containers": map[string]any{
			"node": map[string]any{"running": chain != nil},
			"pool": map[string]any{"running": pool != nil},
		},
		"pool_health":  map[string]any{},
		"pool_metrics": map[string]any{},
		"sync_progress": map[string]any{
			"status": "unknown",
		},
	}
	if miningAddress := env("MINING_POOL_ADDRESS", ""); miningAddress != "" {
		payload["mining_address"] = miningAddress
		payload["pool_health"].(map[string]any)["configured_worker_user"] = miningAddress
	}
	if chain != nil {
		syncProgress := payload["sync_progress"].(map[string]any)
		syncProgress["status"] = "unknown"
		syncProgress["source"] = "redis-chain-snapshot"
		syncProgress["current_block"] = stringValue(chain, "latest_block")
		syncProgress["evm_block_count"] = stringValue(chain, "latest_block")
		syncProgress["chain_block_count"] = stringValue(chain, "chain_latest_block")
		payload["chain_block_count"] = first(stringValue(chain, "chain_latest_block"), stringValue(chain, "latest_block"))
	}
	if global != nil {
		globalTip := nativeDAGStatusOrder(global)
		syncProgress := payload["sync_progress"].(map[string]any)
		syncProgress["current_block"] = globalTip
		syncProgress["evm_block_count"] = globalTip
		syncProgress["chain_block_count"] = globalTip
		payload["chain_block_count"] = globalTip
	}
	if native := nativeSyncProgressCached(ctx); native != nil {
		syncProgress := payload["sync_progress"].(map[string]any)
		for key, value := range native {
			syncProgress[key] = value
		}
		if value := stringValue(native, "chain_block_count"); value != "" {
			payload["chain_block_count"] = value
		}
	}
	if minerHealth := r.minerHealthPayload(ctx); minerHealth != nil {
		payload["miner_health"] = minerHealth
	}
	if asicPerformance != nil {
		payload["asic_performance"] = asicPerformance
		if minerHealth, ok := payload["miner_health"].(map[string]any); ok {
			mergeASICPerformanceIntoMinerHealth(minerHealth, asicPerformance)
		} else if minerHealth := minerHealthFromASICPerformance(asicPerformance); minerHealth != nil {
			payload["miner_health"] = minerHealth
		}
	}
	if asicTelemetry != nil {
		payload["asic_telemetry"] = asicTelemetry
		if minerHealth, ok := payload["miner_health"].(map[string]any); ok {
			mergeASICTelemetryIntoMinerHealth(minerHealth, asicTelemetry)
		}
	}
	if pool != nil {
		metrics := mapValue(pool, "data")
		poolHealth := payload["pool_health"].(map[string]any)
		payload["pool_metrics"] = metrics
		active := number(mapString(metrics, "active_connections"))
		ready := number(mapString(metrics, "ready_miners"))
		acceptedBlocks := number(mapString(metrics, "accepted_block_submissions"))
		submitErrors := number(mapString(metrics, "rejected_block_submissions"))
		validShares := number(mapString(metrics, "shares_accepted_total"))
		poolHealth["valid_share_count"] = decimalInt(validShares)
		poolHealth["block_submit_success_count"] = decimalInt(acceptedBlocks)
		poolHealth["accepted_block_count"] = decimalInt(acceptedBlocks)
		poolHealth["block_submit_error_count"] = decimalInt(submitErrors)
		poolHealth["stale_jobs"] = number(mapString(metrics, "stale_miners")) > 0
		poolHealth["source_job_health"] = map[string]any{
			"authorized_miners": decimalInt(number(mapString(metrics, "authorized_miners"))),
			"ready_miners":      decimalInt(ready),
		}
		templateHealth := poolTemplateHealthFromMetrics(metrics)
		if len(templateHealth) > 0 {
			payload["template_health"] = templateHealth
			poolHealth["template_health"] = templateHealth
		}
		templateSafety := poolTemplateSafetyFromMetrics(metrics)
		if len(templateSafety) > 0 {
			payload["template_safety"] = templateSafety
			poolHealth["template_safety"] = templateSafety
		}
		sourceHealth := poolSelectedBackendHealthFromMetrics(metrics)
		if len(sourceHealth) > 0 {
			poolHealth["selected_backend_source_health"] = sourceHealth
		}
		if mineable, ok := metricBoolValue(metrics, "backend_mineable"); ok {
			poolHealth["source_selected_backend_mineable"] = mineable
		}
		if submitReady, ok := metricBoolValue(metrics, "backend_submit_ready"); ok {
			poolHealth["source_selected_backend_submit_ready"] = submitReady
		}
		if p2pFresh, ok := metricBoolValue(metrics, "backend_p2p_mining_fresh"); ok {
			poolHealth["source_selected_backend_p2p_fresh"] = p2pFresh
		}
		if minerHealth, ok := payload["miner_health"].(map[string]any); ok {
			applyPoolReadinessToManagedASICRows(minerHealth, metrics)
		}
		syncProgress := payload["sync_progress"].(map[string]any)
		syncBlockedByProgress := syncProgressBlocksMining(syncProgress)
		backendMiningSafe := poolBackendMiningSafe(metrics, syncProgress)
		syncBlocked := syncBlockedByProgress && !backendMiningSafe
		minerReady := active > 0 || ready > 0
		submitReady := minerReady && backendMiningSafe
		payload["can_accept_shares"] = minerReady && !syncBlocked
		payload["can_submit_blocks"] = submitReady
		payload["can_mine"] = minerReady && !syncBlocked && submitReady
		advisorySync := syncBlockedByProgress && backendMiningSafe
		if advisorySync {
			markMiningAdvisorySync(syncProgress)
		}
		mode, overall := liveStatusModeAndOverall(syncBlocked, syncProgress, minerReady, submitReady)
		payload["mode"] = mode
		payload["overall"] = overall
	}
	if poolJobState != nil {
		payload["pool_job_state"] = poolJobState
		poolHealth := payload["pool_health"].(map[string]any)
		poolHealth["job_state_status"] = stringValue(poolJobState, "status")
		poolHealth["job_state_reason"] = stringValue(poolJobState, "reason_code")
		poolHealth["current_template_seq"] = stringValue(poolJobState, "current_template_seq")
		poolHealth["last_broadcast_age_ms"] = stringValue(poolJobState, "last_broadcast_age_ms")
		if jobReady := stringValue(poolJobState, "ready_connections"); jobReady != "" {
			poolHealth["source_job_health"] = map[string]any{
				"authorized_miners": stringValue(poolJobState, "authorized_connections"),
				"ready_miners":      jobReady,
			}
		}
	}
	if _, ok := payload["asic_performance"]; !ok {
		if derived := derivedASICPerformancePayload(poolJobState, firstNestedMap(payload, []string{"miner_health"})); derived != nil {
			r.attachASICHashrateHistory(ctx, derived)
			payload["asic_performance"] = derived
			if minerHealth, ok := payload["miner_health"].(map[string]any); ok {
				mergeASICPerformanceIntoMinerHealth(minerHealth, derived)
			} else if minerHealth := minerHealthFromASICPerformance(derived); minerHealth != nil {
				payload["miner_health"] = minerHealth
			}
		}
	}
	runtimeWarnings := appendUniqueStrings(nil, watchdogStatusWarnings()...)
	if minerHealth, ok := payload["miner_health"].(map[string]any); ok {
		runtimeWarnings = appendUniqueStrings(runtimeWarnings, minerHealthASICStallWarnings(minerHealth)...)
	}
	if len(runtimeWarnings) > 0 {
		payload["warnings"] = appendUniqueStrings(stringSlice(payload["warnings"]), runtimeWarnings...)
		if strings.EqualFold(stringify(payload["overall"]), "ok") {
			payload["overall"] = "degraded"
		}
	}
	return payload, nil
}

func watchdogStatusWarnings() []string {
	path := strings.TrimSpace(env("BDAG_WATCHDOG_STATE_FILE", ""))
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return nil
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil
	}
	return watchdogHardwarePowerCycleWarningsFromState(state)
}

func watchdogHardwarePowerCycleWarningsFromState(state map[string]any) []string {
	reason := firstStringOr(stringSlice(state["last_share_warnings"]), "watchdog staged recovery reached hardware-power-cycle-required")
	rows := listMaps(state["last_asic_hardware_power_cycle_required"])
	if !strings.EqualFold(stringify(state["last_status"]), "asic_hardware_power_cycle_required") && len(rows) == 0 {
		return nil
	}
	warnings := []string{}
	for _, row := range rows {
		label := first(mapString(row, "mac"), mapString(row, "identity_key"), mapString(row, "ip"), "managed ASIC")
		if ip := mapString(row, "ip"); ip != "" && !strings.Contains(label, ip) {
			label += " at " + ip
		}
		warnings = append(warnings, "ASIC "+label+" requires hardware power-cycle: "+compactDashboardWarning(reason))
	}
	if len(warnings) == 0 {
		warnings = append(warnings, "ASIC hardware power-cycle required: "+compactDashboardWarning(reason))
	}
	return appendUniqueStrings(nil, warnings...)
}

func minerHealthASICStallWarnings(minerHealth map[string]any) []string {
	warnings := []string{}
	for _, rawRows := range []any{minerHealth["managed_miners"], minerHealth["miners"]} {
		for _, row := range listMaps(rawRows) {
			deviceType := strings.ToLower(mapString(row, "device_type"))
			if deviceType != "" && deviceType != "asic" {
				continue
			}
			if mapString(row, "managed") != "true" && mapString(row, "configured") != "true" {
				continue
			}
			if mapString(row, "ready") == "true" || mapString(row, "pool_active") == "true" || mapString(row, "work_pool_active") == "true" {
				continue
			}
			status := strings.ToLower(first(mapString(row, "status"), mapString(row, "health")))
			if status != "down" && status != "no-stratum" && status != "degraded" {
				continue
			}
			issueText := strings.ToLower(strings.Join([]string{
				mapString(row, "issue"),
				mapString(row, "debug_error"),
				mapString(row, "device_telemetry_errors"),
				mapString(row, "telemetry_errors"),
			}, " "))
			if !strings.Contains(issueText, "no-request") &&
				!strings.Contains(issueText, "connection reset") &&
				!strings.Contains(issueText, "eof") &&
				!strings.Contains(issueText, "cgminer") &&
				!strings.Contains(issueText, "/mcb/pools") {
				continue
			}
			label := first(mapString(row, "mac"), mapString(row, "device_id"), mapString(row, "ip"), "managed ASIC")
			if ip := mapString(row, "ip"); ip != "" && !strings.Contains(label, ip) {
				label += " at " + ip
			}
			warnings = append(warnings, "ASIC "+label+" is API-degraded and share-dead; staged watchdog recovery may require hardware power-cycle.")
		}
	}
	return appendUniqueStrings(nil, warnings...)
}

func firstStringOr(values []string, fallback string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return fallback
}

func compactDashboardWarning(value string) string {
	text := strings.Join(strings.Fields(value), " ")
	if len(text) <= 360 {
		return text
	}
	return text[:357] + "..."
}

func liveStatusModeAndOverall(syncBlocked bool, syncProgress map[string]any, minerReady bool, submitReady bool) (string, string) {
	if syncBlocked {
		if lower(stringValue(syncProgress, "status")) == "syncing" || syncProgress["chain_syncing"] == true {
			return "node_syncing", "degraded"
		}
		return "sync_blocked", "degraded"
	}
	if minerReady {
		if submitReady {
			return "mining", "ok"
		}
		return "mining", "degraded"
	}
	return "ready_no_miners", "degraded"
}

func poolBackendMiningSafe(metrics map[string]any, syncProgress map[string]any) bool {
	if number(mapString(metrics, "active_connections")) <= 0 {
		return false
	}
	backendP2PFreshPresent := metricPresent(metrics, "backend_p2p_mining_fresh")
	backendP2PFresh := number(mapString(metrics, "backend_p2p_mining_fresh")) > 0
	nativeProgressSafe := nativeProgressMiningSafe(syncProgress)
	if backendP2PFreshPresent && !backendP2PFresh {
		return false
	}
	if !backendP2PFresh && !nativeProgressSafe {
		return false
	}
	if metricPresent(metrics, "backend_peer_lead_blocks") && number(mapString(metrics, "backend_peer_lead_blocks")) > 10 {
		return false
	}
	if metricPresent(metrics, "backend_template_coinbase_valid") && number(mapString(metrics, "backend_template_coinbase_valid")) <= 0 {
		return false
	}
	if number(mapString(metrics, "backend_mineable")) > 0 && number(mapString(metrics, "backend_submit_ready")) > 0 {
		return true
	}
	if metricPresent(metrics, "template_age_seconds") && number(mapString(metrics, "template_age_seconds")) > 15 {
		return false
	}
	if number(mapString(metrics, "accepted_block_submissions_delta")) > 0 || metrics["accepted_block_recent"] == true {
		return true
	}
	return false
}

func poolTemplateHealthFromMetrics(metrics map[string]any) map[string]any {
	health := map[string]any{}
	if mineable, ok := metricBoolValue(metrics, "backend_mineable"); ok {
		health["mineable_now"] = mineable
	}
	if submitReady, ok := metricBoolValue(metrics, "backend_submit_ready"); ok {
		health["submit_ready"] = submitReady
	}
	if p2pFresh, ok := metricBoolValue(metrics, "backend_p2p_mining_fresh"); ok {
		health["p2p_mining_fresh"] = p2pFresh
	}
	if coinbaseValid, ok := metricBoolValue(metrics, "backend_template_coinbase_valid"); ok {
		health["template_coinbase_valid"] = coinbaseValid
	}
	if metricPresent(metrics, "template_age_seconds") {
		health["template_age_seconds"] = mapString(metrics, "template_age_seconds")
	}
	if metricPresent(metrics, "ready_miners") {
		health["ready_miner_job_count"] = decimalInt(number(mapString(metrics, "ready_miners")))
	}
	health["reason_code"] = templateHealthReasonCode(health)
	return health
}

func poolTemplateSafetyFromMetrics(metrics map[string]any) map[string]any {
	safety := map[string]any{}
	if coinbaseValid, ok := metricBoolValue(metrics, "backend_template_coinbase_valid"); ok {
		safety["template_coinbase_valid"] = coinbaseValid
	}
	if metricPresent(metrics, "template_coinbase_reject_total") {
		safety["zero_coinbase_reject_total"] = decimalInt(number(mapString(metrics, "template_coinbase_reject_total")))
	} else if metricPresent(metrics, "backend_template_coinbase_valid") {
		safety["zero_coinbase_reject_total"] = "0"
	}
	if metricPresent(metrics, "template_local_stale_ratio") {
		safety["local_stale_ratio"] = mapString(metrics, "template_local_stale_ratio")
	}
	if metricPresent(metrics, "ready_miners") {
		safety["ready_miner_job_count"] = decimalInt(number(mapString(metrics, "ready_miners")))
	}
	return safety
}

func poolSelectedBackendHealthFromMetrics(metrics map[string]any) map[string]any {
	health := map[string]any{}
	if mineable, ok := metricBoolValue(metrics, "backend_mineable"); ok {
		health["node_mineable"] = mineable
		health["mineable_now"] = mineable
	}
	if submitReady, ok := metricBoolValue(metrics, "backend_submit_ready"); ok {
		health["node_submit_ready"] = submitReady
		health["submit_ready"] = submitReady
	}
	if p2pFresh, ok := metricBoolValue(metrics, "backend_p2p_mining_fresh"); ok {
		health["node_p2p_mining_fresh"] = p2pFresh
	}
	if coinbaseValid, ok := metricBoolValue(metrics, "backend_template_coinbase_valid"); ok {
		health["node_template_coinbase_valid"] = coinbaseValid
		health["template_coinbase_valid"] = coinbaseValid
	}
	if metricPresent(metrics, "backend_peer_lead_blocks") {
		health["node_p2p_best_peer_lead_blocks"] = mapString(metrics, "backend_peer_lead_blocks")
	}
	if metricPresent(metrics, "template_age_seconds") {
		health["node_template_age_seconds"] = mapString(metrics, "template_age_seconds")
	}
	return health
}

func metricBoolValue(metrics map[string]any, key string) (bool, bool) {
	if !metricPresent(metrics, key) {
		return false, false
	}
	return number(mapString(metrics, key)) > 0, true
}

func templateHealthReasonCode(health map[string]any) string {
	if valid, ok := health["template_coinbase_valid"].(bool); ok && !valid {
		return "zero-template-coinbase"
	}
	if ready, ok := health["submit_ready"].(bool); ok && !ready {
		return "submit-not-ready"
	}
	if mineable, ok := health["mineable_now"].(bool); ok && !mineable {
		return "not-mineable"
	}
	return "ok"
}

func metricPresent(values map[string]any, key string) bool {
	if values == nil {
		return false
	}
	value, ok := values[key]
	if !ok || value == nil {
		return false
	}
	return strings.TrimSpace(fmt.Sprint(value)) != ""
}

func nativeProgressMiningSafe(syncProgress map[string]any) bool {
	if syncProgress == nil {
		return false
	}
	if lower(stringValue(syncProgress, "status")) != "synced" {
		return false
	}
	if syncProgress["native_is_current"] != true {
		return false
	}
	if syncProgress["chain_syncing"] == true && syncProgress["mining_advisory_sync"] != true && syncProgress["evm_chain_syncing"] != true {
		return false
	}
	if number(mapString(syncProgress, "remaining_blocks")) > 0 {
		return false
	}
	if number(mapString(syncProgress, "p2p_network_gap")) > 10 || number(mapString(syncProgress, "peer_mainorder_gap")) > 10 {
		return false
	}
	return true
}

func markMiningAdvisorySync(syncProgress map[string]any) {
	syncProgress["mining_advisory_sync"] = true
	if syncProgress["chain_syncing"] == true {
		syncProgress["chain_syncing"] = false
		syncProgress["evm_chain_syncing"] = true
		if _, ok := syncProgress["evm_sync_advisory"]; !ok {
			syncProgress["evm_sync_advisory"] = "sync signal is advisory while native mining safety is proven"
		}
	}
}

func syncProgressBlocksMining(syncProgress map[string]any) bool {
	status := lower(stringValue(syncProgress, "status"))
	if nativeProgressCurrent(syncProgress) {
		return false
	}
	if syncProgress["chain_syncing"] == true {
		return true
	}
	return status != "" && status != "synced"
}

func nativeProgressCurrent(syncProgress map[string]any) bool {
	if syncProgress["native_is_current"] != true {
		return false
	}
	if lower(stringValue(syncProgress, "status")) == "p2p_down" {
		return false
	}
	if number(mapString(syncProgress, "p2p_network_gap")) > 0 || number(mapString(syncProgress, "peer_mainorder_gap")) > 0 {
		return false
	}
	return true
}

func (r *DashboardRuntime) minerHealthPayload(ctx context.Context) map[string]any {
	if r == nil || !r.Ready() {
		return nil
	}
	raw, err := r.client.HGetAll(ctx, "bdag:miners:by_mac").Result()
	if err != nil || len(raw) == 0 {
		return nil
	}
	events := make([]map[string]any, 0, len(raw))
	for _, value := range raw {
		var event map[string]any
		if err := json.Unmarshal([]byte(value), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return minerHealthFromASICEvents(events)
}

func minerHealthFromASICEvents(events []map[string]any) map[string]any {
	rows := make([]map[string]any, 0, len(events))
	allowlist := parseASICMACSet(env("POOL_ASIC_MAC_ALLOWLIST", ""))
	for _, event := range events {
		mac := normalizedMAC(mapString(event, "mac"))
		ip := mapString(event, "ip")
		if mac == "" && ip == "" {
			continue
		}
		if len(allowlist) > 0 && !allowlist[mac] {
			continue
		}
		status := first(mapString(event, "status"), "configured")
		connected := lower(status) == "connected"
		row := map[string]any{
			"managed":             true,
			"device_type":         "asic",
			"mac":                 mac,
			"identity":            first(mac, ip),
			"device_id":           "mac:" + mac,
			"ip":                  ip,
			"status":              status,
			"health":              status,
			"connected":           connected,
			"configured_pool_url": first(env("BDAG_POOL_URL", ""), env("MINER_POOL_URL", "")),
			"intended_wallet":     env("MINING_POOL_ADDRESS", ""),
			"last_seen_at":        mapString(event, "updated_at"),
			"source":              first(mapString(event, "source"), "redis-asic-status"),
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return first(mapString(rows[i], "ip"), mapString(rows[i], "mac")) < first(mapString(rows[j], "ip"), mapString(rows[j], "mac"))
	})
	connectedCount := 0
	for _, row := range rows {
		if truthyValue(row["connected"]) {
			connectedCount++
		}
	}
	return map[string]any{
		"managed_count":     len(rows),
		"tracked_count":     len(rows),
		"connected_count":   connectedCount,
		"configured_count":  len(rows),
		"managed_miners":    rows,
		"miners":            rows,
		"source":            "redis-asic-status",
		"source_key":        "bdag:miners:by_mac",
		"identity_contract": "mac-only",
	}
}

func minerHealthFromASICPerformance(performance map[string]any) map[string]any {
	lanes := listMaps(performance["lanes"])
	if len(lanes) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(lanes))
	for _, lane := range lanes {
		row := asicPerformanceMinerRow(lane)
		if row == nil {
			continue
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return first(mapString(rows[i], "ip"), mapString(rows[i], "mac"), mapString(rows[i], "lane_id")) <
			first(mapString(rows[j], "ip"), mapString(rows[j], "mac"), mapString(rows[j], "lane_id"))
	})
	connectedCount := 0
	for _, row := range rows {
		if truthyValue(row["connected"]) {
			connectedCount++
		}
	}
	return map[string]any{
		"managed_count":     len(rows),
		"tracked_count":     len(rows),
		"connected_count":   connectedCount,
		"configured_count":  len(rows),
		"managed_miners":    rows,
		"miners":            rows,
		"source":            "pool-asic-performance",
		"source_key":        "bdag:asic:performance",
		"identity_contract": "mac-or-lane",
	}
}

func mergeASICPerformanceIntoMinerHealth(minerHealth map[string]any, performance map[string]any) {
	lanes := listMaps(performance["lanes"])
	if len(lanes) == 0 {
		return
	}
	byMAC := map[string]map[string]any{}
	byLane := map[string]map[string]any{}
	for _, lane := range lanes {
		if mac := normalizedMAC(mapString(lane, "asic_mac")); mac != "" {
			byMAC[mac] = lane
		}
		if laneID := mapString(lane, "lane_id"); laneID != "" {
			byLane[laneID] = lane
		}
	}
	for _, rawRows := range []any{minerHealth["managed_miners"], minerHealth["miners"]} {
		rows := listMaps(rawRows)
		for _, row := range rows {
			lane := byMAC[normalizedMAC(mapString(row, "mac"))]
			if lane == nil {
				lane = byLane[mapString(row, "lane_id")]
			}
			if lane == nil {
				continue
			}
			mergeASICPerformanceFields(row, lane)
		}
	}
}

func derivedASICPerformancePayload(jobState map[string]any, minerHealth map[string]any) map[string]any {
	now := time.Now().UTC()
	clients := listMaps(jobState["clients"])
	rows := minerSourceRows(map[string]any{"miner_health": minerHealth})
	if len(rows) == 0 && len(clients) == 0 {
		return nil
	}
	byMAC := map[string]map[string]any{}
	byIP := map[string]map[string]any{}
	for _, client := range clients {
		if mac := normalizedMAC(mapString(client, "asic_mac")); mac != "" {
			byMAC[mac] = client
		}
		if ip := first(mapString(client, "remote_host"), clientHost(mapString(client, "address"))); ip != "" {
			byIP[ip] = client
		}
	}
	lanes := make([]map[string]any, 0, maxInt(len(rows), len(clients)))
	seen := map[string]bool{}
	for _, row := range rows {
		lane := derivedASICLane(row, byMAC[normalizedMAC(mapString(row, "mac"))], byIP[mapString(row, "ip")], now)
		if lane == nil {
			continue
		}
		key := first(normalizedMAC(mapString(lane, "asic_mac")), mapString(lane, "remote_host"), mapString(lane, "lane_id"))
		if key != "" {
			seen[key] = true
		}
		lanes = append(lanes, lane)
	}
	for _, client := range clients {
		key := first(normalizedMAC(mapString(client, "asic_mac")), first(mapString(client, "remote_host"), clientHost(mapString(client, "address"))), mapString(client, "lane_id"))
		if key != "" && seen[key] {
			continue
		}
		if lane := derivedASICLane(nil, client, nil, now); lane != nil {
			lanes = append(lanes, lane)
		}
	}
	if len(lanes) == 0 {
		return nil
	}
	sort.SliceStable(lanes, func(i, j int) bool {
		return first(mapString(lanes[i], "remote_host"), mapString(lanes[i], "asic_mac"), mapString(lanes[i], "lane_id")) <
			first(mapString(lanes[j], "remote_host"), mapString(lanes[j], "asic_mac"), mapString(lanes[j], "lane_id"))
	})
	return map[string]any{
		"status":              "ok",
		"updated_at":          now.Format(time.RFC3339),
		"updated_at_epoch":    now.Unix(),
		"age_seconds":         0,
		"source":              "redis-derived-job-state-telemetry",
		"stale_after_seconds": 15,
		"lanes":               lanes,
	}
}

func derivedASICLane(row map[string]any, client map[string]any, clientByIP map[string]any, now time.Time) map[string]any {
	if client == nil {
		client = clientByIP
	}
	mac := normalizedMAC(first(mapString(row, "mac"), mapString(client, "asic_mac")))
	ip := first(mapString(row, "ip"), mapString(client, "remote_host"), clientHost(mapString(client, "address")))
	laneID := first(mapString(client, "lane_id"), mapString(row, "lane_id"))
	if laneID == "" && mac != "" {
		laneID = "mac:" + mac
	}
	if mac == "" && ip == "" && laneID == "" {
		return nil
	}
	ready := truthyValue(client["ready"])
	health := first(mapString(client, "reason_code"), mapString(row, "health"), mapString(row, "status"), "unknown")
	if client == nil && truthyValue(row["connected"]) {
		health = "no-stratum"
	} else if health == "ok" || health == "configured" {
		health = "connected"
	}
	lastMS := epochMilliFromTimeString(first(mapString(row, "last_share_at"), mapString(row, "last_seen_at")))
	if lastMS <= 0 {
		lastMS = now.UnixMilli()
	}
	accepted := firstNumber(row, "device_accepted", "accepted", "valid_shares")
	rejected := firstNumber(row, "device_rejected", "rejected", "rejected_shares")
	rejected += firstNumber(client, "expired_window_rejects", "low_diff_rejects")
	bucket := map[string]any{
		"start_unix_ms": lastMS,
		"counts": map[string]any{
			"accepted":       accepted,
			"rejected":       rejected,
			"rejected_local": firstNumber(client, "low_diff_rejects"),
		},
	}
	return map[string]any{
		"lane_id":                    laneID,
		"lane_identity_source":       first(mapString(client, "lane_identity_source"), "redis-derived"),
		"asic_mac":                   mac,
		"remote_host":                ip,
		"client_addr":                first(mapString(client, "address"), ip),
		"ready":                      ready,
		"health":                     health,
		"current_job_age_ms":         first(mapString(client, "current_job_age_ms"), "0"),
		"protocol_variant":           first(mapString(client, "protocol_variant"), "stratum-v1-basic"),
		"extranonce_subscribed":      truthyValue(client["extranonce_subscribed"]),
		"pdiff":                      mapString(client, "pdiff"),
		"accepted":                   accepted,
		"rejected_30m":               rejected,
		"rejected_local_30m":         firstNumber(client, "low_diff_rejects"),
		"last_event_unix_ms":         lastMS,
		"last_activity_unix_ms":      lastMS,
		"per_asic_block_attribution": false,
		"buckets":                    []any{bucket},
	}
}

func clientHost(address string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err == nil {
		return host
	}
	return strings.TrimSpace(address)
}

func epochMilliFromTimeString(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05-0700", "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UnixMilli()
		}
	}
	if parsed, ok := parseNumber(value); ok && parsed > 0 {
		if parsed > 1_000_000_000_000 {
			return int64(parsed)
		}
		return int64(parsed * 1000)
	}
	return 0
}

func asicPerformanceMinerRow(lane map[string]any) map[string]any {
	laneID := mapString(lane, "lane_id")
	mac := normalizedMAC(mapString(lane, "asic_mac"))
	ip := first(mapString(lane, "remote_host"), mapString(lane, "client_addr"))
	if mac == "" && laneID == "" && ip == "" {
		return nil
	}
	deviceID := laneID
	if mac != "" {
		deviceID = "mac:" + mac
	}
	row := map[string]any{
		"managed":              true,
		"device_type":          "asic",
		"mac":                  mac,
		"identity":             first(mac, laneID, ip),
		"device_id":            deviceID,
		"ip":                   ip,
		"status":               first(mapString(lane, "health"), "not_connected"),
		"health":               first(mapString(lane, "health"), "not_connected"),
		"connected":            truthyValue(lane["ready"]),
		"configured_pool_url":  first(env("BDAG_POOL_URL", ""), env("MINER_POOL_URL", "")),
		"intended_wallet":      env("MINING_POOL_ADDRESS", ""),
		"source":               "pool-asic-performance",
		"lane_id":              laneID,
		"lane_identity_source": mapString(lane, "lane_identity_source"),
	}
	mergeASICPerformanceFields(row, lane)
	return row
}

func mergeASICPerformanceFields(row map[string]any, lane map[string]any) {
	if row == nil || lane == nil {
		return
	}
	if mac := normalizedMAC(mapString(lane, "asic_mac")); mac != "" {
		row["mac"] = mac
	}
	if laneID := mapString(lane, "lane_id"); laneID != "" {
		row["lane_id"] = laneID
	}
	if ip := first(mapString(lane, "remote_host"), mapString(lane, "client_addr")); ip != "" {
		row["ip"] = ip
	}
	row["ready"] = truthyValue(lane["ready"])
	row["connected"] = truthyValue(lane["ready"])
	row["health"] = first(mapString(lane, "health"), mapString(row, "health"))
	row["status"] = first(mapString(lane, "health"), mapString(row, "status"))
	row["protocol_variant"] = mapString(lane, "protocol_variant")
	row["extranonce_subscribed"] = truthyValue(lane["extranonce_subscribed"])
	row["current_job_age_ms"] = mapString(lane, "current_job_age_ms")
	row["pdiff"] = mapString(lane, "pdiff")
	row["blocks_30m"] = mapString(lane, "blocks_30m")
	row["blocks_1h"] = mapString(lane, "blocks_1h")
	row["blocks_6h"] = mapString(lane, "blocks_6h")
	row["blocks_24h"] = mapString(lane, "blocks_24h")
	row["rejected_30m"] = mapString(lane, "rejected_30m")
	row["rejected_local_30m"] = mapString(lane, "rejected_local_30m")
	row["last_block_at_epoch_ms"] = mapString(lane, "last_accepted_unix_ms")
	row["last_event_at_epoch_ms"] = mapString(lane, "last_event_unix_ms")
}

func mergeASICTelemetryIntoMinerHealth(minerHealth map[string]any, telemetry map[string]any) {
	devices := listMaps(telemetry["devices"])
	if len(devices) == 0 {
		return
	}
	byMAC := map[string]map[string]any{}
	for _, device := range devices {
		if mac := normalizedMAC(mapString(device, "mac")); mac != "" {
			byMAC[mac] = device
		}
	}
	for _, rawRows := range []any{minerHealth["managed_miners"], minerHealth["miners"]} {
		for _, row := range listMaps(rawRows) {
			device := byMAC[normalizedMAC(mapString(row, "mac"))]
			if device == nil {
				continue
			}
			summary := mapValue(device, "summary")
			row["device_model"] = mapString(summary, "model")
			row["firmware"] = mapString(summary, "firmware")
			row["manual_powerplan"] = mapString(summary, "manual_powerplan")
			row["clock_mhz"] = mapString(summary, "clock_mhz")
			row["voltage_v"] = mapString(summary, "voltage_v")
			row["chip_count"] = mapString(summary, "chip_count")
			row["temperature"] = mapString(summary, "temperature")
			row["fanspeed"] = mapString(summary, "fanspeed")
			row["hashrate"] = mapString(summary, "hashrate")
			row["avg_hashrate"] = mapString(summary, "avg_hashrate")
			row["active_pool_url"] = mapString(summary, "active_pool_url")
			row["active_pool_user"] = mapString(summary, "active_pool_user")
			row["active_pool_legal"] = mapString(summary, "active_pool_legal")
			row["pool_pass_present"] = truthyValue(summary["pool_pass_present"])
			row["device_miner_status"] = mapString(summary, "miner_status")
			row["device_accepted"] = mapString(summary, "accepted")
			row["device_rejected"] = mapString(summary, "rejected")
			row["device_hw_errors"] = mapString(summary, "hw_errors")
			row["device_nonces"] = mapString(summary, "nonces")
			row["device_telemetry_status"] = mapString(device, "status")
			row["device_telemetry_age_seconds"] = mapString(device, "age_seconds")
			row["device_telemetry_errors"] = telemetryErrorSummary(device)
		}
	}
}

func telemetryErrorSummary(device map[string]any) string {
	errorsByEndpoint := mapValue(device, "errors")
	if len(errorsByEndpoint) == 0 {
		return ""
	}
	keys := make([]string, 0, len(errorsByEndpoint))
	for key := range errorsByEndpoint {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(mapString(errorsByEndpoint, key))
		if value == "" {
			continue
		}
		parts = append(parts, key+": "+shortText(value, 140))
	}
	return strings.Join(parts, " | ")
}

func applyPoolReadinessToManagedASICRows(minerHealth map[string]any, metrics map[string]any) {
	managed := int(number(mapString(minerHealth, "managed_count")))
	if managed <= 0 {
		return
	}
	active := int(number(mapString(metrics, "active_connections")))
	ready := int(number(mapString(metrics, "ready_miners")))
	authorized := int(number(mapString(metrics, "authorized_miners")))
	if active < managed || ready < managed || authorized < managed {
		return
	}
	connected := 0
	for _, rawRows := range []any{minerHealth["managed_miners"], minerHealth["miners"]} {
		rows, ok := rawRows.([]map[string]any)
		if !ok {
			continue
		}
		for _, row := range rows {
			if !truthyValue(row["managed"]) {
				continue
			}
			if lower(mapString(row, "status")) == "configured" || lower(mapString(row, "health")) == "configured" {
				row["status"] = "connected"
				row["health"] = "connected"
				row["connected"] = true
				row["connected_source"] = "pool-metrics"
			}
			if truthyValue(row["connected"]) {
				connected++
			}
		}
		if connected > 0 {
			break
		}
	}
	if connected >= managed {
		minerHealth["connected_count"] = connected
		minerHealth["connected_count_effective"] = connected
		minerHealth["pool_ready_overlay"] = true
	}
}

func nativeSyncProgress(ctx context.Context) map[string]any {
	url := first(
		env("BDAG_NODE_RPC_URL", ""),
		env("NODE_RPC_URL", ""),
		env("BDAG_RPC_URL", ""),
		defaultNativeRPCURL,
	)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	blockCount, err := nativeRPCNumber(ctx, url, "getBlockCount")
	if err != nil {
		return map[string]any{
			"status":          "unknown",
			"source":          "native-rpc",
			"chain_rpc_error": err.Error(),
			"error":           err.Error(),
		}
	}
	current, currentErr := nativeRPCCurrent(ctx, url)
	peers, peerErr := nativeRPCPeers(ctx, url)
	evmSync := nativeEVMSyncing(ctx)
	peerCount, bestPeerMainOrder := summarizeNativePeers(peers)
	gap := int64(0)
	if bestPeerMainOrder > int64(blockCount) {
		gap = bestPeerMainOrder - int64(blockCount)
	}

	status := "synced"
	errText := ""
	switch {
	case peerErr != nil:
		status = "unknown"
		errText = peerErr.Error()
	case peerCount == 0:
		status = "p2p_down"
		errText = "no active native P2P peers"
	case currentErr != nil:
		status = "unknown"
		errText = currentErr.Error()
	case !current || gap > 0:
		status = "syncing"
	case evmSync["chain_syncing"] == true:
		errText = ""
	}

	result := map[string]any{
		"status":              status,
		"source":              "native-rpc",
		"chain_block_count":   blockCount,
		"current_block":       blockCount,
		"highest_block":       blockCount,
		"remaining_blocks":    0,
		"peer_count":          peerCount,
		"p2p_connections":     peerCount,
		"native_is_current":   current,
		"chain_rpc_source":    "getBlockCount",
		"best_peer_mainorder": bestPeerMainOrder,
	}
	for key, value := range evmSync {
		result[key] = value
	}
	if bestPeerMainOrder > 0 {
		result["p2p_network_height"] = bestPeerMainOrder
		result["highest_block"] = maxInt64(int64(blockCount), bestPeerMainOrder)
		result["p2p_network_gap"] = gap
		result["peer_mainorder_gap"] = gap
		result["remaining_blocks"] = gap
	}
	if evmSync["chain_syncing"] == true && status == "synced" && current && gap == 0 {
		result["chain_syncing"] = false
		result["evm_chain_syncing"] = true
		result["mining_advisory_sync"] = true
		result["evm_sync_advisory"] = "eth_syncing active while native P2P mining state is current"
		if current, ok := evmSync["sync_current_block"].(uint64); ok {
			result["evm_block_count"] = current
		}
	}
	if evmSync["chain_syncing"] == true && status != "synced" {
		if current, ok := evmSync["sync_current_block"].(uint64); ok {
			result["current_block"] = current
			result["evm_block_count"] = current
		}
		if highest, ok := evmSync["sync_highest_block"].(uint64); ok {
			result["highest_block"] = highest
			if current, ok := evmSync["sync_current_block"].(uint64); ok && highest >= current {
				result["remaining_blocks"] = highest - current
			}
		}
	}
	if errText != "" {
		result["error"] = errText
	}
	return result
}

func nativeSyncProgressCached(ctx context.Context) map[string]any {
	key := nativeSyncProgressCacheKey()
	now := time.Now()
	nativeSyncProgressCache.mu.Lock()
	if nativeSyncProgressCache.key == key && nativeSyncProgressCache.payload != nil {
		if now.Before(nativeSyncProgressCache.expires) {
			payload := cloneMap(nativeSyncProgressCache.payload)
			fetchedAt := nativeSyncProgressCache.fetchedAt
			nativeSyncProgressCache.mu.Unlock()
			payload["cache"] = "hit"
			payload["cache_age_seconds"] = now.Sub(fetchedAt).Seconds()
			return payload
		}
		if dashboardNodeRPCBudget.backoffActive() {
			payload := cloneMap(nativeSyncProgressCache.payload)
			fetchedAt := nativeSyncProgressCache.fetchedAt
			nativeSyncProgressCache.mu.Unlock()
			payload["cache"] = "stale"
			payload["cache_age_seconds"] = now.Sub(fetchedAt).Seconds()
			payload["best_effort"] = true
			warnings := stringSlice(payload["warnings"])
			warnings = appendUniqueWarning(warnings, "Serving cached node sync status while node RPC backoff is active.")
			payload["warnings"] = warnings
			return payload
		}
	}
	nativeSyncProgressCache.mu.Unlock()

	payload := nativeSyncProgress(ctx)
	if payload == nil {
		return nil
	}
	nativeSyncProgressCache.mu.Lock()
	nativeSyncProgressCache.key = key
	nativeSyncProgressCache.payload = cloneMap(payload)
	nativeSyncProgressCache.fetchedAt = now
	nativeSyncProgressCache.expires = now.Add(nativeSyncProgressCacheTTL())
	nativeSyncProgressCache.mu.Unlock()
	payload = cloneMap(payload)
	payload["cache"] = "miss"
	payload["cache_age_seconds"] = 0
	return payload
}

func nativeSyncProgressCacheKey() string {
	nativeURL := first(
		env("BDAG_NODE_RPC_URL", ""),
		env("NODE_RPC_URL", ""),
		env("BDAG_RPC_URL", ""),
		defaultNativeRPCURL,
	)
	return nativeURL + "|" + env("BDAG_EVM_HTTP_URL", defaultEVMHTTPURL)
}

func nativeSyncProgressCacheTTL() time.Duration {
	ttl := durationEnv("BDAG_STATUS_NATIVE_SYNC_CACHE_SECONDS", 5*time.Second)
	if ttl < 3*time.Second {
		ttl = 3 * time.Second
	}
	if ttl > 10*time.Second {
		ttl = 10 * time.Second
	}
	return ttl
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func nativeEVMSyncing(ctx context.Context) map[string]any {
	url := env("BDAG_EVM_HTTP_URL", defaultEVMHTTPURL)
	raw, err := evmRPCRaw(ctx, url, "eth_syncing", []any{})
	result := map[string]any{
		"evm_rpc_url": url,
	}
	if err != nil {
		result["eth_syncing_error"] = err.Error()
		return result
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("false")) {
		result["eth_syncing"] = false
		result["chain_syncing"] = false
		result["evm_chain_syncing"] = false
		return result
	}
	var syncing map[string]any
	if err := json.Unmarshal(trimmed, &syncing); err == nil && syncing != nil {
		result["eth_syncing"] = syncing
		result["chain_syncing"] = true
		result["evm_chain_syncing"] = true
		if value := first(stringValue(syncing, "currentBlock"), stringValue(syncing, "current_block")); value != "" {
			if parsed, err := parseHexUint(value); err == nil {
				result["sync_current_block"] = parsed
			}
		}
		if value := first(stringValue(syncing, "highestBlock"), stringValue(syncing, "highest_block")); value != "" {
			if parsed, err := parseHexUint(value); err == nil {
				result["sync_highest_block"] = parsed
			}
		}
		return result
	}
	var boolValue bool
	if err := json.Unmarshal(trimmed, &boolValue); err == nil {
		result["eth_syncing"] = boolValue
		result["chain_syncing"] = boolValue
		result["evm_chain_syncing"] = boolValue
		return result
	}
	result["eth_syncing"] = string(trimmed)
	result["chain_syncing"] = len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
	result["evm_chain_syncing"] = result["chain_syncing"]
	return result
}

func evmRPCRaw(ctx context.Context, url, method string, params []any) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if user, pass := env("NODE_RPC_USER", ""), env("NODE_RPC_PASS", ""); user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := dashboardNodeRPCBudget.do(ctx, nil, req)
	if err != nil {
		err = fmt.Errorf("%s failed: %w", method, err)
		dashboardNodeRPCBudget.noteError(err)
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		err := fmt.Errorf("%s returned %s: %s", method, resp.Status, string(raw))
		dashboardNodeRPCBudget.noteError(err)
		return nil, err
	}
	var rpcResp evmRPCResponse
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		err := fmt.Errorf("%s error %d: %s", method, rpcResp.Error.Code, rpcResp.Error.Message)
		dashboardNodeRPCBudget.noteError(err)
		return nil, err
	}
	return rpcResp.Result, nil
}

func nativeRPCNumber(ctx context.Context, url, method string) (uint64, error) {
	var value any
	if err := nativeRPC(ctx, url, method, []any{}, &value); err != nil {
		return 0, err
	}
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return 0, fmt.Errorf("%s returned negative value", method)
		}
		return uint64(typed), nil
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s returned non-numeric value %q", method, typed)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%s returned unsupported value %T", method, value)
	}
}

func nativeRPCCurrent(ctx context.Context, url string) (bool, error) {
	var current bool
	if err := nativeRPC(ctx, url, "isCurrent", []any{}, &current); err != nil {
		return false, err
	}
	return current, nil
}

func nativeRPCPeers(ctx context.Context, url string) ([]nativePeerInfo, error) {
	var peers []nativePeerInfo
	if err := nativeRPC(ctx, url, "getPeerInfo", []any{}, &peers); err != nil {
		return nil, err
	}
	return peers, nil
}

func nativeRPC(ctx context.Context, url, method string, params []any, out any) error {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "1.0",
		"id":      method,
		"method":  method,
		"params":  params,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if user, pass := env("NODE_RPC_USER", ""), env("NODE_RPC_PASS", ""); user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := dashboardNodeRPCBudget.do(ctx, nil, req)
	if err != nil {
		err = fmt.Errorf("%s failed: %w", method, err)
		dashboardNodeRPCBudget.noteError(err)
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("%s response read failed: %w", method, err)
	}
	if resp.StatusCode >= 400 {
		err := fmt.Errorf("%s returned %s: %s", method, resp.Status, string(raw))
		dashboardNodeRPCBudget.noteError(err)
		return err
	}
	var rpcResp nativeRPCResponse
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return fmt.Errorf("%s response decode failed: %w", method, err)
	}
	if rpcResp.Error != nil {
		err := fmt.Errorf("%s error %d: %s", method, rpcResp.Error.Code, rpcResp.Error.Message)
		dashboardNodeRPCBudget.noteError(err)
		return err
	}
	if len(rpcResp.Result) == 0 || string(rpcResp.Result) == "null" {
		return fmt.Errorf("%s returned empty result", method)
	}
	if err := json.Unmarshal(rpcResp.Result, out); err != nil {
		return fmt.Errorf("%s result decode failed: %w", method, err)
	}
	return nil
}

func summarizeNativePeers(peers []nativePeerInfo) (int, int64) {
	active := 0
	bestMainOrder := int64(0)
	for _, peer := range peers {
		if !peer.Active && !peer.State {
			continue
		}
		active++
		if order := graphstateOrder(peer.Graphstate); order > bestMainOrder {
			bestMainOrder = order
		}
	}
	return active, bestMainOrder
}

func graphstateOrder(graphstate map[string]any) int64 {
	for _, key := range []string{"mainorder", "best_main_order", "order"} {
		switch value := graphstate[key].(type) {
		case float64:
			return int64(value)
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func updateSnapshotAge(payload map[string]any) {
	epoch := int64(number(stringValue(payload, "updated_at_epoch")))
	if epoch <= 0 {
		return
	}
	age := time.Now().Unix() - epoch
	if age < 0 {
		age = 0
	}
	payload["age_seconds"] = age
	payload["last_update_age_seconds"] = age
	if staleAfter := int64(number(stringValue(payload, "stale_after_seconds"))); staleAfter > 0 && age > staleAfter && stringValue(payload, "status") != "stale" && stringValue(payload, "status") != "error" {
		payload["status"] = "stale"
	}
}

func (r *DashboardRuntime) handleEvents(w http.ResponseWriter, req *http.Request) {
	if !r.Ready() {
		http.Error(w, "redis runtime unavailable", http.StatusServiceUnavailable)
		return
	}
	maxClients := int64(intEnv("BDAG_DASHBOARD_SSE_MAX_CLIENTS", 16))
	active := r.sseActive.Add(1)
	defer r.sseActive.Add(-1)
	if maxClients > 0 && active > maxClients {
		http.Error(w, "too many dashboard event clients", http.StatusTooManyRequests)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	lastID := strings.TrimSpace(req.Header.Get("Last-Event-ID"))
	if lastID == "" {
		lastID = "$"
	}
	_, _ = fmt.Fprint(w, "retry: 3000\n: connected\n\n")
	flusher.Flush()
	for req.Context().Err() == nil {
		streams, err := r.client.XRead(req.Context(), &redis.XReadArgs{
			Streams: []string{"bdag:dashboard:events", lastID},
			Block:   15 * time.Second,
			Count:   20,
		}).Result()
		if errors.Is(err, redis.Nil) {
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
			continue
		}
		if err != nil {
			if req.Context().Err() == nil {
				log.Printf("dashboard SSE XREAD failed: %v", err)
			}
			return
		}
		for _, stream := range streams {
			for _, message := range stream.Messages {
				lastID = message.ID
				eventType := fmt.Sprint(message.Values["type"])
				if eventType == "" || eventType == "<nil>" {
					eventType = "dashboard"
				}
				data, _ := json.Marshal(message.Values)
				_, _ = fmt.Fprintf(w, "id: %s\nevent: dashboard-update\ndata: %s\n\n", message.ID, data)
			}
		}
		flusher.Flush()
	}
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func intEnv(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
