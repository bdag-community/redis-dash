package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"dashboard/minercontrol"
)

type fakeMinerController struct {
	target minercontrol.Target
	config minercontrol.PoolConfig
}

func (f *fakeMinerController) ApplyPoolConfig(_ context.Context, target minercontrol.Target, config minercontrol.PoolConfig) (minercontrol.Result, error) {
	f.target = target
	f.config = config
	return minercontrol.Result{Tone: "ok", Title: "configured", Message: "done"}, nil
}

func (f *fakeMinerController) Restart(_ context.Context, target minercontrol.Target, _ minercontrol.RestartRequest) (minercontrol.Result, error) {
	f.target = target
	return minercontrol.Result{Tone: "ok", Title: "restarted", Message: "done"}, nil
}

func TestMinerControlViewDisabledByDefault(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"pool_endpoint": "192.168.1.10:3334",
			"miner_health": map[string]any{
				"miners": []any{map[string]any{"ip": "192.168.1.100", "mac": "28:e2:97:4c:e4:0a", "status": "connected"}},
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.minerControlView(context.Background())
	if view.Enabled {
		t.Fatalf("miner control should be disabled by default")
	}
	if len(view.Rows) != 1 {
		t.Fatalf("control rows len = %d, want 1", len(view.Rows))
	}
	if view.DefaultPoolURL != "stratum+tcp://192.168.1.10:3334" {
		t.Fatalf("default pool URL = %q", view.DefaultPoolURL)
	}
}

func TestMinerControlViewUsesRedisStatusProviderWhenCollectorUnavailable(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
		statusDataProvider: func(context.Context) (map[string]any, error) {
			return map[string]any{
				"mining_address": "0xD5F8DF0A60bC1Ff4636250b2a2dff319Bf79B1f5",
				"miner_health": map[string]any{
					"miners": []any{
						map[string]any{
							"ip":                  "192.168.100.80",
							"mac":                 "28:e2:97:2e:00:1b",
							"status":              "connected",
							"configured_pool_url": "stratum+tcp://192.168.100.114:3334",
						},
					},
				},
			}, nil
		},
	}

	view := app.minerControlView(context.Background())
	if view.APIError != "" {
		t.Fatalf("APIError = %q, want none", view.APIError)
	}
	if len(view.Rows) != 1 {
		t.Fatalf("control rows len = %d, want 1", len(view.Rows))
	}
	if view.Rows[0]["target"] != "192.168.100.80|28:e2:97:2e:00:1b" {
		t.Fatalf("target = %q", view.Rows[0]["target"])
	}
	if view.DefaultPoolURL != "stratum+tcp://192.168.100.114:3334" {
		t.Fatalf("default pool URL = %q", view.DefaultPoolURL)
	}
}

func TestHandleMinerConfigureRequiresTokenAndKnownMiner(t *testing.T) {
	fake := &fakeMinerController{}
	app := &App{
		api:                 newAPIClient("http://collector.invalid", nil),
		minerController:     fake,
		minerControlEnabled: true,
		minerControlToken:   "secret",
		templates:           parseTemplates(),
	}
	app.api.caches["/api/status"] = cacheEntry{
		payload: map[string]any{
			"miner_health": map[string]any{
				"miners": []any{map[string]any{"ip": "192.168.1.100", "mac": "28:e2:97:4c:e4:0a", "status": "connected"}},
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	form := url.Values{
		"target":         {"192.168.1.100|28:e2:97:4c:e4:0a"},
		"control_token":  {"secret"},
		"admin_password": {"admin"},
		"pool_url":       {"stratum+tcp://pool:3334"},
		"worker_user":    {"0xabcdef"},
		"pool_password":  {"1234"},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/actions/miner-control/configure", strings.NewReader(form.Encode()))
	request.Header.Set("content-type", "application/x-www-form-urlencoded")

	app.handleMinerConfigure(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if fake.target.IP != "192.168.1.100" || fake.target.MAC != "28:e2:97:4c:e4:0a" {
		t.Fatalf("target = %#v", fake.target)
	}
	if fake.config.PoolURL != "stratum+tcp://pool:3334" || fake.config.WorkerUser != "0xabcdef" {
		t.Fatalf("config = %#v", fake.config)
	}
	if !strings.Contains(recorder.Body.String(), "configured") {
		t.Fatalf("response did not render action result: %s", recorder.Body.String())
	}
}

func TestHandleMinerConfigureUsesRedisStatusProviderForKnownTarget(t *testing.T) {
	fake := &fakeMinerController{}
	app := &App{
		api:                 newAPIClient("http://collector.invalid", nil),
		minerController:     fake,
		minerControlEnabled: true,
		minerControlToken:   "secret",
		templates:           parseTemplates(),
		statusDataProvider: func(context.Context) (map[string]any, error) {
			return map[string]any{
				"miner_health": map[string]any{
					"miners": []any{
						map[string]any{"ip": "192.168.100.80", "mac": "28:e2:97:2e:00:1b", "status": "connected"},
					},
				},
			}, nil
		},
	}
	form := url.Values{
		"target":         {"192.168.100.80|28:e2:97:2e:00:1b"},
		"control_token":  {"secret"},
		"admin_password": {"admin"},
		"pool_url":       {"stratum+tcp://192.168.100.114:3334"},
		"worker_user":    {"0xD5F8DF0A60bC1Ff4636250b2a2dff319Bf79B1f5"},
		"pool_password":  {"x"},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/actions/miner-control/configure", strings.NewReader(form.Encode()))
	request.Header.Set("content-type", "application/x-www-form-urlencoded")

	app.handleMinerConfigure(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if fake.target.IP != "192.168.100.80" || fake.target.MAC != "28:e2:97:2e:00:1b" {
		t.Fatalf("target = %#v", fake.target)
	}
	if !strings.Contains(recorder.Body.String(), "configured") {
		t.Fatalf("response did not render action result: %s", recorder.Body.String())
	}
}

func TestHandleMinerConfigureRejectsInvalidToken(t *testing.T) {
	app := &App{
		api:                 newAPIClient("http://collector.invalid", nil),
		minerController:     &fakeMinerController{},
		minerControlEnabled: true,
		minerControlToken:   "secret",
		templates:           parseTemplates(),
	}
	form := url.Values{
		"target":         {"192.168.1.100|28:e2:97:4c:e4:0a"},
		"control_token":  {"wrong"},
		"admin_password": {"admin"},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/actions/miner-control/configure", strings.NewReader(form.Encode()))
	request.Header.Set("content-type", "application/x-www-form-urlencoded")

	app.handleMinerConfigure(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "invalid miner control token") {
		t.Fatalf("response did not include token rejection: %s", recorder.Body.String())
	}
}
