package main

import (
	"bytes"
	"encoding/json"
	"html/template"
	"os"
	"regexp"
	"strings"
	"testing"

	"dashboard/minercontrol"
)

func TestMetricGridsAreNotNested(t *testing.T) {
	templates := parseTemplates()
	view := TabView{
		Title:        "Pool Status",
		RefreshEvery: "10s",
		Node: StatusPanel{
			Title:       "Node Status",
			Evidence:    sampleEvidence(),
			StatusSteps: []StatusStep{{Label: "Synced", Active: true}},
		},
		StatusSteps: []StatusStep{{Label: "Healthy and Mining", Active: true}},
		Secondary:   sampleEvidence(),
		Evidence:    sampleEvidence(),
		Groups: []EvidenceGroup{
			{Title: "Last Day", Items: sampleEvidence()},
		},
	}
	nestedMetricGrid := regexp.MustCompile(`<div class="metric-grid"[^>]*>\s*<div class="metric-grid"`)

	for _, name := range []string{"status", "pool-earnings", "global"} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := templates.ExecuteTemplate(&buf, name, view); err != nil {
				t.Fatalf("render template: %v", err)
			}
			if nestedMetricGrid.Match(buf.Bytes()) {
				t.Fatalf("rendered %s with a nested metric grid", name)
			}
		})
	}
}

func TestPoolStatusEvidenceRendersInsidePanel(t *testing.T) {
	templates := parseTemplates()
	view := TabView{
		Title:        "Pool Status",
		RefreshEvery: "10s",
		StatusSteps:  []StatusStep{{Label: "Healthy and Mining", Active: true}},
		Evidence:     []Evidence{{Label: "Accepting Shares", Value: "yes"}},
		Secondary:    []Evidence{{Label: "Pool Endpoint", Value: "127.0.0.1:3334"}},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "status-ladder", view); err != nil {
		t.Fatalf("render status ladder: %v", err)
	}
	html := buf.String()
	poolStatus := strings.Index(html, `<div class="eyebrow">Pool Status</div>`)
	if poolStatus < 0 {
		t.Fatalf("pool status heading was not rendered")
	}
	closeSection := strings.Index(html[poolStatus:], "</section>")
	if closeSection < 0 {
		t.Fatalf("pool status panel was not closed")
	}
	section := html[poolStatus : poolStatus+closeSection]
	for _, label := range []string{"Accepting Shares", "Pool Endpoint"} {
		if !strings.Contains(section, label) {
			t.Fatalf("%q was not rendered inside the pool status panel", label)
		}
	}
}

func TestStatusTemplateRendersASICPerformancePanel(t *testing.T) {
	templates := parseTemplates()
	view := TabView{
		Title:     "Pool Status",
		ChartJSON: template.JS(`[{"label":"2a:71:c7:f5:1f:1e","points":[{"t":"2026-06-21T10:00:00Z","accepted":2}]}]`),
		Rows: []map[string]string{
			{
				"mac":              "2a:71:c7:f5:1f:1e",
				"ip":               "192.168.1.101",
				"status":           "mining",
				"ready":            "yes",
				"hashrate":         "244.9",
				"shares":           "12",
				"job_age":          "1.50s",
				"last":             "Jun 21, 2026 12:00:00 SAST",
				"detail_id":        "2a-71-c7-f5-1f-1e",
				"target":           "192.168.1.101|2a:71:c7:f5:1f:1e",
				"diagnostic":       "This ASIC has an authorized Stratum lane and a current job.",
				"blocks_30m":       "4",
				"blocks_1h":        "8",
				"blocks_24h":       "96",
				"rejects_30m":      "1",
				"protocol":         "stratum-v1-extranonce",
				"active_pool":      "stratum+tcp://192.168.1.10:3334",
				"pool_user":        "0xabc",
				"pool_legal":       "true",
				"clock":            "850 MHz",
				"voltage":          "0.76 V",
				"temperature":      "58",
				"fan":              "3000",
				"telemetry":        "ok",
				"telemetry_age":    "2s",
				"telemetry_errors": "none",
				"source":           "test",
			},
		},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "asic-fleet", view); err != nil {
		t.Fatalf("render asic fleet: %v", err)
	}
	html := buf.String()
	for _, expected := range []string{"ASIC Performance", "asicPerformanceChart", "asic-performance-rows", "data-asic-detail-toggle", "Restart ASIC", "stratum-v1-extranonce"} {
		if !strings.Contains(html, expected) {
			t.Fatalf("status template missing %q:\n%s", expected, html)
		}
	}
}

func TestASICFleetRendersDeviceConfigurationSnapshot(t *testing.T) {
	templates := parseTemplates()
	view := TabView{
		ChartJSON: template.JS("[]"),
		Rows: []map[string]string{{
			"mac":              "2a:71:c7:f5:1f:1e",
			"ip":               "192.168.1.102",
			"status":           "api degraded",
			"ready":            "no",
			"hashrate":         "unknown",
			"shares":           "0",
			"job_age":          "0 ms",
			"last":             "Jun 21, 2026 12:00:00 SAST",
			"detail_id":        "2a-71-c7-f5-1f-1e",
			"target":           "192.168.1.102|2a:71:c7:f5:1f:1e",
			"diagnostic":       "Not mining: ASIC API/cgminer telemetry is degraded.",
			"blocks_30m":       "n/a",
			"blocks_1h":        "n/a",
			"blocks_24h":       "n/a",
			"rejects_30m":      "0",
			"protocol":         "stratum-v1-basic",
			"active_pool":      "unknown",
			"pool_user":        "unknown",
			"pool_legal":       "unknown",
			"clock":            "850 MHz",
			"voltage":          "0.76 V",
			"temperature":      "unknown",
			"fan":              "unknown",
			"telemetry":        "degraded",
			"telemetry_age":    "2s",
			"telemetry_errors": "cgminer_devs: connection reset by peer",
			"source":           "test",
		}},
		DeviceRows: []map[string]string{{
			"mac":           "2a:71:c7:f5:1f:1e",
			"ip":            "192.168.1.102",
			"model":         "X100",
			"firmware":      "1.2.3",
			"clock":         "850 MHz",
			"voltage":       "0.76 V",
			"chips":         "6",
			"temp":          "58",
			"fan":           "3000",
			"hashrate":      "1.00 TH/s",
			"powerplan":     "850 MHz 0.76 V",
			"active_pool":   "stratum+tcp://192.168.1.100:3334",
			"pool_user":     "0x1719...e7a0",
			"pool_legal":    "true",
			"miner_status":  "0",
			"accepted":      "12",
			"hw_errors":     "0",
			"telemetry":     "ok",
			"telemetry_age": "2s",
		}},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "asic-fleet", view); err != nil {
		t.Fatalf("render asic fleet: %v", err)
	}
	html := buf.String()
	for _, expected := range []string{"ASIC Performance", "Restart ASIC", "connection reset by peer", "ASIC Device Configuration Snapshot", "Clock", "Voltage", "850 MHz", "0.76 V", "stratum&#43;tcp://192.168.1.100:3334"} {
		if !strings.Contains(html, expected) {
			t.Fatalf("ASIC fleet missing %q: %s", expected, html)
		}
	}
	if strings.Contains(html, "secret") {
		t.Fatalf("ASIC fleet should not render secrets: %s", html)
	}
}

func TestASICPerformanceChartKeepsRecentLineVisibleWithoutDots(t *testing.T) {
	for _, expected := range []string{
		"const sixHourMS = 6 * 60 * 60 * 1000",
		"Miner hashrate (GH/s)",
		"hashrate: Number(p.hashrate) || 0",
		"const useHashrate = allPoints.some",
		"const activePlotted = plotted.filter((p) => p.value > 0)",
		"ctx.lineTo(Math.min(width - m.r, point.x + span), point.y)",
	} {
		if !strings.Contains(indexTemplate, expected) {
			t.Fatalf("ASIC performance chart script missing %q", expected)
		}
	}
	for _, forbidden := range []string{"ctx.arc(point.x", "ctx.ellipse(point.x"} {
		if strings.Contains(indexTemplate, forbidden) {
			t.Fatalf("ASIC performance chart should remain line-only, found %q", forbidden)
		}
	}
}

func TestLiveMetricCardsPreserveCompactEndpointClasses(t *testing.T) {
	for _, expected := range []string{
		`function metricCardClass(label)`,
		`if (text === "Pool Endpoint") cls += " metric-card-endpoint metric-card-nowrap"`,
		`.compact-metrics-grid`,
		`compact-metrics-grid .metric-card-endpoint`,
	} {
		if !strings.Contains(indexTemplate, expected) {
			t.Fatalf("index template missing compact metric renderer marker %q", expected)
		}
	}
}

func TestPoolEndpointFromStatusUsesASICFacingFallbacks(t *testing.T) {
	t.Setenv("BDAG_POOL_URL", "stratum+tcp://192.168.1.100:3334")

	if got := poolEndpointFromStatus(map[string]any{}); got != "192.168.1.100:3334" {
		t.Fatalf("poolEndpointFromStatus empty payload = %q, want ASIC-facing env endpoint", got)
	}
	if got := poolEndpointFromStatus(map[string]any{"pool_endpoint": "10.0.0.2:4444"}); got != "10.0.0.2:4444" {
		t.Fatalf("poolEndpointFromStatus explicit endpoint = %q", got)
	}
	if got := poolEndpointFromStatus(map[string]any{
		"miner_health": map[string]any{
			"managed_miners": []any{
				map[string]any{"expected_pool_url": "stratum+tcp://192.168.1.55:3334"},
			},
		},
	}); got != "192.168.1.55:3334" {
		t.Fatalf("poolEndpointFromStatus miner row endpoint = %q", got)
	}
}

func TestGlobalTabOrdersEarningsBeforeCompactMetrics(t *testing.T) {
	templates := parseTemplates()
	view := TabView{
		Title:     "Global",
		ChartJSON: template.JS("[]"),
		Evidence: []Evidence{
			{Label: "Current Price USD", Value: "n/a"},
			{Label: "Chain Block Count", Value: "11547571"},
			{Label: "Main Order", Value: "unknown"},
			{Label: "EVM Block", Value: "11547509"},
			{Label: "Latest Block", Value: "11547571"},
			{Label: "Data Age", Value: "1s"},
			{Label: "Stream Lag", Value: "62"},
			{Label: "Unique Producers", Value: "14"},
			{Label: "Avg Blocks/sec", Value: "1.042"},
			{Label: "Top Share", Value: "15.17"},
		},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "global", view); err != nil {
		t.Fatalf("render global tab: %v", err)
	}
	html := buf.String()
	earnings := strings.Index(html, `id="global-pool-earnings"`)
	metrics := strings.Index(html, `id="global-metrics" class="metric-grid global-metrics-grid"`)
	if earnings < 0 {
		t.Fatalf("global earnings section was not rendered")
	}
	if metrics < 0 {
		t.Fatalf("compact global metrics section was not rendered")
	}
	if earnings > metrics {
		t.Fatalf("global metrics rendered before earnings section")
	}
}

func TestStatusASICPerformanceStartsWithMACAddress(t *testing.T) {
	templates := parseTemplates()
	view := TabView{
		Title:        "Pool Status",
		RefreshEvery: "10s",
		Node: StatusPanel{
			Title:       "Node Status",
			StatusSteps: []StatusStep{{Label: "Synced", Active: true}},
		},
		StatusSteps: []StatusStep{{Label: "Mining", Active: true}},
		Rows: []map[string]string{
			{
				"mac":       "28:e2:97:4c:e4:0a",
				"ip":        "192.168.100.107",
				"status":    "connected",
				"shares":    "12",
				"last":      "now",
				"detail_id": "28-e2-97-4c-e4-0a",
			},
		},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "asic-fleet", view); err != nil {
		t.Fatalf("render asic fleet template: %v", err)
	}
	html := buf.String()
	header := "<th>MAC Address</th><th>IP</th>"
	if !strings.Contains(html, header) {
		t.Fatalf("ASIC performance table header does not start with MAC then IP: %s", html)
	}
	row := `<td style="font-weight: 600;">28:e2:97:4c:e4:0a</td><td>192.168.100.107</td>`
	if !strings.Contains(html, row) {
		t.Fatalf("ASIC performance row does not start with MAC address: %s", html)
	}

	view.Rows = nil
	buf.Reset()
	if err := templates.ExecuteTemplate(&buf, "asic-fleet", view); err != nil {
		t.Fatalf("render empty asic fleet template: %v", err)
	}
	if !strings.Contains(buf.String(), `colspan="9"`) {
		t.Fatalf("empty-state colspan was not updated to nine columns")
	}
}

func TestMinerControlTemplateRendersSeparateActionSurface(t *testing.T) {
	templates := parseTemplates()
	view := MinerControlView{
		Title:               "Miner Control",
		State:               "Armed",
		StateTone:           "warn",
		Enabled:             true,
		DefaultPoolURL:      "stratum+tcp://192.168.1.10:3334",
		DefaultWorkerUser:   "0xabcdef",
		ReplaceExisting:     true,
		Rows: []map[string]string{
			{
				"target": "192.168.1.100|28:e2:97:4c:e4:0a",
				"label":  "28:E2:97:4C:E4:0A",
				"ip":     "192.168.1.100",
				"status": "connected",
			},
		},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "miner-control", view); err != nil {
		t.Fatalf("render miner control: %v", err)
	}
	html := buf.String()
	for _, expected := range []string{`hx-post="/actions/miner-control/configure"`, `hx-post="/actions/miner-control/restart"`, "Control Token", "stratum&#43;tcp://192.168.1.10:3334"} {
		if !strings.Contains(html, expected) {
			t.Fatalf("miner control template missing %q: %s", expected, html)
		}
	}
	if strings.Contains(html, `hx-trigger="every`) {
		t.Fatalf("miner control tab should not auto-refresh while operators type secrets")
	}
}

func TestIndexUsesSSEPipelineWithoutBrowserPolling(t *testing.T) {
	templates := parseTemplates()
	view := map[string]any{
		"Title":           "Dashboard",
		"StackReleaseTag": "BDAG Community Release v3.0.0-rc.1",
		"Tab": TabView{
			Title:       "Pool Status",
			StatusSteps: []StatusStep{{Label: "Mining", Active: true}},
			Node: StatusPanel{
				Title:       "Node Status",
				StatusSteps: []StatusStep{{Label: "Synced", Active: true}},
			},
		},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "index", view); err != nil {
		t.Fatalf("render index: %v", err)
	}
	html := buf.String()
	for _, forbidden := range []string{`setInterval(`, `htmx.ajax`, `hx-trigger="every`, `hx-trigger="load, every`} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("index should use the SSE pipeline, found browser polling marker %q", forbidden)
		}
	}
	for _, expected := range []string{`new EventSource("/events")`, `/api/live/status`, `/api/live/peers`, `/api/live/stratum-runtime`, `/api/live/chain-dag`, `/api/live/global`, `/api/live/global/trend?range=`, `scheduleLiveUpdate("global")`, `scheduleLiveUpdate("stratumRuntime")`, `scheduleLiveUpdate("chainDAG")`, `scheduleLiveUpdate("peers")`, `if (parsed.length || !currentProducerTrendSeries().length)`} {
		if !strings.Contains(html, expected) {
			t.Fatalf("index missing live pipeline marker %q", expected)
		}
	}
	for _, expected := range []string{`poolStatusGroupsHTML`, `Accept: "text/plain"`} {
		if !strings.Contains(html, expected) {
			t.Fatalf("index missing live pool/log marker %q", expected)
		}
	}
	for _, expected := range []string{`applyChainDAGPatch`, `type === "chain-dag"`, `payload.patch`, `chainDAGLastFullUpdateAt`} {
		if !strings.Contains(html, expected) {
			t.Fatalf("index missing live Chain DAG patch marker %q", expected)
		}
	}
	for _, expected := range []string{`tab === "global"`, `type === "chain-dag"`, `type === "chain-live"`} {
		if !strings.Contains(html, expected) {
			t.Fatalf("index missing Global/Chain DAG shared stream marker %q", expected)
		}
	}
	for _, expected := range []string{`events.onerror = () =>`, `document.addEventListener("visibilitychange"`, `dashboardEvents.close()`, `window.addEventListener("pagehide"`, `scheduleActiveLiveCatchup`} {
		if !strings.Contains(html, expected) {
			t.Fatalf("index missing SSE catch-up marker %q", expected)
		}
	}
}

func TestSSEHandlerUsesResumableEventIDs(t *testing.T) {
	source, err := os.ReadFile("redis_runtime.go")
	if err != nil {
		t.Fatalf("read redis_runtime.go: %v", err)
	}
	text := string(source)
	for _, expected := range []string{`Last-Event-ID`, `retry: 3000`, `id: %s`, `message.ID`, `BDAG_DASHBOARD_SSE_MAX_CLIENTS`, `http.StatusTooManyRequests`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("SSE handler missing resumable event marker %q", expected)
		}
	}
}

func TestIndexUsesDashboardSidebarShell(t *testing.T) {
	templates := parseTemplates()
	view := map[string]any{
		"Title":           "Dashboard",
		"StackReleaseTag": "BDAG Community Release v3.0.0-rc.1",
		"Tab": TabView{
			Title:       "Pool Status",
			StatusSteps: []StatusStep{{Label: "Mining", Active: true}},
			Node: StatusPanel{
				Title:       "Node Status",
				StatusSteps: []StatusStep{{Label: "Synced", Active: true}},
			},
		},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "index", view); err != nil {
		t.Fatalf("render index: %v", err)
	}
	html := buf.String()
	for _, expected := range []string{
		`grid-template-columns: 272px minmax(0, 1fr);`,
		`class="sidebar"`,
		`class="content-shell"`,
		`Mining Operations Dashboard`,
		`BDAG Community Release v3.0.0-rc.1`,
		`hx-get="/tabs/overview"`,
		`hx-get="/tabs/warnings"`,
		`hx-get="/tabs/asic-fleet"`,
		`hx-get="/tabs/stratum-runtime"`,
		`hx-get="/tabs/pool-management"`,
		`hx-get="/tabs/chain-dag"`,
		`hx-get="/tabs/logs"`,
		`--nav-tab-height: 42px;`,
		`min-height: var(--nav-tab-height);`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("index sidebar shell missing %q", expected)
		}
	}
}

func TestStackReleaseTagUsesDashboardOverrideThenReleaseVersion(t *testing.T) {
	t.Setenv("BDAG_STACK_RELEASE_TAG", "pool-vdashboard")
	t.Setenv("BDAG_RELEASE_VERSION", "pool-vfallback")
	if got := stackReleaseTag(); got != "pool-vdashboard" {
		t.Fatalf("stackReleaseTag() = %q, want dashboard override", got)
	}

	t.Setenv("BDAG_STACK_RELEASE_TAG", "")
	if got := stackReleaseTag(); got != "pool-vfallback" {
		t.Fatalf("stackReleaseTag() fallback = %q, want release version", got)
	}
}

func TestLiveTabDOMContractPreservesRefreshAnchors(t *testing.T) {
	templates := parseTemplates()
	view := TabView{
		Title:     "Pool Status",
		ChartJSON: template.JS("[]"),
		Node: StatusPanel{
			Title:  "Node Status",
			Groups: []EvidenceGroup{{Title: "Native Sync", Items: sampleEvidence()}},
		},
		Groups: []EvidenceGroup{{Title: "Health", Items: sampleEvidence()}},
	}

	cases := map[string][]string{
		"overview": {
			`id="tab-content" data-live-tab="status"`,
			`data-live-steps="node"`,
			`data-live-steps="status"`,
			`data-live-evidence="node"`,
			`id="overview-warnings"`,
			`asic-performance-rows`,
		},
		"pool-earnings": {
			`id="tab-content" data-live-tab="pool-earnings"`,
			`id="pool-earnings-metrics"`,
			`id="pool-earnings-groups"`,
			`id="pool-earnings-list"`,
		},
		"global": {
			`id="tab-content" data-live-tab="global"`,
			`id="producerTrendRangeStatus"`,
			`id="producerTrend"`,
			`id="producerTrendLegend"`,
			`id="producerTrendData"`,
			`id="global-metrics"`,
			`id="global-pool-earnings-title"`,
			`id="global-pool-earnings-body"`,
			`id="global-pool-earnings-notes"`,
		},
		"stratum-runtime": {
			`id="tab-content" data-live-tab="stratum-runtime"`,
			`id="stratum-runtime-metrics" class="metric-grid pool-status-grid compact-metrics-grid"`,
			`id="stratum-client-rows"`,
		},
		"chain-dag": {
			`id="tab-content" data-live-tab="chain-dag"`,
			`class="chain-dag-heading-row"`,
			`id="chain-dag-heading-warning" class="chain-dag-heading-warning"`,
			`id="chain-dag-metrics" class="metric-grid chain-dag-metrics"`,
			`id="chainDAGData"`,
			`id="chainDAGCanvas"`,
			`id="chain-dag-rows"`,
		},
	}
	for name, expected := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := templates.ExecuteTemplate(&buf, name, view); err != nil {
				t.Fatalf("render %s: %v", name, err)
			}
			html := buf.String()
			for _, marker := range expected {
				if !strings.Contains(html, marker) {
					t.Fatalf("%s missing live DOM marker %q: %s", name, marker, html)
				}
			}
		})
	}
}

func TestChainDAGTemplateUsesReadableTopologyCanvas(t *testing.T) {
	templates := parseTemplates()
	view := TabView{Title: "Chain DAG", ChartJSON: template.JS(`{"nodes":[],"edges":[]}`)}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "chain-dag", view); err != nil {
		t.Fatalf("render chain-dag: %v", err)
	}
	html := buf.String()
	for _, expected := range []string{
		`class="chart-wrap chain-dag-wrap"`,
		`id="chainDAGCanvas"`,
		`id="chain-dag-heading-warning" class="chain-dag-heading-warning"`,
		`chain-dag-state`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("chain-dag template missing readable canvas marker %q: %s", expected, html)
		}
	}
	for _, forbidden := range []string{
		`Recent block topology from Redis`,
		`{{if .Warnings}}<ul class="warning-list" style="margin-top: 12px;">`,
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("chain-dag template still contains removed copy/old warning layout %q: %s", forbidden, html)
		}
	}
	for _, expected := range []string{
		`.chain-dag-wrap`,
		`height: clamp(392px, 45vh, 532px);`,
		`.chain-dag-heading-row`,
		`.chain-dag-heading-warning`,
		`deriveChainDAGTrunk`,
		`animateChainDAGPositions`,
		`drawOrderTicks`,
		`drawTrunkTrace`,
		`nodeSize`,
		`applyChainDAGPatch`,
		`chainDAGMetricCardsHTML`,
		`chainDAGEdgeKey`,
		`chainDAGProducerLaneIndexes`,
		`chainDAGLastPatchAt`,
		`const direction = child.x >= target.x ? 1 : -1`,
		`ctx.bezierCurveTo(start.x + direction * mid`,
		`parentEdgeByPair`,
		`globalCompositeOperation = "lighter"`,
		`branchLane`,
		`nodeProducer`,
		`producerColor`,
		`producer_pending`,
		`semanticFill`,
		`colorWithAlpha`,
		`chainDAGPulseRAF`,
		`chainDAGWarningsHTML`,
		`const height = Math.max(350, Math.floor(rect.height || 0));`,
		`layout_order`,
		`orderRankByValue`,
		`const progress = Number.isFinite(rank) ? rank / orderRankSpan`,
		`ctx.fillText("older", plotLeft`,
		`ctx.fillText("newest", width - plotRight`,
		`linear_path`,
	} {
		if !strings.Contains(indexTemplate, expected) {
			t.Fatalf("index template missing DAG renderer marker %q", expected)
		}
	}
	for _, forbidden := range []string{
		`drawOrderSpine`,
		`rgba(43, 212, 163, 0.88)`,
		`Recent block topology from Redis`,
	} {
		if strings.Contains(indexTemplate, forbidden) {
			t.Fatalf("index template still contains standalone trunk spine marker %q", forbidden)
		}
	}
}

func TestPoolManagementAndLogsRemainNonLiveTabs(t *testing.T) {
	templates := parseTemplates()
	control := MinerControlView{
		Title:               "Pool Management",
		State:               "Disabled",
		StateTone:           "neutral",
		DefaultPoolURL:      "stratum+tcp://192.168.1.10:3334",
		DefaultWorkerUser:   "0xabcdef",
		Rows: []map[string]string{{
			"target": "192.168.1.100|28:e2:97:4c:e4:0a",
			"label":  "28:E2:97:4C:E4:0A",
			"ip":     "192.168.1.100",
			"status": "connected",
		}},
	}

	var controlHTML bytes.Buffer
	if err := templates.ExecuteTemplate(&controlHTML, "pool-management", control); err != nil {
		t.Fatalf("render pool management: %v", err)
	}
	for _, forbidden := range []string{`data-live-tab`, `hx-trigger="every`} {
		if strings.Contains(controlHTML.String(), forbidden) {
			t.Fatalf("pool management should remain non-live, found %q: %s", forbidden, controlHTML.String())
		}
	}
	for _, expected := range []string{`hx-post="/actions/miner-control/configure"`, `hx-post="/actions/miner-control/restart"`, `disabled`} {
		if !strings.Contains(controlHTML.String(), expected) {
			t.Fatalf("pool management missing gated control marker %q: %s", expected, controlHTML.String())
		}
	}

	var logsHTML bytes.Buffer
	if err := templates.ExecuteTemplate(&logsHTML, "logs", LogsView{
		Node:      LogPanel{Title: "Node Logs", Endpoint: "/logs/node", ContentEndpoint: "/logs/node/content", Content: "node line"},
		Pool:      LogPanel{Title: "Pool Logs", Endpoint: "/logs/pool", ContentEndpoint: "/logs/pool/content"},
		Dashboard: LogPanel{Title: "Dashboard Logs", Endpoint: "/logs/dashboard", ContentEndpoint: "/logs/dashboard/content"},
	}); err != nil {
		t.Fatalf("render logs: %v", err)
	}
	for _, forbidden := range []string{`hx-trigger="every`} {
		if strings.Contains(logsHTML.String(), forbidden) {
			t.Fatalf("logs should avoid htmx polling, found %q: %s", forbidden, logsHTML.String())
		}
	}
	for _, expected := range []string{`data-live-tab="logs"`, `data-log-tab`, "Node Logs", "Pool Logs", "Dashboard Logs", `/logs/dashboard/content`, "node line"} {
		if !strings.Contains(logsHTML.String(), expected) {
			t.Fatalf("logs missing %q: %s", expected, logsHTML.String())
		}
	}
}

func TestProducerTrendRendererDrawsLinesAndSinglePointSegmentWithoutMarkers(t *testing.T) {
	templates := parseTemplates()
	view := TabView{
		Title:     "Global",
		ChartJSON: template.JS(`[{"label":"Pool A","points":[{"t":"2026-06-19T09:00:00Z","blocks":1},{"t":"2026-06-19T09:00:05Z","blocks":2}]}]`),
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "global", view); err != nil {
		t.Fatalf("render global tab: %v", err)
	}
	html := buf.String()
	lineDraw := strings.Index(html, `drawSmoothPath(ctx, plotted);`)
	markerDraw := strings.Index(html, `ctx.arc(p.x, p.y, 2.4`)
	twoPointLine := strings.Index(html, `ctx.lineTo(points[1].x, points[1].y);`)
	singlePointSegment := strings.Index(html, `if (plotted.length === 1)`)
	if lineDraw < 0 {
		t.Fatalf("producer trend renderer is missing line path drawing")
	}
	if markerDraw >= 0 {
		t.Fatalf("producer trend renderer should not draw point markers on trend lines")
	}
	for _, forbidden := range []string{`bestEffortMarkers`, `ctx.arc(point.x`, `ctx.fillStyle = "rgba(245, 196, 94, 0.16)"`} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("producer trend renderer must stay line-only, found %q", forbidden)
		}
	}
	if twoPointLine < 0 {
		t.Fatalf("producer trend renderer must connect two-point series with a line")
	}
	if singlePointSegment < 0 {
		t.Fatalf("producer trend renderer must draw a short segment for one-point live warmup series")
	}
}

func TestProducerTrendControlsShareOneOrderedToolbar(t *testing.T) {
	templates := parseTemplates()
	view := TabView{Title: "Global", ChartJSON: template.JS("[]")}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "global", view); err != nil {
		t.Fatalf("render global tab: %v", err)
	}
	html := buf.String()
	toolbarStart := strings.Index(html, `class="producer-trend-toolbar"`)
	statusStart := strings.Index(html, `id="producerTrendRangeStatus"`)
	if toolbarStart < 0 {
		t.Fatalf("producer trend toolbar was not rendered")
	}
	if statusStart < 0 || statusStart < toolbarStart {
		t.Fatalf("producer trend status was not rendered after toolbar")
	}
	toolbar := html[toolbarStart:statusStart]
	for _, expected := range []string{`Producer Trend`, `producer-trend-metrics`, `data-metric="blocks"`, `data-metric="bdag"`, `data-metric="usd"`, `producer-trend-ranges`, `data-range="1h"`} {
		if !strings.Contains(toolbar, expected) {
			t.Fatalf("producer trend toolbar missing %q", expected)
		}
	}
	heading := strings.Index(toolbar, `Producer Trend`)
	metrics := strings.Index(toolbar, `producer-trend-metrics`)
	blocks := strings.Index(toolbar, `data-metric="blocks"`)
	ranges := strings.Index(toolbar, `producer-trend-ranges`)
	oneHour := strings.Index(toolbar, `data-range="1h"`)
	if !(heading < metrics && metrics < blocks && blocks < ranges && ranges < oneHour) {
		t.Fatalf("producer trend toolbar controls are not ordered heading, metric selectors, timeframe selectors")
	}
}

func TestGlobalProducerTrendUsesPersistentRangeEndpoint(t *testing.T) {
	templates := parseTemplates()
	view := TabView{Title: "Global", ChartJSON: template.JS("[]")}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "global", view); err != nil {
		t.Fatalf("render global tab: %v", err)
	}
	html := buf.String()
	for _, expected := range []string{
		`data-range="1h"`,
		`data-range="4h"`,
		`data-range="12h"`,
		`data-range="24h"`,
		`data-range="3d"`,
		`data-range="7d"`,
		`data-range="1month"`,
		`setProducerTrendRange('1month')`,
	} {
		if !strings.Contains(html, expected) {
			t.Fatalf("global tab missing persistent trend marker %q", expected)
		}
	}
}

func TestGlobalChartJSONUsesTypedRedisHistory(t *testing.T) {
	data := map[string]any{
		"history": []map[string]any{
			{
				"updated_at":        "2026-06-19T09:00:00Z",
				"scan_window_hours": "1",
				"clusters": []any{
					map[string]any{"address": "0xabc", "address_short": "0xabc", "blocks": float64(5), "share_percent": "50.00"},
				},
			},
			{
				"updated_at":        "2026-06-19T09:01:00Z",
				"scan_window_hours": "1",
				"clusters": []any{
					map[string]any{"address": "0xabc", "address_short": "0xabc", "blocks": float64(7), "share_percent": "70.00"},
				},
			},
		},
		"updated_at":        "2026-06-19T09:02:00Z",
		"scan_window_hours": "1",
		"requested_blocks":  "600",
		"fetched_blocks":    "600",
		"status":            "ok",
		"clusters": []any{
			map[string]any{"address": "0xabc", "address_short": "0xabc", "blocks": float64(9), "share_percent": "90.00"},
		},
	}

	raw, _, _ := globalChartJSON(data)
	var series []struct {
		Label  string       `json:"label"`
		Points []chartPoint `json:"points"`
	}
	if err := json.Unmarshal([]byte(raw), &series); err != nil {
		t.Fatalf("chart json did not unmarshal: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("expected one producer series, got %d: %s", len(series), raw)
	}
	if got := len(series[0].Points); got != 3 {
		t.Fatalf("expected Redis history plus current snapshot to produce 3 points, got %d: %s", got, raw)
	}
}

func TestGlobalChartJSONUsesCompactRedisHistory(t *testing.T) {
	data := map[string]any{
		"chart_history": []map[string]any{
			{
				"t":                 "2026-06-19T09:00:00Z",
				"scan_window_hours": "1",
				"counts": map[string]any{
					"0xabc0000000000000000000000000000000000001": float64(5),
				},
				"labels": map[string]any{
					"0xabc0000000000000000000000000000000000001": "Pool A",
				},
			},
			{
				"t":                 "2026-06-19T09:01:00Z",
				"scan_window_hours": "1",
				"counts": map[string]any{
					"0xabc0000000000000000000000000000000000001": float64(7),
				},
				"labels": map[string]any{
					"0xabc0000000000000000000000000000000000001": "Pool A",
				},
				"local_pools": map[string]any{
					"0xabc0000000000000000000000000000000000001": true,
				},
			},
		},
		"clusters": []any{
			map[string]any{"address": "0xabc0000000000000000000000000000000000001", "address_short": "0xabc", "blocks": float64(7), "local_pool": true},
		},
	}

	raw, _, local := globalChartJSON(data)
	var series []struct {
		Label  string       `json:"label"`
		Local  bool         `json:"local"`
		Points []chartPoint `json:"points"`
	}
	if err := json.Unmarshal([]byte(raw), &series); err != nil {
		t.Fatalf("chart json did not unmarshal: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("expected one compact producer series, got %d: %s", len(series), raw)
	}
	if got := len(series[0].Points); got != 2 {
		t.Fatalf("expected two compact chart points, got %d: %s", got, raw)
	}
	if !series[0].Local || !local["0xabc0000000000000000000000000000000000001"] {
		t.Fatalf("expected local producer flag from compact history: %#v %#v", series[0], local)
	}
}

func TestGlobalChartJSONAddsCurrentSnapshotToCompactHistory(t *testing.T) {
	data := map[string]any{
		"chart_history": []map[string]any{
			{
				"t":                 "2026-06-19T09:00:00Z",
				"scan_window_hours": "1",
				"counts": map[string]any{
					"0xabc0000000000000000000000000000000000001": float64(5),
				},
				"labels": map[string]any{
					"0xabc0000000000000000000000000000000000001": "Pool A",
				},
			},
		},
		"updated_at":        "2026-06-19T09:05:00Z",
		"scan_window_hours": "1",
		"requested_blocks":  "600",
		"fetched_blocks":    "600",
		"status":            "ok",
		"clusters": []any{
			map[string]any{"address": "0xabc0000000000000000000000000000000000001", "address_short": "0xabc", "blocks": float64(9)},
		},
	}

	raw, _, _ := globalChartJSON(data)
	var series []struct {
		Points []chartPoint `json:"points"`
	}
	if err := json.Unmarshal([]byte(raw), &series); err != nil {
		t.Fatalf("chart json did not unmarshal: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("expected one compact producer series, got %d: %s", len(series), raw)
	}
	if got := len(series[0].Points); got != 2 {
		t.Fatalf("expected compact history plus current snapshot to produce 2 points, got %d: %s", got, raw)
	}
	if got := series[0].Points[1].T; got != "2026-06-19T09:05:00Z" {
		t.Fatalf("current point timestamp = %q, want latest snapshot timestamp", got)
	}
}

func TestGlobalChartJSONDoesNotDuplicateCurrentSnapshotAlreadyInHistory(t *testing.T) {
	data := map[string]any{
		"chart_history": []map[string]any{
			{
				"t":                 "2026-06-19T09:00:00Z",
				"scan_window_hours": "1",
				"counts": map[string]any{
					"0xabc0000000000000000000000000000000000001": float64(5),
				},
				"labels": map[string]any{
					"0xabc0000000000000000000000000000000000001": "Pool A",
				},
			},
			{
				"t":                 "2026-06-19T09:05:00Z",
				"scan_window_hours": "1",
				"counts": map[string]any{
					"0xabc0000000000000000000000000000000000001": float64(9),
				},
				"labels": map[string]any{
					"0xabc0000000000000000000000000000000000001": "Pool A",
				},
			},
		},
		"updated_at":        "2026-06-19T09:05:00Z",
		"scan_window_hours": "1",
		"requested_blocks":  "600",
		"fetched_blocks":    "600",
		"status":            "ok",
		"clusters": []any{
			map[string]any{"address": "0xabc0000000000000000000000000000000000001", "address_short": "0xabc", "blocks": float64(9)},
		},
	}

	raw, _, _ := globalChartJSON(data)
	var series []struct {
		Points []chartPoint `json:"points"`
	}
	if err := json.Unmarshal([]byte(raw), &series); err != nil {
		t.Fatalf("chart json did not unmarshal: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("expected one compact producer series, got %d: %s", len(series), raw)
	}
	if got := len(series[0].Points); got != 2 {
		t.Fatalf("expected existing history current point not to be duplicated, got %d: %s", got, raw)
	}
}

func TestRedisFunctionsDoNotRegisterLegacyGlobalScanner(t *testing.T) {
	for _, removed := range []string{
		`redis.register_function("bdag_apply_chain_head"`,
		`redis.register_function("bdag_replace_global_window"`,
		`redis.register_function("bdag_apply_global_degraded"`,
		`bdag:global:candidate`,
		`bdag:global:window`,
		`bdag:global:block:`,
	} {
		if strings.Contains(bdagRedisFunctionLibrary, removed) {
			t.Fatalf("Redis function library must not contain legacy global scanner path %q", removed)
		}
	}
	for _, expected := range []string{
		`redis.register_function("bdag_apply_pool_share"`,
		`redis.register_function("bdag_apply_host_metric"`,
	} {
		if !strings.Contains(bdagRedisFunctionLibrary, expected) {
			t.Fatalf("Redis function library missing retained runtime function %q", expected)
		}
	}
}

func TestControlActionResultRendersEvidence(t *testing.T) {
	templates := parseTemplates()
	result := minercontrol.Result{
		Tone:    "ok",
		Title:   "Pool configuration applied",
		Message: "done",
		Items:   []minercontrol.Evidence{{Label: "IP", Value: "192.168.1.100"}},
	}

	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, "control-action-result", result); err != nil {
		t.Fatalf("render control action result: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "Pool configuration applied") || !strings.Contains(html, "192.168.1.100") {
		t.Fatalf("control action result did not render evidence: %s", html)
	}
}

func sampleEvidence() []Evidence {
	return []Evidence{
		{Label: "First Metric", Value: "ready"},
		{Label: "Second Metric", Value: "42"},
	}
}
