package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"dashboard/minercontrol"
)

const (
	defaultAPIBase = "http://127.0.0.1:9280"
	defaultAddr    = ":9290"
)

var displayLocation = time.Local

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
var communityReleaseVersionRE = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

type APIClient struct {
	base     string
	http     *http.Client
	mu       sync.Mutex
	caches   map[string]cacheEntry
	inflight map[string]*apiCall
}

type cacheEntry struct {
	payload map[string]any
	expires time.Time
	err     error
}

type apiCall struct {
	done    chan struct{}
	payload map[string]any
	err     error
}

type App struct {
	api                  *APIClient
	runtime              *DashboardRuntime
	minerController      minercontrol.Controller
	minerControlEnabled  bool
	minerControlToken    string
	minerControlDisabled string
	templates            *template.Template
	statusDataProvider   func(context.Context) (map[string]any, error)
	peerListProvider     func(context.Context) (PeerListView, error)
	nodeLogProvider      func(context.Context, int) string
	poolLogProvider      func(context.Context, int) string
	dashboardLogProvider func(context.Context, int) string
}

type Evidence struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type EvidenceGroup struct {
	Title string     `json:"title"`
	Items []Evidence `json:"items"`
}

type StatusStep struct {
	Label  string `json:"label"`
	Active bool   `json:"active"`
}

type StatusPanel struct {
	Title       string          `json:"title"`
	State       string          `json:"state"`
	Tone        string          `json:"tone"`
	Evidence    []Evidence      `json:"evidence"`
	Groups      []EvidenceGroup `json:"groups,omitempty"`
	StatusSteps []StatusStep    `json:"status_steps"`
}

type TabView struct {
	Title        string              `json:"title"`
	State        string              `json:"state"`
	StateTone    string              `json:"state_tone"`
	Mode         string              `json:"mode"`
	Overall      string              `json:"overall"`
	CanMine      bool                `json:"can_mine"`
	CanAccept    bool                `json:"can_accept_shares"`
	CanSubmit    bool                `json:"can_submit_blocks"`
	UpdatedAt    string              `json:"updated_at"`
	RefreshEvery string              `json:"refresh_every"`
	Evidence     []Evidence          `json:"evidence"`
	Groups       []EvidenceGroup     `json:"groups"`
	Secondary    []Evidence          `json:"secondary"`
	StatusSteps  []StatusStep        `json:"status_steps"`
	Warnings     []string            `json:"warnings"`
	Notes        []string            `json:"notes"`
	Rows         []map[string]string `json:"rows"`
	ASICRows     []map[string]string `json:"asic_rows"`
	DeviceRows   []map[string]string `json:"device_rows"`
	ChartJSON    template.JS         `json:"chart_json"`
	APIError     string              `json:"api_error"`
	Node         StatusPanel         `json:"node"`
	SyncProgress map[string]any      `json:"sync_progress,omitempty"`
	MinerHealth  map[string]any      `json:"miner_health,omitempty"`
	PoolHealth   map[string]any      `json:"pool_health,omitempty"`
	Template     map[string]any      `json:"template_health,omitempty"`
}

type chartPoint struct {
	T               string  `json:"t"`
	Blocks          float64 `json:"blocks"`
	BDAG            float64 `json:"bdag"`
	USD             float64 `json:"usd"`
	Coverage        float64 `json:"coverage,omitempty"`
	BestEffort      bool    `json:"best_effort,omitempty"`
	RequestedBlocks int     `json:"requested_blocks,omitempty"`
	FetchedBlocks   int     `json:"fetched_blocks,omitempty"`
	WindowComplete  bool    `json:"window_complete,omitempty"`
	Status          string  `json:"status,omitempty"`
}

type MinerControlView struct {
	Title             string
	State             string
	StateTone         string
	APIError          string
	Rows              []map[string]string
	Evidence          []Evidence
	Enabled           bool
	DisabledReason    string
	DefaultPoolURL    string
	DefaultWorkerUser string
	ReplaceExisting   bool
}

func main() {
	if os.Getenv("BDAG_LOG_PROXY_MODE") == "1" {
		if err := runDockerLogProxy(); err != nil {
			log.Fatalf("dashboard log proxy: %v", err)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	apiBase := strings.TrimRight(env("BDAG_COLLECTOR_API", defaultAPIBase), "/")
	addr := env("ADDR", defaultAddr)
	displayLocation = configuredLocation()
	runtime, err := startDashboardRuntime(ctx)
	if err != nil {
		log.Printf("dashboard Redis runtime unavailable; legacy collector fallback remains active: %v", err)
	}
	if runtime != nil && runtime.Ready() {
		runtime.startWorkers(ctx)
	}
	control, controlEnabled, controlDisabled := minerControlFromEnv()
	app := &App{
		api:                  newAPIClient(apiBase, nil),
		runtime:              runtime,
		minerController:      control,
		minerControlEnabled:  controlEnabled,
		minerControlToken:    env("BDAG_MINER_CONTROL_TOKEN", ""),
		minerControlDisabled: controlDisabled,
		templates:            parseTemplates(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/static/bdag-community-logo.svg", serveBrandLogo)
	mux.HandleFunc("/static/htmx-2.0.4.min.js", serveHTMX)
	mux.HandleFunc("/", app.handleIndex)
	mux.HandleFunc("/tabs/", app.handleTab)
	mux.HandleFunc("/partials/global-pool-earnings", app.handleGlobalPoolEarnings)
	mux.HandleFunc("/api/status", app.handleRawStatus)
	mux.HandleFunc("/api/live/status", app.handleLiveStatus)
	mux.HandleFunc("/api/live/node-status", app.handleLiveNodeStatus)
	mux.HandleFunc("/api/live/peers", app.handleLivePeers)
	mux.HandleFunc("/api/live/pool-status", app.handleLivePoolStatus)
	mux.HandleFunc("/api/live/stratum-runtime", app.handleLiveStratumRuntime)
	mux.HandleFunc("/api/live/chain-dag", app.handleLiveChainDAG)
	mux.HandleFunc("/api/live/pool-earnings", app.handleLivePoolEarnings)
	mux.HandleFunc("/api/live/global", app.handleLiveGlobal)
	mux.HandleFunc("/api/live/global/trend", app.handleLiveGlobalTrend)
	mux.HandleFunc("/api/live/global-pool-earnings", app.handleLiveGlobalPoolEarnings)
	mux.HandleFunc("/logs/", app.handleLogs)
	mux.HandleFunc("/events", app.handleEvents)
	mux.HandleFunc("/healthz", app.handleHealthz)
	mux.HandleFunc("/actions/miner-control/configure", app.handleMinerConfigure)
	mux.HandleFunc("/actions/miner-control/restart", app.handleMinerRestart)

	server := &http.Server{Addr: addr, Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("dashboard listening on http://127.0.0.1%s using collector API %s display timezone %s", displayAddr(addr), apiBase, displayLocation)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("dashboard server failed: %v", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if runtime != nil {
		runtime.Close()
	}
}

func serveBrandLogo(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(brandLogoSVG)
}

func serveHTMX(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(htmxJS)
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	if a.runtime == nil || !a.runtime.Ready() {
		http.Error(w, "Redis live runtime unavailable", http.StatusServiceUnavailable)
		return
	}
	a.runtime.handleEvents(w, r)
}

func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := map[string]any{
		"status": "ok",
		"redis":  false,
	}
	if a.runtime != nil && a.runtime.Ready() {
		if err := a.runtime.client.Ping(r.Context()).Err(); err == nil {
			status["redis"] = true
		} else {
			status["status"] = "degraded"
			status["redis_error"] = err.Error()
		}
		if global, err := a.liveDAGGlobalPayload(r.Context(), nil); err == nil && global != nil {
			age := int64(number(stringValue(global, "age_seconds")))
			liveLimit := int64(intEnv("BDAG_DASHBOARD_HEALTH_STALE_SECONDS", 20))
			status["global_status"] = stringValue(global, "status")
			status["global_age_seconds"] = age
			status["global_latest_block"] = stringValue(global, "latest_block")
			status["global_chain_latest_block"] = stringValue(global, "chain_latest_block")
			status["global_stream_lag_blocks"] = stringValue(global, "stream_lag_blocks")
			if liveLimit > 0 && age > liveLimit {
				status["status"] = "degraded"
				status["live_error"] = "global live snapshot is stale"
			}
		} else if err != nil {
			status["status"] = "degraded"
			status["global_error"] = err.Error()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if stringValue(status, "status") != "ok" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(status)
}

func newAPIClient(base string, client *http.Client) *APIClient {
	if client == nil {
		client = &http.Client{Timeout: 16 * time.Second}
	}
	return &APIClient{
		base:     strings.TrimRight(base, "/"),
		http:     client,
		caches:   map[string]cacheEntry{},
		inflight: map[string]*apiCall{},
	}
}

func parseTemplates() *template.Template {
	funcs := template.FuncMap{
		"peerColumnTooltip": peerColumnTooltip,
		"peerHashColumn":    peerHashColumn,
		"toneClass":         toneClass,
	}
	return template.Must(template.New("app").Funcs(funcs).Parse(indexTemplate + tabTemplates))
}

func minerControlFromEnv() (minercontrol.Controller, bool, string) {
	if env("BDAG_MINER_CONTROL_ENABLED", "") != "1" {
		return nil, false, "Miner control is disabled. Set BDAG_MINER_CONTROL_ENABLED=1 and BDAG_MINER_CONTROL_TOKEN to enable it."
	}
	if env("BDAG_MINER_CONTROL_TOKEN", "") == "" {
		return nil, false, "Miner control is not armed because BDAG_MINER_CONTROL_TOKEN is empty."
	}
	return minercontrol.NewASICController(minercontrol.ASICConfig{
		AllowedCIDRs:      env("BDAG_ASIC_LAN_CIDRS", ""),
		AllowDockerBridge: env("BDAG_DASHBOARD_ALLOW_DOCKER_BRIDGE_MINER_CONTROL", "") == "1",
	}), true, ""
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	view := map[string]any{
		"Title":           "Dashboard",
		"StackReleaseTag": communityReleaseLabel(stackReleaseTag()),
		"Tab":             a.nodeStatusOverview(r.Context()),
	}
	a.render(w, "index", view)
}

func stackReleaseTag() string {
	return first(env("BDAG_STACK_RELEASE_TAG", ""), env("BDAG_RELEASE_VERSION", ""))
}

func communityReleaseLabel(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	version := tag
	for _, prefix := range []string{"stack-dev-", "stack-"} {
		version = strings.TrimPrefix(version, prefix)
	}
	if communityReleaseVersionRE.MatchString(version) {
		return "BDAG Community Release " + version
	}
	return "BDAG Community Development (" + tag + ")"
}

func (a *App) handleTab(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/tabs/")
	var data any
	var tmpl string
	switch name {
	case "overview", "status":
		data, tmpl = a.nodeStatusOverview(r.Context()), "overview"
	case "peers":
		data, tmpl = a.peerList(r.Context()), "peers"
	case "warnings":
		data, tmpl = a.poolStatus(r.Context()), "warnings"
	case "asic-fleet", "miners":
		data, tmpl = a.poolStatus(r.Context()), "asic-fleet"
	case "pool-management", "miner-control":
		data, tmpl = a.minerControlView(r.Context()), "pool-management"
	case "stratum-runtime":
		data, tmpl = a.stratumRuntime(r.Context()), "stratum-runtime"
	case "chain-dag":
		data, tmpl = a.chainDAG(r.Context()), "chain-dag"
	case "pool-earnings":
		data, tmpl = a.poolEarnings(r.Context()), "pool-earnings"
	case "global":
		data, tmpl = a.global(r.Context()), "global"
	case "logs":
		data, tmpl = a.logsView(r.Context()), "logs"
	case "node-logs":
		panel, ok := a.logPanel(r.Context(), "node")
		if !ok {
			http.NotFound(w, r)
			return
		}
		data, tmpl = panel, "log-tab"
	case "pool-logs":
		panel, ok := a.logPanel(r.Context(), "pool")
		if !ok {
			http.NotFound(w, r)
			return
		}
		data, tmpl = panel, "log-tab"
	default:
		http.NotFound(w, r)
		return
	}
	a.render(w, tmpl, data)
}

func (a *App) logsView(ctx context.Context) LogsView {
	node, _ := a.logPanel(ctx, "node")
	pool, _ := logPanelMeta("pool")
	dashboard, _ := logPanelMeta("dashboard")
	return LogsView{Node: node, Pool: pool, Dashboard: dashboard}
}

func (a *App) nodeStatusOverview(ctx context.Context) TabView {
	return a.poolStatus(ctx)
}

func (a *App) handleMinerConfigure(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderControlResult(w, controlError("Invalid form", err))
		return
	}
	if err := a.authorizeMinerControl(r); err != nil {
		a.renderControlResult(w, controlError("Miner control blocked", err))
		return
	}
	target, err := a.validateKnownMinerControlTarget(r.Context(), r.FormValue("target"))
	if err != nil {
		a.renderControlResult(w, controlError("Miner control blocked", err))
		return
	}
	result, err := a.minerController.ApplyPoolConfig(r.Context(), target, minercontrol.PoolConfig{
		AdminPassword:   r.FormValue("admin_password"),
		PoolURL:         r.FormValue("pool_url"),
		WorkerUser:      r.FormValue("worker_user"),
		PoolPassword:    first(r.FormValue("pool_password"), env("BDAG_MINER_POOL_PASSWORD", ""), "1234"),
		ReplaceExisting: !formFalse(r.PostForm, "replace_existing"),
	})
	if err != nil {
		a.renderControlResult(w, controlError("Pool configuration failed", err))
		return
	}
	a.renderControlResult(w, result)
}

func (a *App) handleMinerRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderControlResult(w, controlError("Invalid form", err))
		return
	}
	if err := a.authorizeMinerControl(r); err != nil {
		a.renderControlResult(w, controlError("Miner control blocked", err))
		return
	}
	target, err := a.validateKnownMinerControlTarget(r.Context(), r.FormValue("target"))
	if err != nil {
		a.renderControlResult(w, controlError("Miner control blocked", err))
		return
	}
	result, err := a.minerController.Restart(r.Context(), target, minercontrol.RestartRequest{
		AdminPassword: r.FormValue("admin_password"),
	})
	if err != nil {
		a.renderControlResult(w, controlError("Restart failed", err))
		return
	}
	a.renderControlResult(w, result)
}

func (a *App) renderControlResult(w http.ResponseWriter, result minercontrol.Result) {
	a.render(w, "control-action-result", result)
}

func controlError(title string, err error) minercontrol.Result {
	return minercontrol.Result{
		Tone:    "down",
		Title:   title,
		Message: minercontrol.RedactSecrets(err.Error()),
	}
}

func (a *App) authorizeMinerControl(r *http.Request) error {
	if !a.minerControlEnabled || a.minerController == nil {
		return fmt.Errorf("%s", first(a.minerControlDisabled, "miner control is disabled"))
	}
	token := r.FormValue("control_token")
	if subtle.ConstantTimeCompare([]byte(token), []byte(a.minerControlToken)) != 1 {
		return fmt.Errorf("invalid miner control token")
	}
	return nil
}

func (a *App) validateKnownMinerControlTarget(ctx context.Context, targetValue string) (minercontrol.Target, error) {
	ip, mac := parseMinerControlTarget(targetValue)
	if ip == "" {
		return minercontrol.Target{}, fmt.Errorf("miner target is required")
	}
	statusData, err := a.statusData(ctx)
	if err != nil {
		return minercontrol.Target{}, fmt.Errorf("status API unavailable, refusing miner control: %w", err)
	}
	for _, row := range minerSourceRows(statusData) {
		rowIP := strings.TrimSpace(mapString(row, "ip"))
		if rowIP != ip {
			continue
		}
		rowMAC := normalizedMAC(minerMAC(row))
		if mac != "" && rowMAC != "" && mac != rowMAC {
			return minercontrol.Target{}, fmt.Errorf("submitted MAC %s does not match known miner MAC %s", mac, rowMAC)
		}
		return minercontrol.Target{IP: ip, MAC: rowMAC}, nil
	}
	return minercontrol.Target{}, fmt.Errorf("%s is not in the known miner registry/status payload", ip)
}

func (a *App) handleGlobalPoolEarnings(w http.ResponseWriter, r *http.Request) {
	a.render(w, "global-pool-earnings", a.globalPoolEarnings(r.Context()))
}

func (a *App) handleLiveStatus(w http.ResponseWriter, r *http.Request) {
	a.renderJSON(w, a.poolStatus(r.Context()))
}

func (a *App) handleLiveNodeStatus(w http.ResponseWriter, r *http.Request) {
	node := a.nodeStatusLogStatus(r.Context())
	a.renderJSON(w, TabView{Title: "Node Status", State: node.State, StateTone: node.Tone, RefreshEvery: "3s", Node: node})
}

func (a *App) handleLivePeers(w http.ResponseWriter, r *http.Request) {
	a.renderJSON(w, a.peerList(r.Context()))
}

func (a *App) handleLivePoolStatus(w http.ResponseWriter, r *http.Request) {
	a.renderJSON(w, a.poolLogStatus(r.Context()))
}

func (a *App) handleLiveStratumRuntime(w http.ResponseWriter, r *http.Request) {
	a.renderJSON(w, a.stratumRuntime(r.Context()))
}

func (a *App) handleLiveChainDAG(w http.ResponseWriter, r *http.Request) {
	a.renderJSON(w, a.chainDAG(r.Context()))
}

func (a *App) handleRawStatus(w http.ResponseWriter, r *http.Request) {
	data, err := a.statusData(r.Context())
	if err != nil {
		a.renderJSONStatus(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unavailable",
			"error":  err.Error(),
		})
		return
	}
	if data == nil {
		a.renderJSONStatus(w, http.StatusServiceUnavailable, map[string]any{
			"status": "warming",
			"error":  "status payload is not ready",
		})
		return
	}
	a.renderJSON(w, data)
}

func (a *App) handleLivePoolEarnings(w http.ResponseWriter, r *http.Request) {
	a.renderJSON(w, a.poolEarnings(r.Context()))
}

func (a *App) handleLiveGlobal(w http.ResponseWriter, r *http.Request) {
	includeChart := r.URL.Query().Get("chart") != "0"
	rangeToken := r.URL.Query().Get("range")
	a.renderJSON(w, struct {
		TabView
		PoolEarnings TabView `json:"pool_earnings"`
	}{
		TabView:      a.globalWithRange(r.Context(), rangeToken, includeChart),
		PoolEarnings: a.globalPoolEarnings(r.Context()),
	})
}

func (a *App) handleLiveGlobalTrend(w http.ResponseWriter, r *http.Request) {
	rangeToken := r.URL.Query().Get("range")
	chartJSON, meta, err := a.trendChart(r.Context(), rangeToken)
	if err != nil {
		a.renderJSON(w, map[string]any{
			"status":     "error",
			"error":      err.Error(),
			"chart_json": "[]",
			"trend":      meta,
		})
		return
	}
	a.renderJSON(w, map[string]any{
		"status":     "ok",
		"chart_json": chartJSON,
		"trend":      meta,
	})
}

func (a *App) handleLiveGlobalPoolEarnings(w http.ResponseWriter, r *http.Request) {
	a.renderJSON(w, a.globalPoolEarnings(r.Context()))
}

func (a *App) handleLogs(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/logs/")
	contentOnly := strings.HasSuffix(name, "/content")
	if contentOnly {
		name = strings.TrimSuffix(name, "/content")
	}
	panel, ok := a.logPanel(r.Context(), name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if contentOnly {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(panel.Content))
		return
	}
	a.render(w, "log-panel", panel)
}

func (a *App) logPanel(ctx context.Context, name string) (LogPanel, bool) {
	panel, ok := logPanelMeta(name)
	if !ok {
		return LogPanel{}, false
	}
	panel.Content = a.logContent(ctx, name, 240)
	return panel, true
}

func logPanelMeta(name string) (LogPanel, bool) {
	var title string
	switch name {
	case "node":
		title = "Node Logs"
	case "pool":
		title = "Pool Logs"
	case "dashboard":
		title = "Dashboard Logs"
	default:
		return LogPanel{}, false
	}
	return LogPanel{
		Title:           title,
		Endpoint:        "/logs/" + name,
		ContentEndpoint: "/logs/" + name + "/content",
	}, true
}

func (a *App) logContent(ctx context.Context, name string, tail int) string {
	switch name {
	case "node":
		if a.nodeLogProvider != nil {
			return a.nodeLogProvider(ctx, tail)
		}
	case "pool":
		if a.poolLogProvider != nil {
			return a.poolLogProvider(ctx, tail)
		}
	case "dashboard":
		if a.dashboardLogProvider != nil {
			return a.dashboardLogProvider(ctx, tail)
		}
	}
	return a.runtimeLogs(ctx, name, tail)
}

func (a *App) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

func (a *App) renderJSON(w http.ResponseWriter, data any) {
	a.renderJSONStatus(w, http.StatusOK, data)
}

func (a *App) renderJSONStatus(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type LogPanel struct {
	Title           string
	Endpoint        string
	ContentEndpoint string
	Content         string
}

type LogsView struct {
	Node      LogPanel
	Pool      LogPanel
	Dashboard LogPanel
}

func (a *App) minerControlView(ctx context.Context) MinerControlView {
	data, err := a.statusData(ctx)
	view := MinerControlView{
		Title:           "Pool Management",
		State:           "Disabled",
		StateTone:       "neutral",
		Enabled:         a.minerControlEnabled,
		DisabledReason:  a.minerControlDisabled,
		ReplaceExisting: true,
	}
	if err != nil {
		view.State = "API Unavailable"
		view.StateTone = "down"
		view.APIError = err.Error()
		return view
	}
	view.Rows = minerControlRows(data)
	view.DefaultPoolURL = minerPoolURLFromStatus(data)
	// A pool coinbase belongs to the operator and is not a miner's worker
	// identity. Only an explicit worker default may populate this control.
	view.DefaultWorkerUser = env("BDAG_DEFAULT_WORKER_USER", "")
	view.Evidence = []Evidence{
		{"Known Control Targets", strconv.Itoa(len(view.Rows))},
		{"Default Pool URL", view.DefaultPoolURL},
		{"Default Worker", shortAddress(view.DefaultWorkerUser)},
		{"Control Module", yesNo(a.minerControlEnabled)},
	}
	if a.minerControlEnabled {
		view.State = "Armed"
		view.StateTone = "warn"
		if len(view.Rows) == 0 {
			view.Enabled = false
			view.State = "No Targets"
			view.StateTone = "neutral"
			view.DisabledReason = "No known miners with private IPv4 addresses are available in the Dashboard API Contract."
		}
	}
	return view
}

func (a *App) poolStatus(ctx context.Context) TabView {
	data, err := a.statusData(ctx)
	view := baseTab("Status", "5s", data, err)
	if data == nil && err == nil {
		view.Title = "Pool Status"
		view.State = "Loading"
		view.StateTone = "neutral"
		view.RefreshEvery = "5s"
		view.StatusSteps = []StatusStep{{"Loading", true}}
		view.Node = StatusPanel{Title: "Node Status", State: "Loading", Tone: "neutral"}
		return view
	}
	if err != nil {
		view.State = "API Unavailable"
		view.StateTone = "down"
		view.Title = "Pool Status"
		view.Node = StatusPanel{Title: "Node Status", State: "Unavailable", Tone: "down"}
		return view
	}

	view.Title = "Pool Status"
	view.Node = liveNodeStatus(data)

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
	view.Mode = first(mode, "unknown")
	view.Overall = first(overall, "unknown")
	if templateDegraded && view.Overall == "ok" {
		view.Overall = "degraded"
	}
	view.CanAccept = accepting
	view.CanSubmit = submitReady
	view.CanMine = (boolValue(data, "can_mine") || (accepting && submitReady)) && !templateDegraded
	view.SyncProgress = firstNestedMap(data, []string{"sync_progress"})
	view.MinerHealth = firstNestedMap(data, []string{"miner_health"})
	view.PoolHealth = firstNestedMap(data, []string{"pool_health"})
	view.Template = firstNestedMap(data, []string{"template_health"})
	paidHealthy := recentAcceptedBlockSignal(data)
	minerSignal := minerPresenceSignal(data)
	poolRunning := nestedBool(data, "containers", "pool", "running")
	minersPresent := accepting || (poolRunning && minerSignal > 0)
	view.State, view.StateTone = collectorPoolState(data)
	templateUnsafe := templateCoinbaseUnsafe(data) || templateDegraded
	view.StatusSteps = []StatusStep{
		{waitingLabel, view.State == "Waiting for Collector" || view.State == "Waiting for Live Data"},
		{"Down", view.State == "Down"},
		{"Waiting for a Miner", view.State == "Waiting for a Miner"},
		{"Waiting for Node Sync", view.State == "Waiting for Node Sync"},
		{"Template Unsafe", templateUnsafe},
		{"Degraded", view.State == "Degraded" || view.State == "Mining Degraded"},
		{"Mining", view.State == "Mining"},
	}
	view.Evidence = []Evidence{
		{modeLabel, first(mode, "unknown")},
		{overallLabel, first(overall, "unknown")},
		{"Miner Activity", yesNo(minersPresent)},
		{"Accepting Shares", yesNo(accepting)},
		{"Submit Ready", yesNo(submitReady)},
		{"Getting Fresh Work", yesNo(workFresh)},
		{"Recent Accepted Block", yesNo(paidHealthy)},
	}
	view.Secondary = []Evidence{
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
	}
	view.Warnings = appendUniqueStrings(view.Warnings, stringSlice(data["warnings"])...)
	view.Warnings = appendUniqueStrings(view.Warnings, stringSlice(data["maintenance_warnings"])...)
	view.Warnings = appendUniqueStrings(view.Warnings, stringSlice(data["degraded_reasons"])...)
	if warning := asicFleetCapacityWarning(data); warning != "" {
		view.Warnings = appendUniqueStrings(view.Warnings, warning)
	}
	if boolValue(data, "collector_status_unavailable") {
		view.Warnings = appendUniqueStrings(view.Warnings, "Collector status sample is warming up; live node and pool state is not available yet.")
	}
	if reason := stringValue(data, "status_reason"); reason != "" {
		view.Notes = append(view.Notes, reason)
	}
	if note := collectorCacheNote(data); note != "" {
		view.Notes = append(view.Notes, note)
	}
	view.Rows = a.minerRows(data)
	view.ASICRows = asicPerformanceRows(data)
	view.DeviceRows = asicDeviceRows(data)
	view.ChartJSON = template.JS(asicPerformanceChartJSON(data))
	return view
}

func (a *App) minerRows(data map[string]any) []map[string]string {
	rows := minerRows(data)
	for _, row := range rows {
		ip := strings.TrimSpace(row["ip"])
		mac := normalizedMAC(row["mac"])
		row["target"] = ip + "|" + mac
		row["restart_disabled"] = ""
		row["restart_reason"] = "Restart uses the configured miner-control path and requires the ASIC admin password plus dashboard control token."
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil || parsedIP.To4() == nil || !parsedIP.IsPrivate() || parsedIP.IsLoopback() || parsedIP.IsMulticast() || parsedIP.IsUnspecified() {
			row["restart_disabled"] = "true"
			row["restart_reason"] = "Restart unavailable because this ASIC row does not have a private IPv4 target."
		}
		if !a.minerControlEnabled || a.minerController == nil {
			row["restart_disabled"] = "true"
			row["restart_reason"] = first(a.minerControlDisabled, "Miner control is disabled.")
		}
	}
	return rows
}

func recentAcceptedBlockSignal(data map[string]any) bool {
	metrics := nestedStatusMap(data, "pool_metrics")
	if metrics == nil {
		metrics = nestedStatusMap(data, "pool", "metrics")
	}
	return truthyValue(metrics["accepted_block_recent"]) || number(mapString(metrics, "accepted_block_submissions_delta")) > 0
}

func (a *App) stratumRuntime(ctx context.Context) TabView {
	jobState, err := a.poolJobStateData(ctx)
	return stratumRuntimeView(jobState, err)
}

func stratumRuntimeView(jobState map[string]any, err error) TabView {
	view := baseTab("Stratum Runtime", "5s", jobState, err)
	if jobState == nil && err == nil {
		view.State = "Loading"
		view.StateTone = "neutral"
		view.Evidence = []Evidence{{"Stratum Runtime", "Loading"}}
		return view
	}
	if err != nil {
		view.State = "Unavailable"
		view.StateTone = "down"
		return view
	}
	switch lower(stringValue(jobState, "status")) {
	case "ok":
		view.State = "Ready"
		view.StateTone = "ok"
	case "degraded", "warning":
		view.State = "Degraded"
		view.StateTone = "warn"
	default:
		view.State = titleStatus(first(stringValue(jobState, "status"), "unknown"))
		view.StateTone = "neutral"
	}
	view.Evidence = []Evidence{
		{"Status", first(stringValue(jobState, "status"), "unknown")},
		{"Reason", first(stringValue(jobState, "reason_code"), "unknown")},
		{"Active", integerStat(stringValue(jobState, "active_connections"))},
		{"Authorized", integerStat(stringValue(jobState, "authorized_connections"))},
		{"Subscribed", integerStat(stringValue(jobState, "subscribed_connections"))},
		{"Ready", integerStat(stringValue(jobState, "ready_connections"))},
		{"Without Job", integerStat(stringValue(jobState, "connections_without_current_job"))},
		{"Awaiting Broadcast", integerStat(stringValue(jobState, "awaiting_current_job_connections"))},
		{"Stale Jobs", integerStat(stringValue(jobState, "stale_current_job_connections"))},
		{"Invalidated Jobs", integerStat(stringValue(jobState, "invalid_current_job_connections"))},
		{"Expired Jobs", integerStat(stringValue(jobState, "expired_current_job_connections"))},
		{"Template Seq", integerStat(stringValue(jobState, "current_template_seq"))},
		{"Last Broadcast", milliseconds(stringValue(jobState, "last_broadcast_age_ms"))},
		{"Max Job Age", milliseconds(stringValue(jobState, "max_current_job_age_ms"))},
	}
	routerNode := nestedStatusMap(jobState, "router", "node")
	if routerNode != nil {
		view.Secondary = []Evidence{
			{"Router Node", first(mapString(routerNode, "name"), "node")},
			{"Router Selected", yesNo(truthyValue(routerNode["selected"]))},
			{"Router Healthy", yesNo(truthyValue(routerNode["healthy"]))},
			{"Router Reason", first(mapString(routerNode, "reason"), "unknown")},
			{"Template Age", milliseconds(mapString(routerNode, "last_template_age_ms"))},
			{"Last Submit Age", milliseconds(mapString(routerNode, "last_submit_age_ms"))},
		}
	}
	view.Rows = stratumClientRows(jobState)
	return view
}

func (a *App) poolJobStateData(ctx context.Context) (map[string]any, error) {
	if a.runtime != nil && a.runtime.Ready() {
		if payload, err := a.runtime.poolJobStatePayload(ctx); err == nil && payload != nil {
			return payload, nil
		}
	}
	return nil, errors.New("pool job-state snapshot is not ready")
}

func stratumClientRows(jobState map[string]any) []map[string]string {
	clients := listMaps(jobState["clients"])
	out := make([]map[string]string, 0, len(clients))
	for _, client := range clients {
		out = append(out, map[string]string{
			"mac":        first(normalizedMAC(mapString(client, "asic_mac")), "not known"),
			"ip":         first(mapString(client, "remote_host"), hostFromAddress(mapString(client, "address")), "not known"),
			"ready":      yesNo(truthyValue(client["ready"])),
			"reason":     first(mapString(client, "reason_code"), "unknown"),
			"job_id":     shortText(mapString(client, "current_job_id"), 30),
			"job_age":    milliseconds(mapString(client, "current_job_age_ms")),
			"seq":        integerStat(mapString(client, "template_seq")),
			"backend":    first(mapString(client, "backend"), "unknown"),
			"parent":     shortHash(mapString(client, "parent")),
			"pdiff":      decimalString(number(mapString(client, "pdiff"))),
			"expired":    integerStat(mapString(client, "expired_rejects")),
			"low_diff":   integerStat(mapString(client, "low_diff_rejects")),
			"protocol":   yesNo(truthyValue(client["extranonce_subscribed"])),
			"subscribed": yesNo(truthyValue(client["subscribed"])),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return first(out[i]["ip"], out[i]["mac"]) < first(out[j]["ip"], out[j]["mac"])
	})
	return out
}

func hostFromAddress(value string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(value))
	if err == nil {
		return host
	}
	return strings.TrimSpace(value)
}

func shortText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit < 8 {
		return value[:limit]
	}
	return value[:limit-4] + "..."
}

func (a *App) chainDAG(ctx context.Context) TabView {
	data, err := a.chainDAGData(ctx, chainDAGDisplayWindow())
	view := baseTab("Chain DAG", "5s", data, err)
	if data == nil && err == nil {
		view.State = "Loading"
		view.StateTone = "neutral"
		view.ChartJSON = template.JS("[]")
		return view
	}
	if err != nil {
		view.State = "Unavailable"
		view.StateTone = "down"
		view.ChartJSON = template.JS("[]")
		return view
	}
	switch lower(stringValue(data, "status")) {
	case "ok":
		view.State = "Live"
		view.StateTone = "ok"
	case "warming":
		view.State = "Warming"
		view.StateTone = "warn"
	case "stale", "degraded":
		view.State = titleStatus(stringValue(data, "status"))
		view.StateTone = "warn"
	case "error":
		view.State = "Error"
		view.StateTone = "down"
	default:
		view.State = titleStatus(first(stringValue(data, "status"), "unknown"))
		view.StateTone = "neutral"
	}
	window := mapValue(data, "window")
	tip := mapValue(data, "tip")
	view.Evidence = []Evidence{
		{"Mode", first(stringValue(data, "mode"), "unknown")},
		{"Source", first(stringValue(data, "source"), "unknown")},
		{"Tip Height", integerStat(mapString(tip, "height"))},
		{"Tip Hash", shortHash(mapString(tip, "hash"))},
		{"Window Available", integerStat(mapString(window, "available"))},
		{"Requested Window", integerStat(mapString(window, "requested"))},
		{"Edges", integerStat(strconv.Itoa(len(listMaps(data["edges"]))))},
	}
	view.Rows = chainDAGRows(data)
	view.Warnings = stringSlice(data["warnings"])
	view.ChartJSON = template.JS(chainDAGGraphJSON(data))
	return view
}

func chainDAGDisplayWindow() int {
	window := intEnv("BDAG_CHAIN_DAG_DISPLAY_WINDOW", defaultNativeDAGVisibleWindow)
	if window < 1 {
		return defaultNativeDAGVisibleWindow
	}
	return chainDAGWindowLimit(window)
}

func (a *App) chainDAGData(ctx context.Context, limit int) (map[string]any, error) {
	if a.runtime != nil && a.runtime.Ready() {
		return a.runtime.chainWindowPayload(ctx, limit)
	}
	return nil, errors.New("Redis live runtime unavailable")
}

func chainDAGRows(data map[string]any) []map[string]string {
	nodes := listMaps(data["nodes"])
	out := make([]map[string]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, map[string]string{
			"height":   integerStat(mapString(node, "height")),
			"hash":     first(mapString(node, "short_hash"), shortHash(mapString(node, "hash"))),
			"parent":   shortHash(mapString(node, "main_parent_hash")),
			"producer": first(mapString(node, "producer_short"), shortAddress(mapString(node, "producer"))),
			"local":    yesNo(truthyValue(node["local_pool"])),
			"parents":  integerStat(mapString(node, "parent_count")),
			"missing":  integerStat(mapString(node, "missing_parent_count")),
			"time":     humanTime(first(mapString(node, "timestamp"), "not known")),
		})
	}
	if len(out) > 80 {
		return out[:80]
	}
	return out
}

func chainDAGGraphJSON(data map[string]any) string {
	graph := map[string]any{
		"mode":          stringValue(data, "mode"),
		"source":        stringValue(data, "source"),
		"live_pipeline": stringValue(data, "live_pipeline"),
		"best_effort":   truthyValue(data["best_effort"]),
		"coverage":      mapValue(data, "coverage"),
		"window":        mapValue(data, "window"),
		"nodes":         chainDAGGraphNodes(data),
		"edges":         chainDAGGraphEdges(data),
		"warnings":      stringSlice(data["warnings"]),
	}
	raw, err := json.Marshal(graph)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func chainDAGGraphNodes(data map[string]any) []map[string]any {
	nodes := listMaps(data["nodes"])
	out := make([]map[string]any, 0, len(nodes))
	localProducers := localProducerSet()
	for _, node := range nodes {
		copyNode := map[string]any{}
		for key, value := range node {
			copyNode[key] = value
		}
		producer := strings.ToLower(strings.TrimSpace(stringValue(copyNode, "producer")))
		copyNode["producer"] = producer
		copyNode["producer_short"] = first(stringValue(copyNode, "producer_short"), shortAddress(producer))
		copyNode["producer_color"] = first(stringValue(copyNode, "producer_color"), stableProducerColor(producer))
		copyNode["producer_pending"] = truthyValue(copyNode["producer_pending"]) || producer == ""
		producerSource := "unknown"
		if producer == "" {
			producerSource = "pending-reward-attribution"
		}
		copyNode["producer_source"] = first(stringValue(copyNode, "producer_source"), producerSource)
		copyNode["local_pool"] = truthyValue(copyNode["local_pool"]) || localProducers[producer]
		copyNode["tip"] = truthyValue(copyNode["tip"])
		copyNode["dag_set"] = first(stringValue(copyNode, "dag_set"), "settled")
		out = append(out, copyNode)
	}
	return out
}

func chainDAGGraphEdges(data map[string]any) []map[string]any {
	edges := listMaps(data["edges"])
	out := make([]map[string]any, 0, len(edges))
	for _, edge := range edges {
		copyEdge := map[string]any{}
		for key, value := range edge {
			copyEdge[key] = value
		}
		out = append(out, copyEdge)
	}
	return out
}

func collectorPoolState(data map[string]any) (string, string) {
	mode := lower(stringValue(data, "mode"))
	overall := lower(stringValue(data, "overall"))
	syncStatus := lower(first(nestedString(data, "sync_progress", "status"), nestedString(data, "nodes", "node", "status")))
	accepting := boolValue(data, "can_accept_shares")
	submitReady := boolValue(data, "can_submit_blocks")

	switch {
	case boolValue(data, "collector_status_unavailable") || mode == "waiting_for_status_sample":
		return "Waiting for Collector", "warn"
	case poolServiceKnownDown(data) || overall == "down" || mode == "down" || mode == "pool_down":
		return "Down", "down"
	case mode == "mining":
		if templateCoinbaseUnsafe(data) || templateHealthDegraded(data) || asicFleetCapacityDegraded(data) {
			return "Mining Degraded", "warn"
		}
		if submitReady {
			return "Mining", "ok"
		}
		return "Mining", "warn"
	case mode == "catchup_pause" || mode == "node_syncing" || mode == "syncing" || (syncStatus != "" && syncStatus != "synced" && syncStatus != "unknown"):
		return "Waiting for Node Sync", "warn"
	case mode == "ready_no_miners" || mode == "sync_only_no_miners" || mode == "no_miners":
		return "Waiting for a Miner", "warn"
	case overall == "degraded":
		return "Degraded", "warn"
	case accepting || submitReady:
		if submitReady {
			if asicFleetCapacityDegraded(data) {
				return "Mining Degraded", "warn"
			}
			return "Mining", "ok"
		}
		return "Mining", "warn"
	case overall == "ok":
		return "Degraded", "warn"
	default:
		return "Unknown", "neutral"
	}
}

func asicFleetCapacityDegraded(data map[string]any) bool {
	expected := expectedASICCount(data)
	ready := readyMinerJobCount(data)
	if expected <= 1 {
		return false
	}
	return ready >= 0 && ready < expected
}

func asicFleetCapacityWarning(data map[string]any) string {
	expected := expectedASICCount(data)
	ready := readyMinerJobCount(data)
	if expected <= 1 || ready < 0 || ready >= expected {
		return ""
	}
	return fmt.Sprintf("ASIC fleet degraded: %d of %d expected ASIC lanes have ready jobs.", ready, expected)
}

func expectedASICCount(data map[string]any) int {
	minerHealth := firstNestedMap(data, []string{"miner_health"})
	best := 0
	for _, key := range []string{"configured_count", "managed_count", "tracked_count"} {
		if value := int(number(mapString(minerHealth, key))); value > best {
			best = value
		}
	}
	for _, key := range []string{"managed_miners", "miners"} {
		if rows, ok := minerHealth[key].([]any); ok {
			count := 0
			for _, raw := range rows {
				row, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if lower(stringValue(row, "device_type")) == "asic" || stringValue(row, "mac") != "" {
					count++
				}
			}
			if count > best {
				best = count
			}
		}
	}
	return best
}

func readyMinerJobCount(data map[string]any) int {
	value := firstNestedString(data,
		[]string{"template_safety", "ready_miner_job_count"},
		[]string{"pool_health", "template_safety", "ready_miner_job_count"},
		[]string{"pool_metrics", "source_job_health", "ready_miners"},
		[]string{"pool_health", "source_job_health", "ready_miners"},
	)
	if value == "" {
		return -1
	}
	return int(number(value))
}

func templateCoinbaseUnsafe(data map[string]any) bool {
	valid, known := boolKnown(firstNestedAny(data,
		[]string{"template_safety", "template_coinbase_valid"},
		[]string{"pool_health", "template_safety", "template_coinbase_valid"},
		[]string{"sync_health", "template_coinbase_valid"},
	))
	return known && !valid
}

func templateHealthDegraded(data map[string]any) bool {
	for _, value := range []any{
		firstNestedAny(data,
			[]string{"template_health", "mineable_now"},
			[]string{"pool_health", "template_health", "mineable_now"},
		),
		firstNestedAny(data,
			[]string{"template_health", "submit_ready"},
			[]string{"pool_health", "template_health", "submit_ready"},
		),
	} {
		ready, known := boolKnown(value)
		if known && !ready {
			return true
		}
	}
	return false
}

func templateCoinbaseValidStat(data map[string]any) string {
	valid, known := boolKnown(firstNestedAny(data,
		[]string{"template_safety", "template_coinbase_valid"},
		[]string{"pool_health", "template_safety", "template_coinbase_valid"},
		[]string{"sync_health", "template_coinbase_valid"},
	))
	if !known {
		return "unknown"
	}
	return yesNo(valid)
}

func templateZeroCoinbaseRejectRate(data map[string]any) string {
	value := firstNestedString(data,
		[]string{"template_safety", "zero_coinbase_reject_rate_per_second"},
		[]string{"pool_health", "template_safety", "zero_coinbase_reject_rate_per_second"},
	)
	if value == "" {
		total := firstNestedString(data,
			[]string{"template_safety", "zero_coinbase_reject_total"},
			[]string{"pool_health", "template_safety", "zero_coinbase_reject_total"},
			[]string{"sync_health", "zero_coinbase_reject_total"},
		)
		if total == "" {
			return "unknown"
		}
		return "0/min"
	}
	return decimalString(number(value)*60) + "/min"
}

func templateReadyMinerJobs(data map[string]any) string {
	return integerStat(firstNestedString(data,
		[]string{"template_safety", "ready_miner_job_count"},
		[]string{"pool_health", "template_safety", "ready_miner_job_count"},
		[]string{"pool_metrics", "source_job_health", "ready_miners"},
		[]string{"pool_health", "source_job_health", "ready_miners"},
	))
}

func templateLocalStaleRatio(data map[string]any) string {
	value := firstNestedString(data,
		[]string{"template_safety", "local_stale_ratio"},
		[]string{"pool_health", "template_safety", "local_stale_ratio"},
		[]string{"sync_health", "local_stale_ratio"},
	)
	if value == "" {
		return "unknown"
	}
	return decimalString(number(value)*100) + "%"
}

func templateAutomationRestart(data map[string]any) string {
	marker := firstNestedMap(data,
		[]string{"template_safety", "automation_restart"},
		[]string{"pool_health", "template_safety", "automation_restart"},
	)
	if marker == nil {
		return "none"
	}
	recent, known := boolKnown(marker["recent"])
	if !known || !recent {
		return "none"
	}
	name := first(mapString(marker, "name"), mapString(marker, "mode"))
	status := mapString(marker, "status")
	if status != "" {
		return first(name, "restart") + " " + status
	}
	return first(name, "restart")
}

func collectorCacheNote(data map[string]any) string {
	cache := nestedStatusMap(data, "collector_api_cache")
	if cache == nil {
		return ""
	}
	if !truthyValue(cache["stale"]) {
		return ""
	}
	age := mapString(cache, "age_seconds")
	source := mapString(cache, "source")
	if age == "" {
		return "Showing the last collector status sample while a refresh is in progress."
	}
	if source == "" {
		return "Showing a " + age + "s old collector status sample while a refresh is in progress."
	}
	return "Showing a " + age + "s old collector status sample from " + source + " while a refresh is in progress."
}

func minerSourceRows(data map[string]any) []map[string]any {
	rows := listMaps(nestedAny(data, "miner_health", "managed_miners"))
	if len(rows) == 0 {
		rows = listMaps(nestedAny(data, "miner_health", "miners"))
	}
	return rows
}

func minerDisplayRows(data map[string]any) []map[string]any {
	rows := minerSourceRows(data)
	managed := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !truthyValue(row["managed"]) {
			continue
		}
		deviceType := lower(mapString(row, "device_type"))
		if deviceType != "" && deviceType != "asic" {
			continue
		}
		model := first(mapString(row, "model"), mapString(row, "device_model"), mapString(row, "machine"))
		if model != "" && !strings.Contains(lower(model), "x100") {
			continue
		}
		managed = append(managed, row)
	}
	if len(managed) > 0 {
		return managed
	}

	x100 := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if minerRowLooksX100(row) {
			x100 = append(x100, row)
		}
	}
	if len(x100) > 0 {
		return x100
	}
	return rows
}

func minerRowLooksX100(row map[string]any) bool {
	for _, value := range []string{
		mapString(row, "model"),
		mapString(row, "device_model"),
		mapString(row, "machine"),
		mapString(row, "hardware"),
	} {
		if strings.Contains(lower(value), "x100") {
			return true
		}
	}
	return false
}

func minerPresenceSignal(data map[string]any) float64 {
	count := maxNumber(
		nestedNumber(data, "miner_health", "connected_count_effective"),
		nestedNumber(data, "miner_health", "connected_count"),
		nestedNumber(data, "pool_metrics", "active_connections"),
		nestedNumber(data, "pool", "metrics", "active_connections"),
		nestedNumber(data, "pool", "metrics_active_connections"),
		nestedNumber(data, "pool_metrics", "source_job_health", "authorized_miners"),
		nestedNumber(data, "pool_metrics", "source_job_health", "ready_miners"),
		nestedNumber(data, "pool", "source_job_health", "authorized_miners"),
		nestedNumber(data, "pool", "source_job_health", "ready_miners"),
		nestedNumber(data, "pool_health", "source_job_health", "authorized_miners"),
		nestedNumber(data, "pool_health", "source_job_health", "ready_miners"),
	)
	if count > 0 {
		return count
	}
	return connectedMinerRowCount(data)
}

func minerInventoryStatus(data map[string]any) string {
	if managed := nestedNumber(data, "miner_health", "managed_count"); managed > 0 {
		return decimalInt(managed)
	}
	count := maxNumber(
		nestedNumber(data, "miner_health", "tracked_count"),
		nestedNumber(data, "miner_health", "connected_count"),
		nestedNumber(data, "miner_health", "ok_count"),
		nestedNumber(data, "miner_health", "stratum_count"),
		connectedMinerRowCount(data),
	)
	if count > 0 {
		return decimalInt(count)
	}
	if minerPresenceSignal(data) > 0 {
		return "activity only"
	}
	return "0"
}

func connectedMinerRowCount(data map[string]any) float64 {
	var count float64
	for _, path := range []any{
		nestedAny(data, "miner_health", "managed_miners"),
		nestedAny(data, "miner_health", "miners"),
	} {
		for _, row := range listMaps(path) {
			if truthyValue(row["connected"]) || lower(mapString(row, "status")) == "connected" {
				count++
			}
		}
	}
	return count
}

func maxNumber(values ...float64) float64 {
	var out float64
	for _, value := range values {
		if value > out {
			out = value
		}
	}
	return out
}

func truthyValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch lower(v) {
		case "1", "true", "yes", "y", "connected":
			return true
		}
	case float64:
		return v > 0
	case int:
		return v > 0
	case int64:
		return v > 0
	}
	return false
}

func boolKnown(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		switch lower(v) {
		case "1", "true", "yes", "y", "ok":
			return true, true
		case "0", "false", "no", "n", "bad":
			return false, true
		}
	case float64:
		return v > 0, true
	case int:
		return v > 0, true
	case int64:
		return v > 0, true
	}
	return false, false
}

func poolServiceKnownDown(data map[string]any) bool {
	running, known := statusBoolField(nestedStatusMap(data, "containers", "pool"), "running")
	if known && !running {
		return true
	}
	status := lower(nestedString(data, "containers", "pool", "status"))
	switch status {
	case "missing", "exited", "stopped", "dead":
		return true
	}
	return poolFailureReason(data) != ""
}

func poolFailureReason(data map[string]any) string {
	for _, key := range []string{"blocking_failures", "stack_failures", "failures"} {
		for _, item := range stringList(data, key) {
			reason := lower(item)
			if strings.Contains(reason, "pool is not running") || strings.Contains(reason, "pool not running") {
				return item
			}
		}
	}
	if err := nestedString(data, "containers", "pool", "error"); strings.Contains(lower(err), "container not found") {
		return "pool container not found"
	}
	return ""
}

func nestedStatusMap(data map[string]any, keys ...string) map[string]any {
	var current any = data
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = asMap[key]
	}
	asMap, _ := current.(map[string]any)
	return asMap
}

func statusBoolField(data map[string]any, key string) (bool, bool) {
	if data == nil {
		return false, false
	}
	value, ok := data[key]
	if !ok {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch lower(typed) {
		case "true", "yes", "1", "running":
			return true, true
		case "false", "no", "0", "stopped", "exited":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func (a *App) poolEarnings(ctx context.Context) TabView {
	data, err := a.poolEarningsData(ctx)
	view := baseTab("Pool Earnings", "30s", data, err)
	if data == nil && err == nil {
		view.State = "Loading"
		view.StateTone = "neutral"
		view.RefreshEvery = "5s"
		view.Evidence = []Evidence{{"Pool Earnings", "Loading"}}
		return view
	}
	if err != nil {
		view.State = "API Unavailable"
		view.StateTone = "down"
		return view
	}
	newPoolEarningsSnapshot(data).applyTo(&view)
	return view
}

func (a *App) global(ctx context.Context) TabView {
	return a.globalWithRange(ctx, defaultTrendRange, true)
}

func (a *App) globalWithRange(ctx context.Context, rangeToken string, includeChart bool) TabView {
	data, err := a.globalData(ctx)
	statusData, statusErr := a.statusData(ctx)
	poolRate := poolHashpower(statusData, localPoolHashpowerURL(statusData))
	view := baseTab("Global", "5s", data, err)
	if data == nil && err == nil {
		view.State = "Loading"
		view.StateTone = "neutral"
		view.RefreshEvery = "5s"
		view.Evidence = []Evidence{{"Global", "Loading"}, {"Pool Hashpower", poolRate}}
		view.ChartJSON = template.JS("[]")
		return view
	}
	if err != nil {
		view.State = "API Unavailable"
		view.StateTone = "down"
		view.RefreshEvery = "5s"
		view.Evidence = []Evidence{{"Pool Hashpower", poolRate}}
		view.ChartJSON = template.JS("[]")
		return view
	}
	earningsData, earningsErr := a.poolEarningsData(ctx)
	price := mapValue(earningsData, "price")
	priceOK := mapString(price, "status") == "ok" && mapString(price, "usd") != ""
	usdPrice := ""
	if priceOK {
		usdPrice = mapString(price, "usd")
	}
	view.Evidence = []Evidence{
		{"Current Price USD", priceQuote(usdPrice, "$")},
		{"Pool Hashpower", poolRate},
		{"Chain Block Count", first(nestedString(statusData, "sync_progress", "chain_block_count"), stringValue(statusData, "chain_block_count"), nestedString(statusData, "nodes", "node", "chain_block_count"))},
		{"Main Order", first(nestedString(statusData, "sync_progress", "chain_main_height"), nestedString(statusData, "sync_progress", "main_order"), nestedString(statusData, "nodes", "node", "best_main_order"))},
		{"EVM Block", first(nestedString(statusData, "sync_progress", "current_block"), nestedString(statusData, "nodes", "node", "latest_block"), nestedString(statusData, "wallet", "block_number"))},
		{"Latest Block", first(stringValue(data, "chain_latest_block"), stringValue(data, "latest_block"))},
		{"Data Age", seconds(stringValue(data, "age_seconds"))},
		{"Stream Lag", first(stringValue(data, "stream_lag_blocks"), "0")},
		{"Unique Producers", stringValue(data, "unique_miners")},
		{"Avg Blocks/sec", stringValue(data, "avg_blocks_per_second")},
		{"Top Share", topShare(data)},
	}
	switch lower(stringValue(data, "status")) {
	case "ok":
		view.State = "Live"
		view.StateTone = "ok"
	case "degraded":
		view.State = "Lagging"
		view.StateTone = "warn"
	case "stale":
		view.State = "Stale"
		view.StateTone = "warn"
	default:
		view.State = "Live"
		view.StateTone = "ok"
	}
	if includeChart {
		chartJSON, trendMeta, trendErr := a.trendChart(ctx, rangeToken)
		if trendErr == nil && chartJSON != "" && chartJSON != "[]" {
			view.ChartJSON = template.JS(chartJSON)
			if coverage := stringValue(trendMeta, "coverage_hours"); coverage != "" {
				view.Notes = append(view.Notes, "Producer Trend is served from persistent Redis buckets for "+first(stringValue(trendMeta, "range_label"), defaultTrendRange)+".")
			}
		} else {
			chartJSON, _, _ := globalChartJSON(data)
			view.ChartJSON = template.JS(chartJSON)
			if trendErr != nil {
				view.Notes = append(view.Notes, "Persistent Producer Trend is warming up; showing live window history: "+trendErr.Error())
			}
		}
	}
	if statusErr != nil {
		view.Notes = append(view.Notes, "Node height diagnostics are unavailable from /api/status: "+statusErr.Error())
	}
	if earningsErr != nil {
		view.Notes = append(view.Notes, "Price diagnostics are unavailable from live dashboard state: "+earningsErr.Error())
	}
	return view
}

func (a *App) trendChart(ctx context.Context, rangeToken string) (string, map[string]any, error) {
	cfg := trendRangeConfigFor(rangeToken)
	meta := trendMeta(cfg, nil, nil)
	if a.runtime == nil || !a.runtime.Ready() {
		return "", meta, errors.New("redis runtime unavailable")
	}
	return a.runtime.trendChartJSON(ctx, cfg.Token, localProducerSet())
}

func (a *App) globalPoolEarnings(ctx context.Context) TabView {
	globalData, globalErr := a.globalData(ctx)
	poolEarningsData, poolEarningsErr := a.globalPoolEarningsData(ctx)
	view := baseTab("Estimated Earnings By Pool", "5s", poolEarningsData, poolEarningsErr)
	fallback := false
	if poolEarningsData == nil && poolEarningsErr == nil {
		view.State = "Loading"
		view.StateTone = "neutral"
		view.RefreshEvery = "5s"
		view.Evidence = []Evidence{{"Estimated Earnings By Pool", "Loading"}}
		return view
	}

	producerColors := map[string]string{}
	localProducers := map[string]bool{}
	if globalErr == nil {
		_, producerColors, localProducers = globalChartJSON(globalData)
	} else {
		view.Notes = append(view.Notes, "Global producer colors are unavailable from /api/global: "+globalErr.Error())
	}
	if poolEarningsErr != nil {
		if hasProducerRows(globalData) && globalWindowComplete(globalData) {
			poolEarningsData = globalData
			fallback = true
			view.Notes = append(view.Notes, "Fixed 600-block estimated pool earnings API is unavailable; temporarily showing the last complete Global 600-block table: "+poolEarningsErr.Error())
		} else {
			view.State = "Warming"
			view.StateTone = "warn"
			view.Notes = append(view.Notes, "Estimated pool earnings API is unavailable and no complete 600-block Global window is ready: "+poolEarningsErr.Error())
		}
	} else if !hasProducerRows(poolEarningsData) && hasProducerRows(globalData) && globalWindowComplete(globalData) {
		poolEarningsData = globalData
		fallback = true
		view.Notes = append(view.Notes, "Fixed 600-block estimated pool earnings API returned no producer rows; temporarily showing the last complete Global 600-block table.")
	}
	view.Title = poolEarningsTitle(poolEarningsData, fallback)
	if globalWindowComplete(poolEarningsData) || globalBestEffortDisplayable(poolEarningsData) {
		view.Rows = poolEarningRows(poolEarningsData, producerColors, localProducers)
		if !globalWindowComplete(poolEarningsData) {
			view.State = "Best Effort"
			view.StateTone = "warn"
			view.Notes = append(view.Notes, globalWindowProgressNote(poolEarningsData, fallback))
		} else {
			view.State = "Live"
			view.StateTone = "ok"
		}
	} else {
		view.Rows = nil
		view.State = "Warming"
		view.StateTone = "warn"
		view.Notes = append(view.Notes, globalWindowProgressNote(poolEarningsData, fallback))
	}
	return view
}

func poolEarningsTitle(data map[string]any, fallback bool) string {
	requested := first(stringValue(data, "requested_blocks"), stringValue(data, "global_block_window"), stringValue(data, "effective_block_window"), "600")
	fetched := first(stringValue(data, "attributed_blocks"), stringValue(data, "fetched_blocks"))
	if snapshotFlag(data, "best_effort") && globalBestEffortDisplayable(data) && fetched != "" {
		return "Estimated Earnings By Pool (best-effort live " + fetched + "/" + requested + " blocks)"
	}
	if fallback {
		if fetched != "" && fetched != "0" {
			return "Estimated Earnings By Pool (Global scan fallback: " + fetched + " blocks)"
		}
		return "Estimated Earnings By Pool (Global scan fallback)"
	}
	if requested != "" {
		return "Estimated Earnings By Pool (last complete " + requested + " blocks)"
	}
	return "Estimated Earnings By Pool"
}

func globalBestEffortDisplayable(data map[string]any) bool {
	if data == nil || !hasProducerRows(data) {
		return false
	}
	source := stringValue(data, "source")
	if !snapshotFlag(data, "best_effort") && source != "native-dag-live" && source != "chain-dag-live" {
		return false
	}
	if number(first(stringValue(data, "fetched_blocks"), stringValue(data, "window"))) <= 0 {
		return false
	}
	if age := number(stringValue(data, "age_seconds")); age > 30 {
		return false
	}
	return true
}

func globalWindowComplete(data map[string]any) bool {
	if data == nil {
		return false
	}
	requested := globalRequestedBlocks(data)
	fetched := int(number(first(stringValue(data, "fetched_blocks"), stringValue(data, "window"))))
	if requested <= 0 || fetched != requested {
		return false
	}
	if number(stringValue(data, "unknown_blocks")) > 0 {
		return false
	}
	for _, key := range []string{"partial_scan", "head_only", "maintenance_deferred", "deferred_scan"} {
		if snapshotFlag(data, key) {
			return false
		}
	}
	return hasProducerRows(data)
}

func globalSnapshotUsableForCurrentTrend(data map[string]any) bool {
	if !globalWindowComplete(data) {
		return false
	}
	if status := lower(stringValue(data, "status")); status != "" && status != "ok" {
		return false
	}
	requested := globalRequestedBlocks(data)
	maxLag := math.Max(30, float64(requested)/10)
	if lag := number(stringValue(data, "stream_lag_blocks")); lag > maxLag {
		return false
	}
	return true
}

func globalSnapshotUsableForBestEffortTrend(data map[string]any, now time.Time) bool {
	if data == nil || !hasProducerRows(data) {
		return false
	}
	for _, key := range []string{"head_only", "maintenance_deferred", "deferred_scan"} {
		if snapshotFlag(data, key) {
			return false
		}
	}
	status := lower(stringValue(data, "status"))
	if status == "error" || status == "unavailable" {
		return false
	}
	updatedEpoch := int64Value(data["updated_at_epoch"])
	if updatedEpoch <= 0 {
		if parsed, err := time.Parse(time.RFC3339, first(stringValue(data, "updated_at"), stringValue(data, "generated_at"))); err == nil {
			updatedEpoch = parsed.Unix()
		}
	}
	if updatedEpoch <= 0 || now.Sub(time.Unix(updatedEpoch, 0).UTC()) > 2*time.Minute {
		return false
	}
	fetched := int(number(first(stringValue(data, "fetched_blocks"), stringValue(data, "window"))))
	return fetched > 0
}

func globalRequestedBlocks(data map[string]any) int {
	requested := int(number(first(stringValue(data, "requested_blocks"), stringValue(data, "global_block_window"), stringValue(data, "effective_block_window"), "600")))
	if requested <= 0 {
		return defaultGlobalBlockWindow
	}
	return requested
}

func snapshotFlag(data map[string]any, key string) bool {
	if value, known := boolKnown(data[key]); known {
		return value
	}
	return false
}

func globalWindowProgressNote(data map[string]any, fallback bool) string {
	requested := globalRequestedBlocks(data)
	fetched := int(number(first(stringValue(data, "attributed_blocks"), stringValue(data, "fetched_blocks"), stringValue(data, "window"))))
	if fetched < 0 {
		fetched = 0
	}
	source := "canonical API"
	if stringValue(data, "global_live_source") == "chain-dag" || stringValue(data, "source") == "chain-dag-live" {
		source = "Chain DAG Redis window"
	}
	if fallback {
		source = "Global scan fallback"
	}
	status := first(stringValue(data, "status"), "warming")
	if globalBestEffortDisplayable(data) {
		if !snapshotFlag(data, "window_contiguous") {
			return fmt.Sprintf("Estimated pool earnings is waiting for a complete %d-block window from the %s; current state is %s with %d/%d blocks indexed. Showing best-effort producer visibility from a non-contiguous Redis window; do not treat it as complete 600-block earnings truth.", requested, source, status, fetched, requested)
		}
		return fmt.Sprintf("Estimated pool earnings is waiting for a complete %d-block window from the %s; current state is %s with %d/%d blocks indexed. Showing best-effort producer visibility while the canonical window warms; do not treat it as complete 600-block earnings truth.", requested, source, status, fetched, requested)
	}
	return fmt.Sprintf("Estimated pool earnings is waiting for a complete %d-block window from the %s; current state is %s with %d/%d blocks indexed, so partial producer rows are not shown as earnings truth.", requested, source, status, fetched, requested)
}

func baseTab(title, refresh string, data map[string]any, err error) TabView {
	view := TabView{Title: title, RefreshEvery: refresh, UpdatedAt: stringValue(data, "updated_at")}
	if view.UpdatedAt == "" {
		view.UpdatedAt = stringValue(data, "generated_at")
	}
	view.UpdatedAt = humanTime(view.UpdatedAt)
	if err != nil {
		view.APIError = err.Error()
	}
	return view
}

func (a *App) statusData(ctx context.Context) (map[string]any, error) {
	if a.statusDataProvider != nil {
		return a.statusDataProvider(ctx)
	}
	if a.runtime != nil && a.runtime.Ready() {
		if payload, err := a.runtime.statusPayload(ctx); err == nil && payload != nil {
			return payload, nil
		}
	}
	return a.api.cachedOrRefresh("/api/status", 5*time.Second)
}

func (a *App) globalData(ctx context.Context) (map[string]any, error) {
	if a.runtime != nil && a.runtime.Ready() {
		if live, err := a.liveDAGGlobalPayload(ctx, nil); err == nil && live != nil {
			return live, nil
		} else if err != nil {
			return nativeDAGGlobalWarmingPayload("native DAG live window is unavailable: " + err.Error()), nil
		}
		return nativeDAGGlobalWarmingPayload("native DAG live window is waiting for producer attribution."), nil
	}
	return a.api.cachedOrRefresh("/api/global", time.Minute)
}

func (a *App) liveDAGGlobalPayload(ctx context.Context, canonical map[string]any) (map[string]any, error) {
	if a == nil || a.runtime == nil || !a.runtime.Ready() {
		return nil, nil
	}
	payload, err := a.runtime.nativeDAGLivePayload(ctx, defaultGlobalBlockWindow)
	if err != nil || payload == nil {
		return nil, err
	}
	if !nativeDAGGlobalSourceUsable(payload) {
		return nil, nil
	}
	out := nativeDAGGlobalPayload(payload, canonical)
	if out == nil {
		return nil, nil
	}
	out["source"] = "chain-dag-live"
	out["source_detail"] = "redis-native-dag-live"
	out["global_live_source"] = "chain-dag"
	return out, nil
}

func nativeDAGGlobalSourceUsable(payload map[string]any) bool {
	if payload == nil || !chainDAGPayloadHasNodes(payload) {
		return false
	}
	if status := lower(stringValue(payload, "status")); status == "stale" || status == "error" || status == "unavailable" {
		return false
	}
	if strings.TrimSpace(stringValue(payload, "source_error")) != "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(stringValue(payload, "live_pipeline_status")), "source-degraded") {
		return false
	}
	coverage := mapValue(payload, "coverage")
	requested := number(first(mapString(coverage, "requested"), mapString(payload, "requested")))
	failures := number(first(mapString(coverage, "failures"), mapString(payload, "failures")))
	if requested > 0 && failures >= requested {
		return false
	}
	age := number(stringValue(payload, "age_seconds"))
	return age <= 0 || age <= 30
}

func nativeDAGGlobalWarmingPayload(reason string) map[string]any {
	now := time.Now().UTC()
	out := map[string]any{
		"status":                 "warming",
		"updated_at":             now.Format(time.RFC3339),
		"updated_at_epoch":       now.Unix(),
		"age_seconds":            0,
		"source":                 "chain-dag-live",
		"source_detail":          "redis-native-dag-live",
		"global_live_source":     "chain-dag",
		"requested_blocks":       defaultGlobalBlockWindow,
		"fetched_blocks":         0,
		"attributed_blocks":      0,
		"window_complete":        false,
		"attribution_complete":   false,
		"partial_scan":           true,
		"best_effort":            false,
		"unique_miners":          0,
		"avg_blocks_per_second":  "0",
		"chain_clusters":         []any{},
		"miner_estimates":        []any{},
		"canonical_global_state": "warming",
	}
	if strings.TrimSpace(reason) != "" {
		out["warnings"] = []string{reason}
	}
	return out
}

func nativeDAGGlobalPayload(dag map[string]any, canonical map[string]any) map[string]any {
	nodes := listMaps(dag["nodes"])
	if len(nodes) == 0 {
		return nil
	}
	requested := defaultGlobalBlockWindow
	if window := mapValue(dag, "window"); number(mapString(window, "requested")) > 0 {
		requested = maxInt(requested, int(number(mapString(window, "requested"))))
	}
	requested = chainDAGWindowLimit(requested)
	if len(nodes) > requested {
		nodes = nodes[:requested]
	}
	counts := map[string]int{}
	lastSeen := map[string]time.Time{}
	local := map[string]bool{}
	minTS, maxTS := time.Time{}, time.Time{}
	latestOrder := 0.0
	localSet := localProducerSet()
	for _, node := range nodes {
		producer := strings.ToLower(strings.TrimSpace(mapString(node, "producer")))
		if producer == "" || boolValue(node, "producer_pending") {
			continue
		}
		counts[producer]++
		if boolValue(node, "local_pool") || localSet[producer] {
			local[producer] = true
		}
		if parsed, err := time.Parse(time.RFC3339, mapString(node, "timestamp")); err == nil {
			if minTS.IsZero() || parsed.Before(minTS) {
				minTS = parsed
			}
			if parsed.After(maxTS) {
				maxTS = parsed
			}
			if parsed.After(lastSeen[producer]) {
				lastSeen[producer] = parsed
			}
		}
		if order := number(mapString(node, "order")); order > latestOrder {
			latestOrder = order
		}
		if order := number(mapString(node, "layout_order")); order > latestOrder {
			latestOrder = order
		}
	}
	if len(counts) == 0 {
		return nil
	}
	type producerCount struct {
		Key   string
		Count int
	}
	producers := make([]producerCount, 0, len(counts))
	total := 0
	for key, count := range counts {
		producers = append(producers, producerCount{Key: key, Count: count})
		total += count
	}
	sort.SliceStable(producers, func(i, j int) bool {
		if producers[i].Count == producers[j].Count {
			return producers[i].Key < producers[j].Key
		}
		return producers[i].Count > producers[j].Count
	})
	rows := make([]any, 0, len(producers))
	for rank, item := range producers {
		share := 0.0
		if total > 0 {
			share = float64(item.Count) / float64(total) * 100
		}
		last := ""
		lastEpoch := int64(0)
		if !lastSeen[item.Key].IsZero() {
			last = lastSeen[item.Key].UTC().Format(time.RFC3339)
			lastEpoch = lastSeen[item.Key].Unix()
		}
		rows = append(rows, map[string]any{
			"rank":            rank + 1,
			"address":         item.Key,
			"address_short":   shortAddress(item.Key),
			"blocks":          item.Count,
			"share_percent":   strconv.FormatFloat(share, 'f', 2, 64),
			"last_seen_at":    last,
			"last_seen_epoch": lastEpoch,
			"local_pool":      local[item.Key],
			"rpc_sources":     []string{"native-dag-live"},
		})
	}
	updatedAt := first(stringValue(dag, "updated_at"), time.Now().UTC().Format(time.RFC3339))
	updatedEpoch := int64Value(dag["updated_at_epoch"])
	if updatedEpoch <= 0 {
		if parsed, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			updatedEpoch = parsed.Unix()
		}
	}
	windowSeconds := 0
	if !minTS.IsZero() && !maxTS.IsZero() && maxTS.After(minTS) {
		windowSeconds = int(maxTS.Sub(minTS).Seconds())
	}
	if windowSeconds <= 0 {
		windowSeconds = int(number(stringValue(canonical, "scan_window_seconds")))
	}
	availableBlocks := len(nodes)
	out := map[string]any{
		"status":                 "ok",
		"updated_at":             updatedAt,
		"updated_at_epoch":       updatedEpoch,
		"age_seconds":            number(stringValue(dag, "age_seconds")),
		"source":                 "chain-dag-live",
		"source_detail":          "redis-native-dag-live",
		"global_live_source":     "chain-dag",
		"data_version":           1,
		"requested_blocks":       requested,
		"fetched_blocks":         availableBlocks,
		"attributed_blocks":      total,
		"scan_window_seconds":    windowSeconds,
		"scan_window_hours":      math.Max(float64(windowSeconds)/3600.0, 1.0/3600.0),
		"window_complete":        availableBlocks >= requested && total >= requested,
		"window_contiguous":      availableBlocks >= requested,
		"attribution_complete":   total >= availableBlocks,
		"partial_scan":           availableBlocks < requested || total < requested,
		"best_effort":            availableBlocks < requested || total < requested,
		"latest_block":           int64(latestOrder),
		"chain_latest_block":     int64(latestOrder),
		"stream_lag_blocks":      0,
		"unique_miners":          len(producers),
		"avg_blocks_per_second":  decimalString(float64(total) / math.Max(float64(windowSeconds), 1)),
		"chain_clusters":         rows,
		"miner_estimates":        rows,
		"canonical_global_state": "stale",
	}
	warnings := stringSlice(dag["warnings"])
	warnings = appendUniqueWarning(warnings, "Global producer counts are derived from the live Chain DAG Redis window so Chain DAG and Global use the same recent-block stream.")
	if availableBlocks < requested {
		warnings = appendUniqueWarning(warnings, fmt.Sprintf("Chain DAG live window has %d/%d blocks; showing best-effort producer visibility until the full window is retained.", availableBlocks, requested))
	}
	if total < availableBlocks {
		warnings = appendUniqueWarning(warnings, fmt.Sprintf("Chain DAG live window has producer attribution for %d/%d retained blocks; unattributed blocks are omitted from work share until coinbase attribution catches up.", total, availableBlocks))
	}
	if dashboardNodeRPCBudget.backoffActive() {
		out["best_effort"] = true
		warnings = appendUniqueWarning(warnings, "Serving native DAG Redis estimate while node RPC backoff is active; values may lag.")
	}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	updateSnapshotAge(out)
	return out
}

func (a *App) poolEarningsData(ctx context.Context) (map[string]any, error) {
	if payload, err := a.runtimePoolEarningsData(ctx); err == nil && payload != nil {
		return payload, nil
	}
	return a.api.cachedOrRefresh("/api/earnings", 30*time.Second)
}

func (a *App) runtimePoolEarningsData(ctx context.Context) (map[string]any, error) {
	statusData, err := a.statusData(ctx)
	if err != nil || statusData == nil {
		return nil, err
	}
	globalData, _ := a.globalData(ctx)
	localBDAGHour := localPoolBDAGPerHour(globalData)
	miners := minerSourceRows(statusData)
	estimates := minerEarningEstimatesFromStatus(miners, localBDAGHour)
	if len(estimates) == 0 {
		return nil, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	price := a.currentBDAGPriceSnapshot(ctx)
	return map[string]any{
		"status":                 "ok",
		"source":                 "redis-live-estimate",
		"updated_at":             now,
		"generated_at":           now,
		"miner_estimates":        estimates,
		"price":                  price,
		"payment_wallet_balance": map[string]any{"total_bdag": "not known", "ok_address_count": 0},
		"credit_wallet_balance":  map[string]any{"total_bdag": "not known", "ok_address_count": 0},
		"credits": map[string]any{
			"recent_1h": map[string]any{"total_bdag": strconv.FormatFloat(localBDAGHour, 'f', 2, 64)},
		},
	}, nil
}

func minerEarningEstimatesFromStatus(miners []map[string]any, localBDAGHour float64) []map[string]any {
	managed := make([]map[string]any, 0, len(miners))
	for _, row := range miners {
		if !truthyValue(row["managed"]) {
			continue
		}
		if deviceType := lower(mapString(row, "device_type")); deviceType != "" && deviceType != "asic" {
			continue
		}
		managed = append(managed, row)
	}
	if len(managed) == 0 {
		return nil
	}
	weights := make([]float64, len(managed))
	totalWeight := 0.0
	for i, row := range managed {
		for _, key := range []string{"device_accepted", "accepted", "valid_shares", "avg_hashrate", "hashrate"} {
			if value, ok := parseNumber(mapString(row, key)); ok && value > 0 {
				weights[i] = value
				totalWeight += value
				break
			}
		}
	}
	if totalWeight <= 0 {
		for i := range weights {
			weights[i] = 1
		}
		totalWeight = float64(len(weights))
	}
	out := make([]map[string]any, 0, len(managed))
	for i, row := range managed {
		share := 0.0
		if totalWeight > 0 {
			share = weights[i] / totalWeight
		}
		estimate := localBDAGHour * share
		mac := normalizedMAC(mapString(row, "mac"))
		name := first(mac, mapString(row, "ip"), mapString(row, "identity"), "unknown miner")
		out = append(out, map[string]any{
			"name":                              name,
			"mac":                               mac,
			"device_id":                         first(mapString(row, "device_id"), "mac:"+mac),
			"estimated_wallet_bdag_recent_hour": strconv.FormatFloat(estimate, 'f', 2, 64),
			"estimated_bdag_avg_hour":           strconv.FormatFloat(estimate, 'f', 2, 64),
			"estimated_bdag_total":              "not known",
			"last_share_at":                     first(mapString(row, "last_share_at"), mapString(row, "last_seen_at")),
		})
	}
	return out
}

func localPoolBDAGPerHour(data map[string]any) float64 {
	if data == nil {
		return 0
	}
	windowHours := number(stringValue(data, "scan_window_hours"))
	if windowHours <= 0 {
		if seconds := number(stringValue(data, "scan_window_seconds")); seconds > 0 {
			windowHours = seconds / 3600
		}
	}
	if windowHours <= 0 {
		windowHours = 1
	}
	localSet := localProducerSet()
	for _, row := range producerRows(data) {
		key := strings.ToLower(producerKey(row))
		if key == "" {
			continue
		}
		if !boolValue(row, "local_pool") && !localSet[key] {
			continue
		}
		blocks := number(mapString(row, "blocks"))
		if blocks <= 0 {
			continue
		}
		return blocks / windowHours * trendRewardPerBlockBDAG()
	}
	return 0
}

func (a *App) currentBDAGPriceSnapshot(ctx context.Context) map[string]any {
	if a != nil && a.runtime != nil && a.runtime.Ready() {
		if payload, err := a.runtime.pricePayload(ctx); err == nil && payload != nil {
			return payload
		}
	}
	if usd, ok := configuredBDAGPriceUSD(); ok {
		now := time.Now().UTC()
		return map[string]any{
			"status":           "ok",
			"source":           "configured-env",
			"updated_at":       now.Format(time.RFC3339),
			"updated_at_epoch": now.Unix(),
			"usd":              usd,
			"price_usd":        usd,
		}
	}
	return map[string]any{"status": "unavailable", "source": "not-configured"}
}

func configuredBDAGPriceUSD() (float64, bool) {
	for _, key := range []string{"BDAG_PRICE_USD", "BDAG_USD_PRICE"} {
		if value, ok := parseNumber(os.Getenv(key)); ok && value > 0 {
			return value, true
		}
	}
	return 0, false
}

func trendRewardPerBlockBDAG() float64 {
	for _, key := range []string{"BDAG_REWARD_PER_BLOCK", "BDAG_BLOCK_REWARD_BDAG"} {
		if value, ok := parseNumber(os.Getenv(key)); ok && value > 0 {
			return value
		}
	}
	return 260
}

func priceUSDFromSnapshot(snapshot map[string]any) (float64, bool) {
	for _, key := range []string{"usd", "price_usd", "volume_weighted_usd"} {
		if value, ok := parseNumber(mapString(snapshot, key)); ok && value > 0 {
			return value, true
		}
	}
	return 0, false
}

func (a *App) globalPoolEarningsData(ctx context.Context) (map[string]any, error) {
	if a.runtime != nil && a.runtime.Ready() {
		if live, liveErr := a.liveDAGGlobalPayload(ctx, nil); liveErr == nil && live != nil {
			return live, nil
		} else if liveErr != nil {
			return nativeDAGGlobalWarmingPayload("native DAG live window is unavailable: " + liveErr.Error()), nil
		}
		return nativeDAGGlobalWarmingPayload("native DAG live window is waiting for producer attribution."), nil
	}
	return a.api.get(ctx, "/api/global/pool-earnings", time.Minute)
}

func (c *APIClient) get(ctx context.Context, path string, ttl time.Duration) (map[string]any, error) {
	key := path
	now := time.Now()
	c.mu.Lock()
	entry, ok := c.caches[key]
	if ok && now.Before(entry.expires) {
		payload, err := entry.payload, entry.err
		c.mu.Unlock()
		if payload != nil {
			return payload, nil
		}
		return nil, err
	}
	if ok && entry.payload != nil {
		c.startRefreshLocked(key, path, ttl)
		payload := entry.payload
		c.mu.Unlock()
		return payload, nil
	}
	if call := c.inflight[key]; call != nil {
		c.mu.Unlock()
		return waitForAPICall(ctx, call)
	}
	call := &apiCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	c.finishRefresh(ctx, key, path, ttl, call)
	return call.payload, call.err
}

func (c *APIClient) cachedOrRefresh(path string, ttl time.Duration) (map[string]any, error) {
	key := path
	now := time.Now()
	c.mu.Lock()
	entry, ok := c.caches[key]
	if ok && entry.payload != nil {
		if now.After(entry.expires) {
			c.startRefreshLocked(key, path, ttl)
		}
		payload := entry.payload
		c.mu.Unlock()
		return payload, nil
	}
	if ok && now.Before(entry.expires) && entry.err != nil {
		err := entry.err
		c.mu.Unlock()
		return nil, err
	}
	c.startRefreshLocked(key, path, ttl)
	c.mu.Unlock()
	return nil, nil
}

func (c *APIClient) startRefreshLocked(key string, path string, ttl time.Duration) *apiCall {
	if call := c.inflight[key]; call != nil {
		return call
	}
	call := &apiCall{done: make(chan struct{})}
	c.inflight[key] = call
	go c.finishRefresh(context.Background(), key, path, ttl, call)
	return call
}

func (c *APIClient) finishRefresh(ctx context.Context, key string, path string, ttl time.Duration, call *apiCall) {
	payload, err := c.fetch(ctx, path)
	c.mu.Lock()
	if err == nil || payload != nil {
		c.caches[key] = cacheEntry{payload: payload, expires: time.Now().Add(ttl), err: err}
	} else {
		entry := c.caches[key]
		entry.err = err
		entry.expires = time.Now().Add(refreshRetryDelay(ttl))
		c.caches[key] = entry
	}
	if current := c.inflight[key]; current == call {
		delete(c.inflight, key)
	}
	call.payload = payload
	call.err = err
	close(call.done)
	c.mu.Unlock()
}

func (c *APIClient) fetch(ctx context.Context, path string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		err = fmt.Errorf("canonical API %s returned %s", path, resp.Status)
		return payload, err
	}
	return payload, nil
}

func waitForAPICall(ctx context.Context, call *apiCall) (map[string]any, error) {
	select {
	case <-call.done:
		return call.payload, call.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func refreshRetryDelay(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return 5 * time.Second
	}
	delay := ttl / 4
	if delay < 2*time.Second {
		return 2 * time.Second
	}
	if delay > 10*time.Second {
		return 10 * time.Second
	}
	return delay
}

func minerRows(data map[string]any) []map[string]string {
	rows := asicFleetSourceRows(data)
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		ip := first(mapString(row, "ip"), mapString(row, "remote_host"), mapString(row, "client_addr"))
		mac := asicRowMAC(row)
		status, reason := asicFleetStatus(row)
		lastSeen := first(mapString(row, "last_share_at"), mapString(row, "last_seen_at"), humanUnixMilli(mapString(row, "last_event_at_epoch_ms"), mapString(row, "last_block_at_epoch_ms")), mapString(row, "updated_at"))
		telemetryStatus := first(mapString(row, "device_telemetry_status"), mapString(row, "telemetry_status"), "unknown")
		telemetryErrors := first(mapString(row, "device_telemetry_errors"), mapString(row, "telemetry_errors"), "none")
		out = append(out, map[string]string{
			"mac":              first(mac, "not known"),
			"identity":         first(mac, mapString(row, "identity"), mapString(row, "name"), "not known"),
			"ip":               first(ip, "not known"),
			"observed":         strings.Join(nonEmpty(ip, mapString(row, "worker"), mapString(row, "port")), " / "),
			"intent":           first(mapString(row, "configured_pool_url"), mapString(row, "pool_url"), mapString(row, "intended_wallet"), "not known"),
			"status":           status,
			"shares":           first(mapString(row, "shares"), mapString(row, "valid_shares"), mapString(row, "device_accepted"), mapString(row, "accepted"), "0"),
			"last":             humanTime(first(lastSeen, "not known")),
			"detail_id":        safeDOMID(first(mac, ip, "asic")),
			"diagnostic":       reason,
			"ready":            yesNo(truthyValue(row["ready"])),
			"job_age":          milliseconds(mapString(row, "current_job_age_ms")),
			"protocol":         first(mapString(row, "protocol_variant"), "stratum-v1-basic"),
			"blocks_30m":       blockCountDisplay(row, "blocks_30m"),
			"blocks_1h":        blockCountDisplay(row, "blocks_1h"),
			"blocks_24h":       blockCountDisplay(row, "blocks_24h"),
			"rejects_30m":      decimalInt(number(mapString(row, "rejected_30m")) + number(mapString(row, "rejected_local_30m"))),
			"active_pool":      first(mapString(row, "active_pool_url"), "unknown"),
			"pool_user":        first(mapString(row, "active_pool_user"), shortAddress(mapString(row, "intended_wallet")), "unknown"),
			"pool_legal":       first(mapString(row, "active_pool_legal"), "unknown"),
			"hashrate":         first(mapString(row, "hashrate"), mapString(row, "avg_hashrate"), "unknown"),
			"clock":            first(mapString(row, "clock_mhz"), "unknown"),
			"voltage":          first(mapString(row, "voltage_v"), "unknown"),
			"temperature":      first(mapString(row, "temperature"), "unknown"),
			"fan":              first(mapString(row, "fanspeed"), "unknown"),
			"telemetry":        telemetryStatus,
			"telemetry_age":    seconds(mapString(row, "device_telemetry_age_seconds")),
			"telemetry_errors": telemetryErrors,
			"source":           first(mapString(row, "source"), "unknown"),
			"configured_ip":    first(mapString(row, "configured_ip"), "n/a"),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return first(out[i]["ip"], out[i]["mac"]) < first(out[j]["ip"], out[j]["mac"]) })
	return out
}

func asicFleetSourceRows(data map[string]any) []map[string]any {
	byKey := map[string]map[string]any{}
	add := func(row map[string]any) {
		if row == nil {
			return
		}
		mac := asicRowMAC(row)
		ip := first(mapString(row, "ip"), mapString(row, "remote_host"), mapString(row, "client_addr"))
		if mac == "" && ip == "" {
			return
		}
		if _, ok := row["managed"]; ok && !truthyValue(row["managed"]) {
			return
		}
		deviceType := lower(mapString(row, "device_type"))
		if deviceType != "" && deviceType != "asic" && mac == "" {
			return
		}
		key := "ip:" + ip
		if mac != "" {
			key = "mac:" + mac
		}
		existing := byKey[key]
		if mac != "" && ip != "" {
			if byIP := byKey["ip:"+ip]; byIP != nil {
				if existing == nil {
					existing = byIP
				} else {
					mergeASICFleetRow(existing, byIP)
				}
				delete(byKey, "ip:"+ip)
			}
		} else if mac == "" && ip != "" {
			for candidateKey, candidate := range byKey {
				if !strings.HasPrefix(candidateKey, "mac:") {
					continue
				}
				candidateIP := first(mapString(candidate, "ip"), mapString(candidate, "remote_host"), mapString(candidate, "client_addr"))
				if candidateIP == ip {
					key = candidateKey
					existing = candidate
					break
				}
			}
		}
		if existing == nil {
			existing = map[string]any{}
			byKey[key] = existing
		}
		mergeASICFleetRow(existing, row)
	}
	for _, row := range minerSourceRows(data) {
		add(row)
	}
	for _, lane := range listMaps(nestedAny(data, "asic_performance", "lanes")) {
		add(asicPerformanceMinerRow(lane))
	}
	for _, row := range telemetryDeviceRows(data) {
		add(row)
	}
	rows := make([]map[string]any, 0, len(byKey))
	for _, row := range byKey {
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return first(mapString(rows[i], "ip"), asicRowMAC(rows[i])) < first(mapString(rows[j], "ip"), asicRowMAC(rows[j]))
	})
	return rows
}

func mergeASICFleetRow(dst, src map[string]any) {
	for key, value := range src {
		text := strings.TrimSpace(stringify(value))
		if text == "" || text == "<nil>" {
			continue
		}
		current := strings.TrimSpace(stringify(dst[key]))
		if current == "" || current == "<nil>" || current == "unknown" || current == "not known" || current == "0" {
			dst[key] = value
			continue
		}
		switch key {
		case "ready", "connected", "pool_pass_present", "extranonce_subscribed":
			dst[key] = truthyValue(dst[key]) || truthyValue(value)
		case "status", "health":
			if current == "configured" || current == "not_connected" || current == "unknown" {
				dst[key] = value
			}
		case "device_telemetry_errors", "telemetry_errors":
			if text != "none" {
				dst[key] = value
			}
		}
	}
}

func asicFleetStatus(row map[string]any) (string, string) {
	ready := truthyValue(row["ready"])
	telemetry := lower(first(mapString(row, "device_telemetry_status"), mapString(row, "telemetry_status")))
	errors := first(mapString(row, "device_telemetry_errors"), mapString(row, "telemetry_errors"))
	activePool := first(mapString(row, "active_pool_url"), "unknown")
	hashrate := first(mapString(row, "hashrate"), mapString(row, "avg_hashrate"))
	health := lower(first(mapString(row, "health"), mapString(row, "status")))
	if ready {
		if telemetry == "degraded" {
			return "mining, telemetry degraded", "This ASIC has a ready Stratum job, but its web/cgminer telemetry is degraded: " + first(errors, "unknown telemetry error")
		}
		return "mining", "This ASIC has an authorized Stratum lane and a current job."
	}
	if telemetry == "degraded" {
		return "api degraded", "Not mining: ASIC API/cgminer telemetry is degraded. " + first(errors, "No detailed telemetry error was reported.")
	}
	if activePool == "unknown" {
		return first(health, "not mining"), "Not mining: no ready Stratum lane is visible, and the active pool could not be confirmed from ASIC telemetry."
	}
	if strings.TrimSpace(hashrate) == "" || strings.EqualFold(hashrate, "unknown") || number(hashrate) <= 0 {
		return first(health, "not mining"), "Not mining: no ready Stratum lane is visible and miner-reported hashrate is unavailable or zero."
	}
	return first(health, "not mining"), "Not mining: this ASIC is configured/reachable, but no current ready Stratum job is visible for its MAC."
}

func safeDOMID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func asicPerformanceRows(data map[string]any) []map[string]string {
	return newMinerFleetSnapshot(data).PerformanceRows()
}

func blockCountDisplay(row map[string]any, key string) string {
	raw := strings.TrimSpace(mapString(row, key))
	if raw == "" {
		return "n/a"
	}
	return decimalInt(number(raw))
}

func asicDeviceRows(data map[string]any) []map[string]string {
	rows := asicFleetSourceRows(data)
	if len(rows) == 0 {
		rows = telemetryDeviceRows(data)
	}
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		mac := minerMAC(row)
		if mac == "not known" {
			mac = normalizedMAC(mapString(row, "mac"))
		}
		out = append(out, map[string]string{
			"mac":           first(mac, "not known"),
			"ip":            first(mapString(row, "ip"), "not known"),
			"model":         first(mapString(row, "device_model"), mapString(row, "model"), "unknown"),
			"firmware":      first(mapString(row, "firmware"), "unknown"),
			"clock":         first(mapString(row, "clock_mhz"), "unknown"),
			"voltage":       first(mapString(row, "voltage_v"), "unknown"),
			"chips":         first(mapString(row, "chip_count"), "unknown"),
			"temp":          first(mapString(row, "temperature"), "unknown"),
			"fan":           first(mapString(row, "fanspeed"), "unknown"),
			"hashrate":      first(mapString(row, "hashrate"), mapString(row, "avg_hashrate"), "unknown"),
			"powerplan":     first(mapString(row, "manual_powerplan"), "unknown"),
			"active_pool":   first(mapString(row, "active_pool_url"), mapString(row, "configured_pool_url"), "unknown"),
			"pool_user":     first(mapString(row, "active_pool_user"), shortAddress(mapString(row, "intended_wallet")), "unknown"),
			"pool_legal":    first(mapString(row, "active_pool_legal"), "unknown"),
			"pool_pass":     yesNo(truthyValue(row["pool_pass_present"])),
			"miner_status":  first(mapString(row, "device_miner_status"), mapString(row, "status"), "unknown"),
			"accepted":      first(mapString(row, "device_accepted"), "0"),
			"rejected":      first(mapString(row, "device_rejected"), "0"),
			"hw_errors":     first(mapString(row, "device_hw_errors"), "0"),
			"telemetry_age": seconds(mapString(row, "device_telemetry_age_seconds")),
			"telemetry":     first(mapString(row, "device_telemetry_status"), "unknown"),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return first(out[i]["ip"], out[i]["mac"]) < first(out[j]["ip"], out[j]["mac"])
	})
	return out
}

func telemetryDeviceRows(data map[string]any) []map[string]any {
	devices := listMaps(nestedAny(data, "asic_telemetry", "devices"))
	rows := make([]map[string]any, 0, len(devices))
	for _, device := range devices {
		summary := mapValue(device, "summary")
		row := map[string]any{
			"managed":                      true,
			"device_type":                  "asic",
			"mac":                          normalizedMAC(mapString(device, "mac")),
			"identity":                     normalizedMAC(mapString(device, "mac")),
			"ip":                           mapString(device, "ip"),
			"device_model":                 mapString(summary, "model"),
			"firmware":                     mapString(summary, "firmware"),
			"manual_powerplan":             mapString(summary, "manual_powerplan"),
			"clock_mhz":                    mapString(summary, "clock_mhz"),
			"voltage_v":                    mapString(summary, "voltage_v"),
			"chip_count":                   mapString(summary, "chip_count"),
			"temperature":                  mapString(summary, "temperature"),
			"fanspeed":                     mapString(summary, "fanspeed"),
			"hashrate":                     mapString(summary, "hashrate"),
			"avg_hashrate":                 mapString(summary, "avg_hashrate"),
			"active_pool_url":              mapString(summary, "active_pool_url"),
			"active_pool_user":             mapString(summary, "active_pool_user"),
			"active_pool_legal":            mapString(summary, "active_pool_legal"),
			"pool_pass_present":            truthyValue(summary["pool_pass_present"]),
			"device_miner_status":          mapString(summary, "miner_status"),
			"device_accepted":              mapString(summary, "accepted"),
			"device_rejected":              mapString(summary, "rejected"),
			"device_hw_errors":             mapString(summary, "hw_errors"),
			"device_telemetry_age_seconds": mapString(device, "age_seconds"),
			"device_telemetry_status":      mapString(device, "status"),
			"device_telemetry_errors":      telemetryErrorSummary(device),
		}
		rows = append(rows, row)
	}
	return rows
}

func asicPerformanceChartJSON(data map[string]any) string {
	lanes := listMaps(nestedAny(data, "asic_performance", "lanes"))
	if len(lanes) == 0 {
		return "[]"
	}
	palette := []string{"#2bd4a3", "#f5c542", "#ff7a7a", "#7ca7ff", "#d68fff", "#8bd3ff"}
	series := make([]map[string]any, 0, len(lanes))
	for i, lane := range lanes {
		points := make([]map[string]any, 0)
		for _, bucket := range listMaps(lane["buckets"]) {
			startMS := int64(number(mapString(bucket, "start_unix_ms")))
			if startMS <= 0 {
				continue
			}
			counts := mapValue(bucket, "counts")
			points = append(points, map[string]any{
				"t":              time.UnixMilli(startMS).UTC().Format(time.RFC3339),
				"accepted":       number(mapString(counts, "accepted")),
				"rejected":       number(mapString(counts, "rejected")),
				"rejected_local": number(mapString(counts, "rejected_local")),
			})
		}
		for _, point := range listMaps(lane["hashrate_points"]) {
			timestamp := first(mapString(point, "t"), mapString(point, "updated_at"))
			if timestamp == "" {
				timestamp = formatUnixMS(int64(number(mapString(point, "t_ms"))))
			}
			if strings.TrimSpace(timestamp) == "" {
				continue
			}
			points = append(points, map[string]any{
				"t":            timestamp,
				"hashrate":     number(mapString(point, "hashrate")),
				"avg_hashrate": number(mapString(point, "avg_hashrate")),
				"accepted":     number(mapString(point, "accepted")),
				"rejected":     number(mapString(point, "rejected")),
				"hw_errors":    number(mapString(point, "hw_errors")),
				"temperature":  number(mapString(point, "temperature")),
			})
		}
		sort.SliceStable(points, func(i, j int) bool {
			return mapString(points[i], "t") < mapString(points[j], "t")
		})
		label := first(normalizedMAC(mapString(lane, "asic_mac")), mapString(lane, "remote_host"), mapString(lane, "lane_id"), "unknown")
		series = append(series, map[string]any{
			"label":  label,
			"mac":    normalizedMAC(mapString(lane, "asic_mac")),
			"ready":  truthyValue(lane["ready"]),
			"color":  palette[i%len(palette)],
			"points": points,
		})
	}
	raw, err := json.Marshal(series)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func formatUnixMS(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func minerControlRows(data map[string]any) []map[string]string {
	return newMinerFleetSnapshot(data).ControlRows()
}

func parseMinerControlTarget(value string) (string, string) {
	ip, mac, ok := strings.Cut(strings.TrimSpace(value), "|")
	if !ok {
		return strings.TrimSpace(value), ""
	}
	return strings.TrimSpace(ip), normalizedMAC(mac)
}

func minerMAC(row map[string]any) string {
	if mac := asicRowMAC(row); mac != "" {
		return mac
	}
	return "not known"
}

func asicRowMAC(row map[string]any) string {
	if mac := normalizedMAC(mapString(row, "mac")); mac != "" {
		return mac
	}
	if mac := normalizedMAC(mapString(row, "asic_mac")); mac != "" {
		return mac
	}
	deviceID := strings.TrimSpace(mapString(row, "device_id"))
	if mac, ok := strings.CutPrefix(strings.ToLower(deviceID), "mac:"); ok && normalizedMAC(mac) != "" {
		return normalizedMAC(mac)
	}
	return ""
}

func normalizedMAC(value string) string {
	raw := strings.ToLower(strings.TrimSpace(value))
	if raw == "" || raw == "not known" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "-", ":")
	if strings.Count(raw, ":") == 5 {
		parts := strings.Split(raw, ":")
		for _, part := range parts {
			if len(part) != 2 {
				return ""
			}
			for _, r := range part {
				if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
					return ""
				}
			}
		}
		return strings.Join(parts, ":")
	}
	compact := strings.ReplaceAll(raw, ":", "")
	if len(compact) != 12 {
		return ""
	}
	for _, r := range compact {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return ""
		}
	}
	parts := make([]string, 0, 6)
	for i := 0; i < len(compact); i += 2 {
		parts = append(parts, compact[i:i+2])
	}
	return strings.Join(parts, ":")
}

func minerPoolURLFromStatus(data map[string]any) string {
	for _, row := range minerSourceRows(data) {
		if url := first(mapString(row, "expected_pool_url"), mapString(row, "configured_pool_url"), mapString(row, "pool_url")); strings.HasPrefix(url, "stratum+") {
			return url
		}
	}
	if configured := nestedString(data, "pool_health", "configured_pool_url"); configured != "" {
		return normalizeStratumURL(configured)
	}
	if endpoint := stringValue(data, "pool_endpoint"); endpoint != "" {
		return normalizeStratumURL(endpoint)
	}
	return minerDefaultPoolURL()
}

func poolEndpointFromStatus(data map[string]any) string {
	value := first(stringValue(data, "pool_endpoint"), minerPoolURLFromStatus(data), "127.0.0.1:3334")
	parsed, err := url.Parse(normalizeStratumURL(value))
	if err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return value
}

func minerDefaultPoolURL() string {
	if configured := env("BDAG_POOL_URL", ""); configured != "" {
		return configured
	}
	if host := env("BDAG_POOL_HOST", ""); host != "" {
		port := env("BDAG_POOL_PORT", "3334")
		return "stratum+tcp://" + host + ":" + port
	}
	return "stratum+tcp://<pool-lan-ip>:3334"
}

func normalizeStratumURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "://") {
		return value
	}
	return "stratum+tcp://" + value
}

func formFalse(values url.Values, key string) bool {
	for _, value := range values[key] {
		switch lower(value) {
		case "false", "0", "off":
			return true
		}
	}
	return false
}

func minerEarningRows(data map[string]any) []map[string]string {
	return newPoolEarningsSnapshot(data).MinerEarningRows()
}

func poolEarningRows(data map[string]any, producerColors map[string]string, localProducers map[string]bool) []map[string]string {
	return newGlobalProductionSnapshot(data).PoolEarningRows(producerColors, localProducers)
}

func hasProducerRows(data map[string]any) bool {
	return newGlobalProductionSnapshot(data).HasProducerRows()
}

func producerRows(data map[string]any) []map[string]any {
	return newGlobalProductionSnapshot(data).ProducerRows()
}

const producerFallbackColor = "#22d3ee"

func stableProducerColor(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return producerFallbackColor
	}
	hash := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		hash ^= uint32(key[i])
		hash *= 16777619
	}
	hue := float64(hash % 360)
	saturation := 0.68 + float64((hash>>9)%18)/100
	lightness := 0.52 + float64((hash>>18)%12)/100
	return hslHex(hue, saturation, lightness)
}

func hslHex(hue, saturation, lightness float64) string {
	chroma := (1 - math.Abs(2*lightness-1)) * saturation
	x := chroma * (1 - math.Abs(math.Mod(hue/60, 2)-1))
	m := lightness - chroma/2
	var red, green, blue float64
	switch {
	case hue < 60:
		red, green, blue = chroma, x, 0
	case hue < 120:
		red, green, blue = x, chroma, 0
	case hue < 180:
		red, green, blue = 0, chroma, x
	case hue < 240:
		red, green, blue = 0, x, chroma
	case hue < 300:
		red, green, blue = x, 0, chroma
	default:
		red, green, blue = chroma, 0, x
	}
	return fmt.Sprintf("#%02x%02x%02x", colorByte(red+m), colorByte(green+m), colorByte(blue+m))
}

func colorByte(value float64) int {
	value = math.Round(value * 255)
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return int(value)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func producerKey(row map[string]any) string {
	return first(mapString(row, "address"), mapString(row, "address_short"), mapString(row, "pool_label"), mapString(row, "label"), mapString(row, "pool_name"))
}

func globalChartJSON(data map[string]any) (string, map[string]string, map[string]bool) {
	return newGlobalProductionSnapshot(data).ChartJSON()
}

func compactGlobalChartJSON(data map[string]any, history []map[string]any) (string, map[string]string, map[string]bool) {
	return newGlobalProductionSnapshot(data).compactChartJSON(history)
}

func stringIn(needle string, haystack []string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

func producerLabel(row map[string]any, key string) string {
	label := first(mapString(row, "pool_label"), mapString(row, "label"))
	if strings.HasPrefix(lower(label), "unknown-mac") {
		label = ""
	}
	return first(label, mapString(row, "address_short"), shortAddress(mapString(row, "address")), key)
}

func lastPoint(points []chartPoint) float64 {
	if len(points) == 0 {
		return 0
	}
	return points[len(points)-1].Blocks
}

func firstNumber(m map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := parseNumber(mapString(m, key)); ok {
			return value
		}
	}
	return 0
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func displayAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	host, port, err := net.SplitHostPort(addr)
	if err == nil && port != "" {
		if host == "" || host == "0.0.0.0" || host == "::" {
			return ":" + port
		}
		return ":" + port
	}
	return ":" + addr
}

func stringValue(m map[string]any, key string) string { return stringify(m[key]) }
func mapString(m map[string]any, key string) string   { return stringify(m[key]) }

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if math.Trunc(x) == x {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(x)
	}
}

func boolValue(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func nestedAny(m map[string]any, keys ...string) any {
	var cur any = m
	for _, key := range keys {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = next[key]
	}
	return cur
}

func nestedString(m map[string]any, keys ...string) string { return stringify(nestedAny(m, keys...)) }
func nestedBool(m map[string]any, keys ...string) bool {
	v, _ := nestedAny(m, keys...).(bool)
	return v
}
func nestedNumber(m map[string]any, keys ...string) float64 { return number(nestedString(m, keys...)) }

func firstNestedAny(m map[string]any, paths ...[]string) any {
	for _, path := range paths {
		value := nestedAny(m, path...)
		if value != nil && stringify(value) != "" {
			return value
		}
	}
	return nil
}

func firstNestedString(m map[string]any, paths ...[]string) string {
	return stringify(firstNestedAny(m, paths...))
}

func firstNestedMap(m map[string]any, paths ...[]string) map[string]any {
	for _, path := range paths {
		if value, ok := nestedAny(m, path...).(map[string]any); ok && value != nil {
			return value
		}
	}
	return nil
}

func mapValue(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}

func listMaps(v any) []map[string]any {
	switch raw := v.(type) {
	case []map[string]any:
		out := make([]map[string]any, len(raw))
		copy(out, raw)
		return out
	case []any:
		out := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func numberMap(v any) map[string]float64 {
	out := map[string]float64{}
	switch raw := v.(type) {
	case map[string]any:
		for key, value := range raw {
			if parsed, ok := parseNumber(stringify(value)); ok {
				out[key] = parsed
			}
		}
	case map[string]float64:
		for key, value := range raw {
			out[key] = value
		}
	case map[string]int:
		for key, value := range raw {
			out[key] = float64(value)
		}
	case map[string]uint64:
		for key, value := range raw {
			out[key] = float64(value)
		}
	}
	return out
}

func stringMap(v any) map[string]string {
	out := map[string]string{}
	switch raw := v.(type) {
	case map[string]any:
		for key, value := range raw {
			out[strings.ToLower(strings.TrimSpace(key))] = stringify(value)
		}
	case map[string]string:
		for key, value := range raw {
			out[strings.ToLower(strings.TrimSpace(key))] = value
		}
	}
	return out
}

func boolMap(v any) map[string]bool {
	out := map[string]bool{}
	switch raw := v.(type) {
	case map[string]any:
		for key, value := range raw {
			switch typed := value.(type) {
			case bool:
				out[strings.ToLower(strings.TrimSpace(key))] = typed
			case string:
				out[strings.ToLower(strings.TrimSpace(key))] = lower(typed) == "true" || typed == "1"
			}
		}
	case map[string]bool:
		for key, value := range raw {
			out[strings.ToLower(strings.TrimSpace(key))] = value
		}
	}
	return out
}

func stringList(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s := stringify(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func stringSlice(value any) []string {
	switch raw := value.(type) {
	case []string:
		out := make([]string, len(raw))
		copy(out, raw)
		return out
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if text := stringify(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	out := make([]string, 0, len(dst)+len(values))
	for _, value := range append(dst, values...) {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, text)
	}
	return out
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func nonEmpty(values ...string) []string {
	out := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func number(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	if s == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	if s == "" || strings.EqualFold(s, "n/a") || strings.EqualFold(s, "not known") {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func displayStat(s string) string {
	if strings.TrimSpace(s) == "" {
		return "n/a"
	}
	return s
}

func integerStat(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "not known"
	}
	value, ok := parseNumber(s)
	if !ok {
		return s
	}
	return commaInt(int64(math.Round(value)))
}

func commaInt(value int64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	raw := strconv.FormatInt(value, 10)
	if len(raw) <= 3 {
		return sign + raw
	}
	prefix := len(raw) % 3
	if prefix == 0 {
		prefix = 3
	}
	var out strings.Builder
	out.WriteString(raw[:prefix])
	for i := prefix; i < len(raw); i += 3 {
		out.WriteByte(',')
		out.WriteString(raw[i : i+3])
	}
	return sign + out.String()
}

func bdagValue(s string) string {
	value := bdagInt(s)
	if value == "n/a" || strings.EqualFold(value, "not known") || strings.EqualFold(value, "unknown") {
		return value
	}
	return value + " BDAG"
}

func bdagInt(s string) string {
	value, ok := parseNumber(s)
	if !ok {
		return displayStat(s)
	}
	return strconv.FormatInt(int64(math.Round(value)), 10)
}

func currency(s, prefix string) string {
	v, ok := parseNumber(s)
	if !ok {
		return "n/a"
	}
	return prefix + " " + strconv.FormatFloat(v, 'f', 2, 64)
}

func priceQuote(s, prefix string) string {
	v, ok := parseNumber(s)
	if !ok {
		return "n/a"
	}
	return prefix + " " + strconv.FormatFloat(v, 'f', 6, 64)
}

func multiplyCurrency(amount, price, prefix string) string {
	amountValue, amountOK := parseNumber(amount)
	priceValue, priceOK := parseNumber(price)
	if !amountOK || !priceOK {
		return "n/a"
	}
	return currency(strconv.FormatFloat(amountValue*priceValue, 'f', -1, 64), prefix)
}

func decimalString(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func decimalInt(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "0"
	}
	return strconv.FormatInt(int64(math.Round(value)), 10)
}

func shortAddress(address string) string {
	address = strings.TrimSpace(address)
	if len(address) <= 12 {
		return address
	}
	return address[:6] + "..." + address[len(address)-4:]
}

func (a *App) runtimeLogs(ctx context.Context, name string, tail int) string {
	if tail <= 0 {
		tail = 160
	}
	source := lower(env("BDAG_DASHBOARD_LOG_SOURCE", "docker"))
	if source != "collector" {
		container := name
		if name == "node" {
			container = env("BDAG_NODE_LOG_CONTAINER", env("BDAG_NODE_CONTAINER", "node"))
		}
		if name == "pool" {
			container = env("BDAG_POOL_LOG_CONTAINER", env("BDAG_POOL_CONTAINER", "pool"))
		}
		if name == "dashboard" {
			container = env("BDAG_DASHBOARD_LOG_CONTAINER", env("BDAG_DASHBOARD_CONTAINER", "dashboard"))
		}
		content, err := dockerContainerLogs(ctx, env("BDAG_DASHBOARD_DOCKER_SOCKET", "/var/run/docker.sock"), container, tail)
		if err == nil {
			return cleanLogContent(content)
		}
		if source == "docker" {
			return "Could not read " + name + " Docker logs: " + err.Error()
		}
	}
	endpoint := "/api/logs/" + name
	return a.collectorLogs(ctx, endpoint, tail)
}

func dockerContainerLogs(ctx context.Context, socket string, container string, tail int) (string, error) {
	socket = strings.TrimSpace(socket)
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	if _, err := os.Stat(socket); err != nil {
		return "", fmt.Errorf("Docker socket %s is not available: %w", socket, err)
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	path := "/containers/" + url.PathEscape(container) + "/logs?stdout=1&stderr=1&timestamps=1&tail=" + strconv.Itoa(tail)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Docker API returned %s for container %s: %s", resp.Status, container, strings.TrimSpace(string(raw)))
	}
	return stripDockerLogFrames(raw), nil
}

func stripDockerLogFrames(raw []byte) string {
	var out bytes.Buffer
	parsed := false
	for i := 0; i < len(raw); {
		if i+8 > len(raw) {
			if parsed {
				out.Write(raw[i:])
				return out.String()
			}
			return string(raw)
		}
		stream := raw[i]
		size := int(raw[i+4])<<24 | int(raw[i+5])<<16 | int(raw[i+6])<<8 | int(raw[i+7])
		if stream < 1 || stream > 3 || raw[i+1] != 0 || raw[i+2] != 0 || raw[i+3] != 0 || size < 0 || i+8+size > len(raw) {
			if parsed {
				out.Write(raw[i:])
				return out.String()
			}
			return string(raw)
		}
		parsed = true
		out.Write(raw[i+8 : i+8+size])
		i += 8 + size
	}
	if parsed {
		return out.String()
	}
	return string(raw)
}

func cleanLogContent(content string) string {
	return strings.TrimRight(ansiEscapeRE.ReplaceAllString(content, ""), "\n")
}

func (a *App) collectorLogs(ctx context.Context, endpoint string, tail int) string {
	if tail <= 0 {
		tail = 160
	}
	data, err := a.api.get(ctx, endpoint+"?tail="+strconv.Itoa(tail), 5*time.Second)
	if err != nil {
		return "Could not read collector logs: " + err.Error()
	}
	content, ok := data["content"].(string)
	if !ok {
		return "Collector log response did not include content"
	}
	return cleanLogContent(content)
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
func configuredLocation() *time.Location {
	name := strings.TrimSpace(os.Getenv("DASHBOARD_TIMEZONE"))
	if name == "" {
		return time.Local
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		log.Printf("invalid DASHBOARD_TIMEZONE %q: %v; using local timezone %s", name, err, time.Local)
		return time.Local
	}
	return location
}

func humanTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "not known" || s == "unknown" {
		return s
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05.000Z",
		"2006/01/02 15:04:05",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			return parsed.In(displayLocation).Format("Jan 2, 2006 15:04:05 MST")
		}
	}
	return s
}

func humanUnixMilli(values ...string) string {
	for _, value := range values {
		ms := int64(number(value))
		if ms <= 0 {
			continue
		}
		return time.UnixMilli(ms).In(displayLocation).Format("Jan 2, 2006 15:04:05 MST")
	}
	return "not known"
}

func milliseconds(s string) string {
	value := number(s)
	if value <= 0 {
		return "0 ms"
	}
	if value >= 1000 {
		return decimalString(value/1000) + "s"
	}
	return decimalInt(value) + " ms"
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
func toneFromStatus(status string) string {
	switch status {
	case "ok", "mining", "synced":
		return "ok"
	case "degraded", "syncing", "warn":
		return "warn"
	case "":
		return "neutral"
	default:
		return "down"
	}
}
func seconds(s string) string {
	if s == "" {
		return "not known"
	}
	return s + "s"
}
func hours(s string) string {
	if s == "" {
		return "not known"
	}
	return s + "h"
}
func percent(s string) string {
	if s == "" {
		return "0%"
	}
	if strings.Contains(s, "%") {
		return s
	}
	return s + "%"
}
func topShare(data map[string]any) string {
	rows := producerRows(data)
	if len(rows) == 0 {
		return "not known"
	}
	return percent(mapString(rows[0], "share_percent"))
}
func titleStatus(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "_", " "))
	if s == "" {
		return ""
	}
	parts := strings.Fields(s)
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}
func toneClass(tone string) string {
	switch tone {
	case "ok":
		return "state-badge state-ok"
	case "warn":
		return "state-badge state-warn"
	case "down":
		return "state-badge state-down"
	default:
		return "state-badge state-neutral"
	}
}

func peerColumnTooltip(column string) string {
	switch column {
	case "fork_status":
		return "Native DAG Main-chain classification. same_main_chain means the peer tip is proven on this node's Main chain; divergent_main_chain means the peer tip is known locally but not on this node's Main chain; common_main_chain_prefix means only a shared prefix is proven; unknown means evidence is incomplete."
	case "same_native_fork":
		return "Nullable native fork verdict. true means same_main_chain is proven; false means divergent_main_chain is proven; blank means the node has not proven either case."
	case "fork_confidence":
		return "Evidence level behind fork_status. main_tip is strongest; sync_point means only a shared Main-chain prefix; order mismatch or unknown sync point values keep the verdict conservative."
	case "fork_reason":
		return "Plain-language explanation for the exact fork classification branch, such as matching Main tip, known non-Main tip, shared prefix, or missing local evidence."
	default:
		return ""
	}
}

func peerHashColumn(column string) bool {
	column = strings.ToLower(strings.TrimSpace(column))
	return strings.Contains(column, "hash") ||
		strings.Contains(column, "root") ||
		column == "genesis"
}
