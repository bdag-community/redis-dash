package minercontrol

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestEncryptPasswordMatchesOpenSSLCompatibleVector(t *testing.T) {
	got, err := encryptPassword("123456789")
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	want := "bbad7537f4c8b6ea31eea0b3d760e257"
	if got != want {
		t.Fatalf("ciphertext = %s, want %s", got, want)
	}
	if _, err := hex.DecodeString(got); err != nil {
		t.Fatalf("ciphertext is not hex: %v", err)
	}
}

func TestValidateMinerIPRejectsUnsafeTargets(t *testing.T) {
	for _, ip := range []string{"8.8.8.8", "127.0.0.1", "0.0.0.0", "not-ip", "172.16.0.5"} {
		if err := validateMinerIP(ip, "", false); err == nil {
			t.Fatalf("validateMinerIP(%q) succeeded, want rejection", ip)
		}
	}
	for _, ip := range []string{"192.168.1.100", "10.0.0.20"} {
		if err := validateMinerIP(ip, "", false); err != nil {
			t.Fatalf("validateMinerIP(%q) rejected private LAN IP: %v", ip, err)
		}
	}
}

func TestApplyPoolConfigUsesAuthenticatedMinerAdapter(t *testing.T) {
	pools := []map[string]any{
		{"url": "stratum+tcp://old-pool:3334", "user": "old-worker", "pass": "old", "pool-priority": float64(1), "active": false},
	}
	var sawLogin, sawNewPool, sawReplace bool
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/mcb/pools":
			return jsonResponse(http.StatusOK, pools), nil
		case req.Method == http.MethodGet && req.URL.Path == "/user/login":
			sawLogin = true
			if req.URL.Query().Get("username") != "admin" || req.URL.Query().Get("cipher") != "true" {
				t.Fatalf("unexpected login query: %s", req.URL.RawQuery)
			}
			return jsonResponse(http.StatusOK, map[string]any{"JWT Token": "token"}), nil
		case req.Method == http.MethodPut && req.URL.Path == "/mcb/newpool":
			sawNewPool = true
			requireBearer(t, req)
			var payload map[string]any
			decodeJSON(t, req.Body, &payload)
			pools = append(pools, map[string]any{
				"url":           payload["url"],
				"user":          payload["user"],
				"pass":          payload["pass"],
				"pool-priority": float64(0),
				"active":        true,
			})
			return jsonResponse(http.StatusOK, map[string]any{"ok": true}), nil
		case req.Method == http.MethodPut && req.URL.Path == "/mcb/delpool":
			requireBearer(t, req)
			if len(pools) > 1 {
				pools = pools[1:]
			}
			return jsonResponse(http.StatusOK, map[string]any{"ok": true}), nil
		case req.Method == http.MethodPut && req.URL.Path == "/mcb/pools":
			sawReplace = true
			requireBearer(t, req)
			var payload []map[string]any
			decodeJSON(t, req.Body, &payload)
			pools = payload
			pools[0]["active"] = true
			return jsonResponse(http.StatusOK, map[string]any{"ok": true}), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		return nil, nil
	})}
	controller := NewASICController(ASICConfig{HTTPClient: client, Sleep: func(time.Duration) {}})
	result, err := controller.ApplyPoolConfig(context.Background(), Target{IP: "192.168.1.100", MAC: "28:e2:97:4c:e4:0a"}, PoolConfig{
		AdminPassword:   "admin",
		PoolURL:         "stratum+tcp://pool:3334",
		WorkerUser:      "0xabcdef1234567890",
		PoolPassword:    "1234",
		ReplaceExisting: true,
	})
	if err != nil {
		t.Fatalf("apply pool config: %v", err)
	}
	if !sawLogin || !sawNewPool || !sawReplace {
		t.Fatalf("adapter did not perform expected calls: login=%v newpool=%v replace=%v", sawLogin, sawNewPool, sawReplace)
	}
	if result.Tone != "ok" {
		t.Fatalf("result tone = %q, want ok", result.Tone)
	}
	if len(pools) != 1 || mapString(pools[0], "url") != "stratum+tcp://pool:3334" {
		t.Fatalf("final pools = %#v", pools)
	}
}

func jsonResponse(status int, body any) *http.Response {
	raw, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(raw)),
	}
}

func decodeJSON(t *testing.T, body io.Reader, out any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(out); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
}

func requireBearer(t *testing.T, req *http.Request) {
	t.Helper()
	if got := req.Header.Get("authorization"); !strings.HasPrefix(got, "Bearer ") {
		t.Fatalf("authorization = %q, want bearer token", got)
	}
}
