# Ben Dash

Ben Dash is the next-generation candidate for the BlockDAG operations dashboard. It exists to evolve the current dashboard experience while preserving the stack contracts needed for promotion into the canonical release path.

## Language

**Successor Dashboard Candidate**:
A dashboard implementation intended to replace or be promoted into the canonical BlockDAG operations dashboard after it proves parity, safety, and operational value.
_Avoid_: Prototype, detached dashboard, side dashboard

**Canonical Dashboard**:
The current production dashboard source used by the BlockDAG stack release path.
_Avoid_: Old dashboard, legacy dashboard

**Pool Dashboard**:
The operator-facing name for the successor dashboard user interface.
_Avoid_: Ben Dash, BlockDAG Operations

**Single-Pool Dashboard**:
The v1 dashboard scope: one local Stratum pool connected to one local BlockDAG node backend.
_Avoid_: Multi-pool dashboard, backend switcher, pool aggregator

**Catching Up**:
The node is connected to peers and actively importing chain data, but it is not ready for block submission.
_Avoid_: Synced, ready

**Synced**:
The node chain tip is fresh and imports are not stale. This does not by itself mean the pool can safely submit blocks.
_Avoid_: Mining ready

**Mining Ready**:
The node and pool template path are fresh, submit-ready, and clear of sync vetoes, so solved work can be submitted safely.
_Avoid_: Synced

**Chain Block Count**:
The primary node height shown by the dashboard, sourced from native BlockDAG chain RPC.
_Avoid_: EVM height, template height

**Node Status Log Event**:
The structured `node_status_log` JSON event emitted in node logs with schema `bdag.nodeworker.status_log.v1`. It is retained for explicit log diagnostics; the Overview uses the shared live status sample so service-based and container-based nodes work alike.
_Avoid_: Treating missing log events as a stopped node

**Node Status Card Grid**:
The Overview presentation of native and EVM sync evidence from the shared live status sample. Node sync and pool submit readiness remain distinct.
_Avoid_: Treating synced as mining ready or presenting missing evidence as healthy

**Pool Status Log Event**:
The structured `pool_status` JSON event emitted in pool logs with schema `bdag.pool.status.v1`. It is retained for explicit log diagnostics; the Overview uses the shared live pool and job-state sample.
_Avoid_: Treating missing log events as a stopped pool

**Pool Status Card Groups**:
The Overview presentation of live pool readiness and ASIC performance, based on the same cached sample as the status API.
_Avoid_: Treating a synced node alone as proof that mining is ready

**Status Evidence Rows**:
The default Status evidence set for readiness facts such as Accepting Shares, Work Fresh, Submit Ready, Paid Mining Healthy, and supporting pool evidence.
_Avoid_: Separate node evidence tab, hidden readiness ladder

**P2P Summary**:
A compact peer connectivity and P2P health context area when P2P evidence is shown.
_Avoid_: Error-only P2P, hidden peer state

**Observed Peer IPs**:
Read-only P2P evidence. It is not Global production context.
_Avoid_: Peer IPs as global production context

**Peer List Tab**:
The read-only Peers tab showing the current native node peer RPC result. It uses
`getPeerForks` when available and falls back to older peer RPCs for node builds
that do not expose fork-aware peer details.
_Avoid_: Global production peers, editable peer inventory

**Peer Fork Evidence**:
Read-only Peers tab evidence showing whether the node currently observes each peer as sharing the local Main chain, sharing only a known prefix, diverging, or remaining unknown.
_Avoid_: editable peer labels, peer ban list

**Main Order**:
A BlockDAG sync and P2P diagnostic value used to explain catch-up state.
_Avoid_: Primary height

**EVM Block**:
An EVM-side diagnostic value used for wallet and EVM health context.
_Avoid_: Mining height, chain truth

**Read-Only Dashboard**:
The default dashboard interaction scope: Status, Pool Earnings, Global, and logs present operational evidence and recommended actions without restarting services, editing runtime config, or changing miner settings. Explicit miner mutation belongs behind the Miner Control Module, not inside collector or evidence views.
_Avoid_: Repair console, control plane

**Evidence Link**:
A read-only link or button that opens supporting logs, payload details, or diagnostic evidence for a degraded state.
_Avoid_: Repair action, restart button, config mutation

**Miner Control Module**:
A separate privileged module used by the Pool Dashboard to apply miner pool configuration or restart known miners. It owns ASIC control protocol details, target safety checks, control-token authorization, retries, redaction, and action results.
_Avoid_: Collector mutation, dashboard-owned ASIC protocol, unaudited repair button

**Miner Control Action**:
An explicit operator-triggered mutation routed through the Miner Control Module, scoped to a known miner target from the Dashboard API Contract.
_Avoid_: Automatic repair, collector side effect, inferred config change

**Accepting Shares**:
The pool can accept Stratum share submissions from connected miners.
_Avoid_: Paid mining

**Work Fresh**:
The pool has current template or job work suitable for miners.
_Avoid_: Synced

**Submit Ready**:
The pool and node submit path can safely submit solved block candidates.
_Avoid_: Accepting shares

**Paid Mining Healthy**:
The pool has recent accepted block submissions or confirmed on-chain/blue production.
_Avoid_: Share healthy, miner connected

**Status State Banner**:
The primary Status presentation: all Pool Readiness Ladder states are visible, with the highest proven active state highlighted and the other states muted.
_Avoid_: Miner table as headline, raw metrics first, worst blocker as headline, hidden inactive readiness states

**Pool Readiness Ladder**:
The ordered Status state model: Accepting Shares, Work Fresh, Submit Ready, Paid Mining Healthy.
_Avoid_: Flat pool health, blocker-first status

**Status Readiness Snapshot**:
A read-only projection of Status readiness facts used to render the Status State Banner, Pool Readiness Ladder, Pool Evidence Checklist, warnings, and supporting evidence without changing miner configuration or runtime state.
_Avoid_: Mutable pool state, repair plan, hidden readiness derivation

**Pool Evidence Checklist**:
The Status evidence rows show each readiness level as achieved or missing beneath the Status State Banner.
_Avoid_: Hidden missing readiness levels, ambiguous pool healthy

**Miner Table**:
A dense Status table for physical ASIC observations, grouped by MAC identity and optimized for scanning. It is visible directly under the Status summary.
_Avoid_: Miner cards, IP-keyed miner list, separate miner tab, status-grouped miners

**Miner Fleet Snapshot**:
A read-only projection of known physical ASICs used by Status, Pool Earnings, and Miner Control target selection. It normalizes MAC-based identity, current IP observation, configured intent, performance evidence, telemetry evidence, and stale/configured state without mutating miners.
_Avoid_: Miner control state, IP-keyed miner registry, mutable ASIC inventory

**Miner Observed Identity**:
The currently observed miner facts, including MAC identity, IP address, worker label, and port.
_Avoid_: IP as identity, worker label as identity

**Miner Configured Intent**:
The expected miner configuration, such as intended pool URL or wallet, when available from the Dashboard API Contract.
_Avoid_: Observation-only configuration truth

**Stale Miner Observation**:
A previously known miner that is no longer freshly observed but remains visible with stale/offline evidence.
_Avoid_: Silent miner disappearance

**Pool Earnings**:
The tab for local pool and miner-attributed earnings, including wallet totals, pending or confirmed maturity, and recent local earning trends.
_Avoid_: Global production

**Pool Earnings Snapshot**:
A read-only projection of local pool earnings facts used to render Confirmed Earnings, Pending Earnings, Fiat Earnings Context, Miner Earnings List, and live estimate notes without changing accounting truth.
_Avoid_: Global production snapshot, optimistic payout state, mutable accounting state

**Confirmed Earnings**:
The primary Pool Earnings headline value: local pool earnings that are confirmed rather than pending or unconfirmed, denominated first in BDAG.
_Avoid_: Combined pending headline, optimistic earnings, fiat-primary earnings

**Pending Earnings**:
Pool earnings that are visible but not yet confirmed; shown as supporting context under confirmed earnings with payout or maturity state folded into the summary.
_Avoid_: Confirmed earnings

**Fiat Earnings Context**:
The secondary fiat conversion display for pool earnings.
_Avoid_: Fiat-primary earnings

**Miner Earnings List**:
The primary miner-attributed Pool Earnings presentation: a ranked visual list or chart of miner earnings contribution, defaulting to a 24-hour chart window.
_Avoid_: Dense miner earnings table as default

**All-Time Earnings Lens**:
The default Pool Earnings time lens. Shorter windows may be selectable, but all-time confirmed earnings remain the default.
_Avoid_: 24-hour default earnings

**Global**:
The tab for network-wide production context, including global chain production, top producers, local pool share, and global trends.
_Avoid_: Local earnings

**Pool Hashpower**:
The Global tab's sum of fresh, unique ASIC-reported rates for devices whose
active endpoint matches the local pool. The display calls this the total only
when fresh job-state evidence covers every running configured pool and every
connected lane maps to a measured ASIC; otherwise it labels the sum observed.
It is not network hashpower or a share-based accounting value.
_Avoid_: Network hashpower, share-based accounting value

**Global Production Snapshot**:
A read-only projection of network producer evidence used by Network Production Health, Producer Trend, Local Pool Highlight, and Estimated Pool Earnings Table. It preserves the Dashboard API Contract as truth while allowing Redis live evidence to provide bounded best-effort context.
_Avoid_: Duplicate global truth engine, pool earnings source, unbounded producer payload

**Network Production Health**:
The primary Global headline: network-wide chain production and health context before local pool comparison.
_Avoid_: Local pool share as Global headline

**Global Stats Strip**:
A compact Global summary strip for current chain and scan facts such as Chain Block Count, Main Order, EVM Block, latest block, scanned blocks, unique producers, scan window, and block rate.
_Avoid_: Hidden global scan context

**Global Chart Window**:
The default Global chart time window is 24 hours.
_Avoid_: All-time global chart default

**Producer Trend**:
The primary Global chart, matching the canonical dashboard's producer trend view and placed at the top of the Global tab.
_Avoid_: Producer trend below tables, different first chart

**Top Producers Chart**:
The primary Global presentation for ranked network producers.
_Avoid_: Top producers table as default

**Local Pool Highlight**:
The local pool remains visible inside Global producer comparisons even when it is not near the top of the network ranking.
_Avoid_: Local pool only below chart, hidden local rank

**Estimated Pool Earnings Scan**:
The Global estimated pool earnings view uses a fixed 600 scanned-block window.
_Avoid_: Variable estimated earnings scan size, 2048-block estimated earnings default

**Estimated Pool Earnings Table**:
The Global table below Producer Trend showing estimated earnings by pool with the canonical dashboard columns: Pool, Blocks, Work %, and Last Seen.
_Avoid_: Visual-only pool earnings comparison, redesigned estimated pool earnings columns

**Dashboard API Contract**:
The existing canonical dashboard API used as the v1 truth source for the successor dashboard candidate. Ben Dash v1 consumes this contract rather than collecting chain, pool, miner, earnings, or global truth independently.
_Avoid_: Reimplemented collectors, duplicate truth engine

**Truth Parity**:
The successor dashboard presents the same operational truth as the canonical dashboard for node, pool, miner, earnings, and global chain state.
_Avoid_: Visual-only redesign, approximate status

**Status**:
The tab that shows Stratum pool health, pool work readiness, and miner health/configuration. Physical ASIC identity is MAC-based; IP address, worker label, port, and display name are observations.
_Avoid_: Node Status tab, Pool Status tab, Miners tab, ASIC status tab
