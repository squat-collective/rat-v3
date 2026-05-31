# rat-bench-latency-go — per-RPC latency benchmark (sub-phase 0f)

Quantifies the one perf number the RAT architecture actually trades on: the
**core-mediated gateway's overhead vs a direct call**. ADR-005 accepted "a latency
hop per control call" (and a direct-dial fast-path *only if a profiling pass shows
it's needed*); ADR-008 added a streaming relay. This is that profiling pass.

It measures the **same plugin RPC two ways** — direct (`caller → plugin`) and
mediated (`caller → gateway → plugin`) — for a unary RPC (`state.Get`) and a
server-streaming one (`runtime.Execute`), and reports p50/p99/mean + the delta. The
plugin RPCs are trivial (a fixed response / a few fixed frames) so the measurement
isolates transport + mediation cost, not the plugin's work. The mediated path
includes the client-side marshal/unmarshal + the `rat-callmeta-bin` envelope stamp
(the SDK's real cost), and the gateway's traceparent-validate + identity-restamp +
passthrough relay.

## Run it (containerized — no host installs)

```bash
make bench
# or:
podman run --rm -v "$PWD":/work:Z -v rat-gocache:/go/pkg/mod \
  -w /work/examples/bench/latency-go golang:1.25 go run . 20000
```

Sample (localhost TCP, single goroutine — numbers vary by host):

```
  scenario                                  p50        p99       mean
  unary  state.Get        direct         62.3µs    301.8µs     85.4µs
  unary  state.Get        mediated      228.2µs   1108.9µs    263.2µs
  stream runtime.Execute   direct        66.0µs    404.4µs     95.6µs
  stream runtime.Execute   mediated     248.9µs   1001.9µs    282.2µs
  → unary  state.Get         gateway overhead (p50): +165.9µs (+266%)
  → stream runtime.Execute   gateway overhead (p50): +182.9µs (+277%)
```

## Reading the result

Mediation roughly triples a control RPC's latency (it adds a full extra gRPC hop +
serialization) but the **absolute cost is ~0.2ms**. That's the accepted price of
central enforcement (C2/C5/C7/C8 + trace + audit at one point — ADR-005): a pipeline
run makes a handful of control calls, so ~ms of mediation overhead is negligible
against the actual data work. And the hot path doesn't pay it at all — **bulk DATA
bypasses the gateway entirely** via the out-of-band `ArrowStream` leg
(`overview.md` "data plane bypasses core for bytes"). The benchmark confirms the
ADR-005 bet: cheap enough for control, and absent from data. If a future hot control
path ever needs sub-mediation latency, the direct-dial fast-path ADR-005 left open
can be added — but this shows it isn't needed for v1.

This is a benchmark, not a conformance reference (no `harness_test.go`), so it is
excluded from `make conformance`.
