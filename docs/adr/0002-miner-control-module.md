# ADR 0002: Add Miner Control as a Separate Privileged Module

Date: 2026-06-17

## Status

Accepted

## Context

ADR 0001 made Ben Dash a read-only Pool Dashboard over the canonical Dashboard API Contract. That remains the right default for Status, Pool Earnings, Global, and logs because the collector owns operational truth and the Go dashboard should not duplicate expensive or safety-sensitive collection work.

Operators now need an explicit way to apply miner pool configuration and restart known miners from the dashboard. Those actions are mutations, not evidence reads. Putting them in the collector would make a read-oriented truth source perform side effects, weakening locality and making failures harder to reason about.

## Decision

Ben Dash may expose explicit Miner Control Actions, but they must go through a separate Miner Control Module.

The collector remains read-only. It reports observed miner state through the Dashboard API Contract. It does not configure miners, restart miners, or hide mutation behind collection endpoints.

The Go dashboard remains the operator-facing UI. It can render a Miner Control tab and action forms, but it calls the Miner Control Module through a narrow interface rather than embedding ASIC protocol concerns across dashboard views.

The initial implementation keeps the Miner Control Module as a Go package inside this repository. The module interface is the seam. The first adapter speaks the X30/X100-style MCB HTTP control protocol directly. If deployment requirements demand stronger isolation, the same module interface can later be backed by a local daemon over localhost or a Unix socket.

Miner control is disabled by default. Enabling it requires:

- `BDAG_MINER_CONTROL_ENABLED=1`
- `BDAG_MINER_CONTROL_TOKEN`
- `BDAG_DEFAULT_WORKER_USER` optionally supplies an explicit worker or wallet
  for the form. Without it the field is empty; the pool operator's coinbase
  address is not assumed to be an ASIC owner's address.

Each action validates that the target is a known miner from the Dashboard API Contract before crossing into the control module. The adapter refuses public, loopback, multicast, unspecified, and Docker bridge-range targets by default, and it can be constrained further with `BDAG_ASIC_LAN_CIDRS`.

## Consequences

The collector keeps high locality for observation and truth parity.

The Miner Control Module concentrates mutation knowledge: ASIC login, payload shape, retries, redaction, target safety, and action result formatting.

The Pool Dashboard gains a privileged action path, so deployment must treat miner control differently from ordinary read-only dashboard evidence. Operators should keep the dashboard bound to a trusted interface or put it behind access control when miner control is enabled.

Tests can use a fake Miner Control Module adapter at the dashboard seam and fake HTTP transport inside the ASIC adapter. This keeps UI/authorization tests separate from protocol tests.

## Alternatives Considered

Put miner control in the collector.

Rejected because it turns the collector into both truth source and mutation actor. That makes read paths harder to trust and spreads side-effect safety across collection code.

Let the Go dashboard directly own ASIC HTTP calls in handlers.

Rejected because it is shallow: each handler would need to know target validation, authentication, MCB paths, token headers, retries, and redaction. The Miner Control Module gives better locality and a seam that can later move to a separate process.

Build a separate daemon immediately.

Deferred. A separate daemon may be the right deployment shape, but the current repository can first prove the interface and operator workflow with an in-process module. The interface is intentionally shaped so a daemon adapter can replace the in-process adapter without changing dashboard views.
