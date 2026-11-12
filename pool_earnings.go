package main

import "sort"

type PoolEarningsSnapshot struct {
	data map[string]any
}

func newPoolEarningsSnapshot(data map[string]any) PoolEarningsSnapshot {
	return PoolEarningsSnapshot{data: data}
}

func (s PoolEarningsSnapshot) applyTo(view *TabView) {
	paymentWallet := mapValue(s.data, "payment_wallet_balance")
	creditWallet := mapValue(s.data, "credit_wallet_balance")
	walletBalanceBdag := first(mapString(paymentWallet, "total_bdag"), nestedString(s.data, "wallet_balance", "total_bdag"), stringValue(s.data, "wallet_bdag"))
	if walletBalanceBdag == "" {
		walletBalanceBdag = "not known"
	}
	creditWalletBdag := mapString(creditWallet, "total_bdag")
	poolCreditsBdag := first(nestedString(s.data, "credits", "recent_24h", "total_bdag"), nestedString(s.data, "earnings_24h", "bdag"), walletBalanceBdag)
	hourly := mapValue(s.data, "hourly_averages")
	price := mapValue(s.data, "price")
	priceOK := mapString(price, "status") == "ok" && mapString(price, "usd") != ""
	usdPrice := ""
	if priceOK {
		usdPrice = mapString(price, "usd")
	}
	lastHourBdag := nestedString(s.data, "credits", "recent_1h", "total_bdag")
	wallet24hBdag := first(nestedString(s.data, "earnings_24h", "bdag"), mapString(hourly, "wallet_24h_bdag"))

	view.Rows = s.MinerEarningRows()
	view.State = "Earnings"
	view.StateTone = "neutral"
	if number(poolCreditsBdag) == 0 && len(view.Rows) == 0 {
		view.State = "No Earnings Yet"
		view.StateTone = "warn"
	}
	view.Evidence = []Evidence{{"Payment Wallet Balance", bdagValue(walletBalanceBdag)}}
	if creditWalletBdag != "" && creditWalletBdag != walletBalanceBdag && number(mapString(creditWallet, "ok_address_count")) > 0 {
		view.Evidence = append(view.Evidence, Evidence{"Credit Address Balance", bdagValue(creditWalletBdag)})
	}
	view.Groups = []EvidenceGroup{
		{
			Title: "Last Day",
			Items: []Evidence{
				{"24h Earned USD", currency(first(nestedString(s.data, "earnings_24h", "usd"), stringValue(s.data, "wallet_24h_usd")), "$")},
				{"24h Earned BDAG", bdagInt(wallet24hBdag)},
			},
		},
		{
			Title: "Last Hour",
			Items: []Evidence{
				{"Income USD/h", multiplyCurrency(lastHourBdag, usdPrice, "$")},
				{"Income BDAG/h", bdagInt(lastHourBdag)},
			},
		},
	}
	if len(view.Rows) == 0 {
		view.Notes = append(view.Notes, "No earnings rows are available yet; the source data is still warming up or no miner credits have been recorded.")
	}
	if source := stringValue(s.data, "source"); source == "redis-live-estimate" {
		view.Notes = append(view.Notes, "Miner rows are live estimates from Redis miner telemetry and pool job-state; durable paid credits still come from pool accounting when available.")
	}
	if reason := stringValue(s.data, "history_stale_reason"); reason != "" {
		view.Notes = append(view.Notes, reason)
	}
}

func (s PoolEarningsSnapshot) MinerEarningRows() []map[string]string {
	rows := listMaps(s.data["miner_estimates"])
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]string{
			"wallet": first(mapString(row, "name"), mapString(row, "mac"), mapString(row, "device_id"), mapString(row, "miner"), "not known"),
			"avg":    first(mapString(row, "estimated_wallet_bdag_recent_hour"), mapString(row, "estimated_bdag_avg_hour"), mapString(row, "estimated_bdag_1h"), "0"),
			"total":  bdagInt(first(mapString(row, "estimated_bdag_total"), "0")),
			"last":   humanTime(first(mapString(row, "last_share_at"), "not known")),
		})
	}
	if len(out) == 0 {
		rows = listMaps(nestedAny(s.data, "credits", "recent_24h_by_address"))
		if len(rows) == 0 {
			rows = listMaps(nestedAny(s.data, "credits", "by_address"))
		}
		for _, row := range rows {
			total := first(mapString(row, "total_bdag"), "0")
			out = append(out, map[string]string{
				"wallet": shortAddress(first(mapString(row, "miner_address"), "not known")),
				"avg":    decimalInt(number(total) / 24),
				"total":  bdagInt(total),
				"last":   humanTime(first(mapString(row, "last_credit_at"), "not known")),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return number(out[i]["avg"]) > number(out[j]["avg"]) })
	if len(out) > 12 {
		return out[:12]
	}
	return out
}
