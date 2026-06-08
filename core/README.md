# `core/` — the RAT v3 spike core

> Module `github.com/rat-dev/rat/core`. The Phase-1 **contract-de-risking spike**
> ([ADR-013](../docs/architecture/adrs/013-phase-1-spike-and-commitment-gate.md) ·
> [ADR-014](../docs/architecture/adrs/014-spike-core-registry-and-invoke-gateway.md)).
> **Not** the full core — two of the six things, built to make **C5 real**.

## What this is

The minimum real core that turns **C5 (capability enforcement)** from a self-asserted
stub into a decision *derived from declared plugin manifests*:

- [`manifest/`](manifest/) — loads the frozen `plugin.v1.json` manifest shape (the real
  `contracts/examples/*.plugin.yaml`) into Go structs; validates the capability-URI grammar.
- [`registry/`](registry/) — indexes manifests by name + provided capability, and answers
  the C5 decision: **`Authorize(caller, capability)` is allowed iff the caller's manifest
  `requires` it AND some registered plugin `provides` it.** No hardcoded allowlist — the
  thing the throwaway `plugins/*/gateway_test.go` stubs faked.

## Coming next (this spike)

- `gateway/` — the `CapabilityInvokeService` (seeded from `plugins/bench/latency-go/gateway.go`),
  with its C5 decision wired to `registry.Authorize` + an audit record per decision (C4).
- A composition-on-Go test that re-runs the pipeline through this gateway, plus the
  C5-negative / C1 crash-mid-strategy / C2 truncation cases (ADR-014 §5).

## Run

```bash
# from the repo root (containerized, no host installs):
podman run --rm --userns=keep-id -e HOME=/tmp -e GOTOOLCHAIN=local \
  -v "$PWD":/work:Z -v rat-gocache:/go/pkg/mod -w /work/core \
  docker.io/library/golang:1.25 sh -c 'go mod tidy && go test ./...'
```
