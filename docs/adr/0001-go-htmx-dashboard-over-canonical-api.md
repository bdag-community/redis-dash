# ADR 0001: Build Pool Dashboard as a Go HTMX UI over the Canonical Dashboard API

Date: 2026-06-05

## Status

Accepted. Amended by ADR 0002 for explicit Miner Control Actions.

## Context

Ben Dash is a successor dashboard candidate for the BlockDAG pool operations dashboard. The v1 goal is truth parity with the canonical dashboard plus a redesigned operator experience.

The canonical dashboard already owns the expensive and safety-sensitive collection path for node, pool, miner, earnings, P2P, and global chain state. Its collectors read Docker, node RPCs, logs, pool metrics, Postgres, wallet/EVM data, runtime cache files, and background sampler output. Reimplementing those collectors in Ben Dash v1 would create a second truth engine and increase runtime pressure on constrained mining hosts.

The desired v1 stack is Go, HTMX, and Tailwind.

## Decision

Ben Dash v1 will be a Go server-rendered dashboard using HTMX partial updates and Tailwind styling.

Ben Dash v1 will consume the existing canonical dashboard API as its operational truth source. It will not independently collect chain, pool, miner, earnings, P2P, or global production truth.

The Go server will map canonical API payloads into small view models and render HTML pages or HTMX partials. Browser-side JavaScript will be limited to bounded interactions that need it, especially charts.

## UI Scope

The dashboard title is `Pool Dashboard`.

The header contains only the logo and title.

The v1 tabs are:

- Status
- Pool Earnings
- Global

The v1 dashboard is read-only. It may expose evidence links for logs, payloads, or diagnostics, but it will not restart services, mutate config, or configure miners.

## Tab Decisions

Status leads with a State Banner that shows the highest proven pool readiness level in this ladder:

- Accepting Shares
- Work Fresh
- Submit Ready
- Paid Mining Healthy

Missing higher readiness levels are shown in the evidence checklist beneath the banner.

Status includes the miner table directly under the summary. Miner rows are grouped by MAC-based physical identity. Rows show observed identity and configured intent when available. Stale miners remain visible with stale/offline evidence instead of disappearing silently.

Pool Earnings leads with confirmed earnings in BDAG. Pending or unconfirmed earnings and payout/maturity state are folded into the summary. Fiat conversion is secondary context.

Pool Earnings uses a ranked visual miner earnings list or chart before a dense table. The miner contribution chart defaults to a 24-hour window. The headline earnings lens remains all-time confirmed earnings.

Global leads with the canonical Producer Trend graph, defaulting to a 24-hour chart window. The local pool remains highlighted inside producer comparisons even if it is not near the top of the ranking.

Global keeps a compact stats strip for chain and scan facts, including Chain Block Count, Main Order, and EVM Block. Global keeps the estimated pool earnings table below Producer Trend with the canonical dashboard columns:

- Pool
- Blocks
- Work %
- Last Seen

The Global estimated pool earnings view uses a fixed 600 scanned-block window.

## Browser Performance

The Go server should avoid sending full canonical API payloads to the browser. It should fetch, reduce, and render only the fields required for each view or partial.

HTMX refreshes should be section-level, not whole-page refreshes. Active tabs may refresh frequently; inactive tabs should refresh slowly or on activation.

Chart data must be bounded. Producer Trend should limit visible producer series to top producers plus the local pool and downsample points if needed.

Go-side short TTL caching should align with canonical API refresh expectations, for example status around 5-10 seconds, earnings around 30 seconds, and global around 60 seconds.

## Consequences

Truth parity is easier to verify because Ben Dash reads from the same source as the canonical dashboard.

Runtime pressure is lower because Ben Dash does not duplicate Docker, RPC, log, miner, or database collectors.

The canonical dashboard API becomes a promotion-critical contract for Ben Dash v1.

Some UI requirements may require small canonical API changes. In particular, the fixed 600-block estimated pool earnings scan should be separated from Producer Trend data if the current global API cannot represent those windows independently.

Replacing collectors with Go-native collectors is explicitly deferred until after v1 truth parity is proven.

## Alternatives Considered

Reimplement collectors in Go.

Rejected for v1 because it would create duplicate operational truth, increase load, and risk contradictory dashboard states.

Build a browser-heavy frontend against the canonical JSON API.

Rejected for v1 because the desired stack is Go + HTMX + Tailwind, and server-rendered partials reduce browser payload size and layout churn.

Keep the existing Python dashboard UI and only restyle it.

Rejected because Ben Dash is intended to create a successor dashboard candidate while preserving the canonical truth source.
