package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func hashpowerDevice(mac, endpoint, rate, status, age string) map[string]any {
	return map[string]any{
		"mac": mac, "status": status, "age_seconds": age,
		"summary": map[string]any{"active_pool_url": endpoint, "hashrate": rate},
	}
}

func hashpowerSnapshot(devices ...map[string]any) map[string]any {
	rows := make([]any, 0, len(devices))
	for _, row := range devices {
		rows = append(rows, row)
	}
	return map[string]any{"asic_telemetry": map[string]any{"status": "ok", "age_seconds": "5", "devices": rows}}
}

func TestPoolHashpowerCountsFreshUniqueLocalDevices(t *testing.T) {
	local := "stratum+tcp://192.168.100.22:3334"
	data := hashpowerSnapshot(
		hashpowerDevice("02:00:00:00:00:01", local, "80.5", "ok", "2"),
		hashpowerDevice("02:00:00:00:00:02", local, "1.5 TH/s", "ok", "3"),
		hashpowerDevice("02:00:00:00:00:02", local, "9 TH/s", "ok", "3"),
		hashpowerDevice("02:00:00:00:00:03", local, "999", "stale", "5"),
		hashpowerDevice("02:00:00:00:00:04", "stratum+tcp://192.168.100.9:3334", "999", "ok", "5"),
		hashpowerDevice("02:00:00:00:00:05", local, "999", "ok", "121"),
	)
	if got := poolHashpower(data, local); got != "1580.50 GH/s observed (2 ASICs)" {
		t.Fatalf("pool hashpower = %q", got)
	}
}

func TestPoolHashpowerDoesNotTurnMissingOrUnsafeEvidenceIntoZero(t *testing.T) {
	local := "stratum+tcp://192.168.100.22:3334"
	for _, tc := range []struct {
		name string
		data map[string]any
		url  string
	}{
		{"missing telemetry", nil, local},
		{"unknown pool endpoint", hashpowerSnapshot(hashpowerDevice("02:00:00:00:00:01", local, "100", "ok", "1")), ""},
		{"missing age", hashpowerSnapshot(hashpowerDevice("02:00:00:00:00:01", local, "100", "ok", "")), local},
		{"invalid rate", hashpowerSnapshot(hashpowerDevice("02:00:00:00:00:01", local, "NaN", "ok", "1")), local},
		{"other pool", hashpowerSnapshot(hashpowerDevice("02:00:00:00:00:01", "stratum+tcp://192.168.100.9:3334", "100", "ok", "1")), local},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := poolHashpower(tc.data, tc.url); got != "unavailable" {
				t.Fatalf("pool hashpower = %q, want unavailable", got)
			}
		})
	}
}

func TestPoolHashpowerTotalRequiresCompleteFreshPoolEvidence(t *testing.T) {
	local := "stratum+tcp://192.168.100.22:3334"
	makeStatus := func() map[string]any {
		status := hashpowerSnapshot(
			hashpowerDevice("02:00:00:00:00:01", local, "80", "ok", "2"),
			hashpowerDevice("02:00:00:00:00:02", local, "20", "ok", "2"),
		)
		status["pool_job_state"] = map[string]any{
			"status": "ok", "source": "pool-health-job-state", "generated_at": time.Now().UTC().Format("2006-01-02T15:04:05-0700"),
			"running_pool_containers": 1, "reporting_pool_containers": 1,
			"active_connections": 2, "authorized_connections": 2,
			"clients": []any{
				map[string]any{"asic_mac": "02:00:00:00:00:01", "authorized": true},
				map[string]any{"asic_mac": "02:00:00:00:00:02", "authorized": true},
			},
		}
		return status
	}
	if got := poolHashpower(makeStatus(), local); got != "100.00 GH/s total (2 ASICs)" {
		t.Fatalf("complete pool total = %q", got)
	}
	direct := makeStatus()
	state := mapValue(direct, "pool_job_state")
	delete(state, "generated_at")
	delete(state, "running_pool_containers")
	delete(state, "reporting_pool_containers")
	state["generated_at_unix_ms"] = float64(time.Now().UnixMilli())
	state["router"] = map[string]any{"node": map[string]any{"healthy": true}}
	if got := poolHashpower(direct, local); got != "100.00 GH/s total (2 ASICs)" {
		t.Fatalf("direct pool total = %q", got)
	}
	state["generated_at_unix_ms"] = float64(time.Now().Add(-3 * time.Minute).UnixMilli())
	if got := poolHashpower(direct, local); got != "100.00 GH/s observed (2 ASICs)" {
		t.Fatalf("stale direct pool must stay observed = %q", got)
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"pool reporter missing", func(s map[string]any) { mapValue(s, "pool_job_state")["running_pool_containers"] = 2 }},
		{"job state stale", func(s map[string]any) {
			mapValue(s, "pool_job_state")["generated_at"] = time.Now().Add(-3 * time.Minute).UTC().Format(time.RFC3339Nano)
		}},
		{"missing client", func(s map[string]any) {
			mapValue(s, "pool_job_state")["active_connections"] = 3
			mapValue(s, "pool_job_state")["authorized_connections"] = 3
		}},
		{"unmeasured client", func(s map[string]any) {
			mapValue(s, "pool_job_state")["clients"].([]any)[1].(map[string]any)["asic_mac"] = "02:00:00:00:00:03"
		}},
		{"unauthorized client", func(s map[string]any) {
			mapValue(s, "pool_job_state")["clients"].([]any)[1].(map[string]any)["authorized"] = false
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := makeStatus()
			tc.change(status)
			if got := poolHashpower(status, local); got != "100.00 GH/s observed (2 ASICs)" {
				t.Fatalf("incomplete evidence should remain observed: %q", got)
			}
		})
	}
}

func TestGlobalTabAndLiveRefreshShowPoolHashpower(t *testing.T) {
	local := "stratum+tcp://192.168.100.22:3334"
	t.Setenv("BDAG_POOL_URL", local)
	status := hashpowerSnapshot(hashpowerDevice("02:00:00:00:00:01", local, "80.5", "ok", "2"))
	status["miner_health"] = map[string]any{"managed_miners": []any{map[string]any{"expected_pool_url": "stratum+tcp://192.168.100.9:3334"}}}
	client := newAPIClient("http://127.0.0.1", nil)
	for _, path := range []string{"/api/global", "/api/earnings", "/api/global/pool-earnings"} {
		client.caches[path] = cacheEntry{payload: map[string]any{"status": "ok"}, expires: time.Now().Add(time.Hour)}
	}
	app := &App{api: client, templates: parseTemplates(), statusDataProvider: func(context.Context) (map[string]any, error) {
		return status, nil
	}}
	view := app.globalWithRange(context.Background(), "1h", false)
	var html bytes.Buffer
	if err := app.templates.ExecuteTemplate(&html, "global", view); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "Pool Hashpower") || !strings.Contains(html.String(), "80.50 GH/s observed (1 ASIC)") {
		t.Fatal("Global tab did not render the observed pool hashpower")
	}
	rec := httptest.NewRecorder()
	app.handleLiveGlobal(rec, httptest.NewRequest("GET", "/api/live/global?chart=0", nil))
	var response struct {
		Evidence []Evidence `json:"evidence"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &response) != nil {
		t.Fatalf("live Global response status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, item := range response.Evidence {
		if item.Label == "Pool Hashpower" && item.Value == "80.50 GH/s observed (1 ASIC)" {
			return
		}
	}
	t.Fatalf("live Global refresh did not contain pool hashpower: %+v", response.Evidence)
}

func TestGlobalHashpowerSurvivesUnavailableGlobalProducerData(t *testing.T) {
	local := "stratum+tcp://192.168.100.22:3334"
	t.Setenv("BDAG_POOL_URL", local)
	status := hashpowerSnapshot(hashpowerDevice("02:00:00:00:00:01", local, "80.5", "ok", "2"))
	client := newAPIClient("http://127.0.0.1", nil)
	client.caches["/api/global"] = cacheEntry{err: errors.New("producer source unavailable"), expires: time.Now().Add(time.Hour)}
	app := &App{api: client, statusDataProvider: func(context.Context) (map[string]any, error) { return status, nil }}
	view := app.globalWithRange(context.Background(), "1h", false)
	for _, item := range view.Evidence {
		if item.Label == "Pool Hashpower" && item.Value == "80.50 GH/s observed (1 ASIC)" {
			return
		}
	}
	t.Fatal("producer source failure hid available pool hashpower")
}
