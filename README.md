# rtb-engine

A real-time bidding exchange in Go: accepts a bid request over HTTP/JSON,
fans it out to N bidders over gRPC under a hard latency budget, selects a
winner, and returns it. Sheds load deterministically when saturated,
rather than queuing and degrading unpredictably.

## How it works

```
HTTP POST /openrtb2/auction (JSON)
        |
        v
  validate + admission check  (MinViableBidderWindow shed floor)
        |
        v
  fan-out to every bidder, bounded by a fixed semaphore
        |         |         |
        v         v         v
    bidder A  bidder B  bidder C   (gRPC, each under its own sub-deadline)
        |         |         |
        v         v         v
  fan-in over a buffered channel (partial results if the deadline hits first)
        |
        v
  select winner (highest qualifying bid, ties -> earliest response)
        |
        v
  HTTP response (200 / 204 / 429)
```

- **Ingress**: HTTP POST + JSON, matching real OpenRTB shape.
- **Egress**: gRPC unary + Protobuf, one persistent `ClientConn` per bidder.
- **Concurrency**: a fixed-size `semaphore.Weighted` bounds in-flight RPCs
  (no dynamic resizing, no queue — a bidder call that can't get a slot is
  shed immediately). Fan-in happens over a buffered channel so a stray
  goroutine never blocks the response and never leaks.
- **Deadlines**: a 300ms total budget per request, with a 245ms per-bidder
  sub-deadline. If a bidder doesn't answer in time, its result is simply
  excluded — the auction still completes with whoever did answer
  (**partial results**).
- **Full spec**: see [`rtb-engine-BUILD-SPEC.md`](rtb-engine-BUILD-SPEC.md)
  (local only, not tracked in git) for the complete design rationale,
  constants, and phase breakdown this was built against.

## Repo layout

```
cmd/
  engine/main.go          HTTP ingress server
  mockbidder/main.go      gRPC bidder with delay/error/timeout/no-bid injection
internal/
  auction/                fan-out, fan-in, winner selection
  bidder/                 gRPC client, one ClientConn per bidder
  budget/                 fixed timing constants (300ms total, 245ms per-bidder, ...)
  config/                 YAML config loader
  httpapi/                ingress handler, JSON codec, error taxonomy
  metrics/                Prometheus collectors
proto/rtbv1/               protobuf schema + generated code
deploy/                    prometheus.yml, Grafana dashboard JSON
loadtest/                  k6 scenarios + results (see loadtest/README.md)
```

## Running it

```bash
# two mock bidders
go run ./cmd/mockbidder --listen :50051 --seat-id bidder-a --delay-mean 30 --delay-stddev 15
go run ./cmd/mockbidder --listen :50052 --seat-id bidder-b --delay-mean 30 --delay-stddev 15

# the engine
go run ./cmd/engine --config config.yaml
```

```bash
curl -X POST http://localhost:8080/openrtb2/auction \
  -H "Content-Type: application/json" \
  -d '{"id":"req-1","items":[{"id":"item-1","bid_floor":1.0,"bid_floor_cur":"USD"}],"tmax":300}'
```

Metrics: `http://localhost:9090/metrics` (Prometheus format). Point
`deploy/prometheus.yml` / `deploy/grafana-dashboard.json` at it for the
latency/throughput/shed-rate dashboard.

## Error taxonomy

| HTTP | Meaning |
|---|---|
| 200 | Winner selected |
| 204 | Valid auction, zero qualifying bids (no-bid or timed-out-partial) |
| 400 | Malformed JSON, multi-item request, non-USD currency, missing field |
| 429 | Shed — semaphore full, or not enough time left to attempt dispatch |
| 500 | Internal error |

v1 restrictions: exactly one `Item` per request, USD only.

## Testing

```bash
go build ./...
go test ./...
```

Unit + table-driven tests for winner selection (floor rejection, ties,
empty), a fuzz test, and deterministic concurrency tests (partial results
under a slow bidder, all-shed under semaphore exhaustion, timeout
classification, goroutine-leak check under a burst) using a
channel-controlled fake bidder — no live gRPC needed for these.

> `go test -race` is not runnable in this dev environment (only a 32-bit
> MinGW gcc is on `PATH`; cgo needs a 64-bit toolchain for the race
> detector on Windows). All tests pass under plain `go test`.

## Performance

Everything below was measured against this repo's own mock bidders on a
local dev box (16 cores; engine capped to `GOMAXPROCS=4`, each mock
bidder to `GOMAXPROCS=2` — the load generator and the system under test
share the same machine, so treat these as *shape of behavior*, not
production capacity numbers). Full methodology and reasoning behind each
number is in [`loadtest/README.md`](loadtest/README.md).

### Baseline — steady load, well under capacity

20 req/s for 30s, bidders at 30ms±15ms response delay:

| Metric | Value |
|---|---|
| Shed rate | 0% |
| `http_req_duration` p90 / p95 | 60ms / 69ms |
| Budget headroom | 69ms used of 300ms (23%) |

### Spike — finding the real throughput ceiling

Ramped target rate up to 3000 req/s (well past the semaphore ceiling):

| Metric | Value |
|---|---|
| Sustained actual throughput | ~1,850 req/s |
| Shed rate at that throughput | 0.19% |
| p95 latency | 69ms (unchanged from baseline) |

Steady-state in-flight RPC estimate at this throughput: ~1850 × 38ms ×
2 bidders ≈ 140 concurrent — about 1.8x headroom under the configured
`semaphore_ceiling: 256`. Confirmed adequate, no change made.

### Chaos — 10% error rate, 10% timeout rate, 20% no-bid (mixed across bidders)

20 req/s for 20s against bidders with injected failures:

| Metric | Value |
|---|---|
| Shed rate | 0% |
| 204 (no qualifying bid) rate | 5.5% |
| `http_req_duration` median | 38ms |
| `http_req_duration` p90 / p95 | 245ms / 246ms |

The latency distribution is **bimodal by design**: fast when a bidder
answers normally, and clipped exactly at the 245ms `BidderBudget` when a
bidder's injected timeout fires — direct evidence the per-bidder deadline
is enforced, not just documented. Confirmed via Prometheus:
`rtb_bidder_timeouts_total{seat="bidder-a"}`,
`rtb_bidder_errors_total{seat=...}`, and per-outcome
`rtb_requests_total{outcome="completed"|"no_bid"|"timed_out_partial"|"shed"}`
counts all moved independently and correctly.

### Soak — leak check over sustained load

20 req/s for 2 minutes (compressed from the spec's 30+ minutes for this
environment), `rtb_goroutines` sampled every 30s:

| t=30s | t=60s | t=90s | t=120s |
|---|---|---|---|
| 64 | 62 | 62 | 59 |

Flat/slightly declining — no leak. A real leak shows as a monotonic
upward trend over the full window, not this kind of noise.

### Volume — uncapped burst at 100K and 1M total requests

`loadtest/volume.js`, 200 VUs firing as fast as possible (no rate
limiting), fixed total request count:

| Total requests | Wall time | Actual throughput | Shed rate | Completed | p95 (completed) |
|---|---|---|---|---|---|
| 100,000 | 6.8s | ~14,700 req/s | 62.3% | 37.7% | 65ms |
| 1,000,000 | 85.2s | ~11,700 req/s | 54.4% | 45.6% | 68ms |

This is deliberately harder than the spec's spike scenario — no ramp,
just immediate maximum-concurrency saturation. The shed rate is high
because that's the semaphore doing exactly its job: fast-rejecting
(0ms, no RPC attempted) rather than queuing and degrading. Health
checks after each run confirmed the system fully recovered:

- **100K run**: `rtb_goroutines` back to 19 (idle baseline) immediately after.
- **1M run**: `rtb_goroutines` at 18 afterward, `rtb_semaphore_inflight`
  at 0, and the engine served a fresh request at 20ms latency — fully
  healthy, no degradation carried over from 1.1M cumulative requests
  across both runs.
- Zero unexpected HTTP statuses across either run (100% of responses
  were 200/204/429, exactly the documented taxonomy) — no crashes,
  no panics, no unbounded queuing.

Reproduce:

```bash
k6 run -e ITERATIONS=100000  -e VUS=200 loadtest/volume.js
k6 run -e ITERATIONS=1000000 -e VUS=200 loadtest/volume.js
```
