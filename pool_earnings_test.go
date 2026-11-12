package main

import (
	"context"
	"testing"
	"time"
)

func TestPoolEarningsSeparatesPaymentAndCreditWalletBalances(t *testing.T) {
	app := &App{
		api:       newAPIClient("http://collector.invalid", nil),
		templates: parseTemplates(),
	}
	app.api.caches["/api/earnings"] = cacheEntry{
		payload: map[string]any{
			"payment_wallet_balance": map[string]any{
				"ok_address_count": 0,
				"total_bdag":       "0.00",
			},
			"wallet_balance": map[string]any{
				"ok_address_count": 0,
				"total_bdag":       "0.00",
			},
			"credit_wallet_balance": map[string]any{
				"ok_address_count": 1,
				"total_bdag":       "1420072.66",
			},
			"credits": map[string]any{
				"recent_1h": map[string]any{
					"total_bdag": "20.98",
				},
				"recent_24h": map[string]any{
					"total_bdag": "1620.81",
				},
			},
			"earnings_24h": map[string]any{
				"bdag": "1620.81",
				"usd":  "0.07",
			},
		},
		expires: time.Now().Add(time.Minute),
	}

	view := app.poolEarnings(context.Background())

	if value := evidenceValue(view.Evidence, "Payment Wallet Balance"); value != "0 BDAG" {
		t.Fatalf("Payment Wallet Balance = %q, want 0 BDAG", value)
	}
	if value := evidenceValue(view.Evidence, "Credit Address Balance"); value != "1420073 BDAG" {
		t.Fatalf("Credit Address Balance = %q, want 1420073 BDAG", value)
	}
	if value := evidenceValue(view.Evidence, "My Wallet Balance"); value != "" {
		t.Fatalf("My Wallet Balance = %q, want absent", value)
	}
}

func TestPoolEarningsSnapshotProjectsMinerRowsAndLiveNotes(t *testing.T) {
	data := map[string]any{
		"source": "redis-live-estimate",
		"payment_wallet_balance": map[string]any{
			"total_bdag": "100",
		},
		"credits": map[string]any{
			"recent_1h": map[string]any{
				"total_bdag": "12",
			},
			"recent_24h": map[string]any{
				"total_bdag": "288",
			},
		},
		"price": map[string]any{
			"status": "ok",
			"usd":    "0.01",
		},
		"miner_estimates": []any{
			map[string]any{
				"name":                              "miner-b",
				"estimated_wallet_bdag_recent_hour": "2",
				"estimated_bdag_total":              "10",
				"last_share_at":                     "2026-06-26T10:00:00Z",
			},
			map[string]any{
				"name":                              "miner-a",
				"estimated_wallet_bdag_recent_hour": "5",
				"estimated_bdag_total":              "20",
				"last_share_at":                     "2026-06-26T10:01:00Z",
			},
		},
		"history_stale_reason": "credit history is warming",
	}
	view := TabView{}

	newPoolEarningsSnapshot(data).applyTo(&view)

	if view.State != "Earnings" || view.StateTone != "neutral" {
		t.Fatalf("state = %s/%s, want Earnings/neutral", view.State, view.StateTone)
	}
	if got := evidenceValue(view.Evidence, "Payment Wallet Balance"); got != "100 BDAG" {
		t.Fatalf("Payment Wallet Balance = %q, want 100 BDAG", got)
	}
	if len(view.Rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(view.Rows))
	}
	if got := view.Rows[0]["wallet"]; got != "miner-a" {
		t.Fatalf("first row wallet = %q, want miner-a", got)
	}
	if len(view.Notes) != 2 {
		t.Fatalf("notes = %#v, want live estimate note and stale reason", view.Notes)
	}
}
