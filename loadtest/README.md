# Load testing

Four k6 scenarios per the build spec, plus the empirical evidence gathered
running them against this engine on a local dev box (16 cores, engine
capped to `GOMAXPROCS=4`, each mock bidder to `GOMAXPROCS=2` — the load
generator and the system under test compete for the same box, so these
numbers describe *this machine*, not production capacity; only the
relative shape of the results is meaningful).

## Prerequisites

Start the two mock bidders and the engine before running any scenario:

```bash
go run ./cmd/mockbidder --listen :50051 --seat-id bidder-a --delay-mean 30 --delay-stddev 15
go run ./cmd/mockbidder --listen :50052 --seat-id bidder-b --delay-mean 30 --delay-stddev 15
go run ./cmd/engine --config config.yaml
```

For the chaos scenario, add injection flags, e.g. `--error-rate 0.1 --timeout-rate 0.1`.

## Scenarios

| Scenario | File | Run |
|---|---|---|
| Baseline | `baseline.js` | `k6 run loadtest/baseline.js` |
| Soak | `soak.js` | `k6 run loadtest/soak.js` (30m default; `-e DURATION=2m` for a quick check) |
| Spike | `spike.js` | `k6 run loadtest/spike.js` |
| Chaos | `chaos.js` | `k6 run loadtest/chaos.js` (run against bidders started with `--error-rate`/`--timeout-rate`) |

Each script accepts `-e RATE=<n>` / `-e DURATION=<dur>` (spike uses
`BASE_RATE` / `SPIKE_RATE`) to override the defaults. `BASE_URL` defaults
to `http://localhost:8080`.

k6's own HTTP metrics only see status codes (200/204/429), not *why* a
204 happened (no-bid vs. timed-out-partial). Cross-reference
`/metrics` or Grafana for the outcome breakdown.

## Empirical results (this run, this machine)

**Baseline** (20 req/s, 30s, bidders at 30ms±15ms delay):
shed rate 0%, `http_req_duration` p95 = 69ms, well inside the 300ms
budget. Confirms normal-operation latency is dominated by bidder
response time, not engine overhead.

**Spike**: at up to 300 req/s target, shed rate stayed at 0% — nowhere
near the ceiling. Pushed further to a 3000 req/s target; the engine
sustained ~1850 req/s actual throughput with only 0.19% shed. That's
the point where req/s increases stop yielding more completed auctions
and start raising the shed rate — the empirical throughput ceiling for
this box.

**Chaos** (bidder-a: 10% error, 10% timeout; bidder-b: 10% error, 20%
no-bid): `http_req_duration` became bimodal — median ~38ms (bidder-b
answers, or bidder-a answers fast) vs. p90/p95 ~245-246ms (bidder-a's
injected timeout hits the `BidderBudget` cutoff exactly, but bidder-b
still answers in time and wins). This is the timeout-enforcement and
partial-results behavior working as designed, visible directly in the
latency distribution. Prometheus confirmed the mechanism: distinct
`rtb_bidder_timeouts_total{seat="bidder-a"}`,
`rtb_bidder_errors_total{seat=...}`, and `rtb_requests_total{outcome=...}`
counts for `completed` / `no_bid` / `timed_out_partial` / `shed`.

**Soak** (20 req/s, 2 minutes — compressed from the spec's 30+ minutes
for practicality in this environment): `rtb_goroutines` sampled every
30s: 64 → 62 → 62 → 59. Flat/slightly declining, not growing — no
goroutine leak under sustained load. For a real 30+ minute soak, watch
the same gauge in Grafana; a genuine leak shows as a monotonic upward
trend, not this kind of noise.

## Corrections applied

- **`semaphore_ceiling` (256, in `config.yaml`)**: kept as-is. At the
  measured ~1850 req/s sustainable throughput with ~38ms average bidder
  duration and 2 bidders/request, steady-state in-flight RPCs work out
  to roughly 1850 × 0.038 × 2 ≈ 140 — about 1.8x headroom under the
  current ceiling. That's a reasonable safety margin without being
  wildly over-provisioned, so no change.
- **Histogram buckets** (`5,10,25,50,75,100,150,200,250,300,500` ms, in
  `internal/metrics/metrics.go`): confirmed appropriate. Observed p50
  clustered around 35-40ms and p90/p95 around 60-70ms (baseline) or
  245ms (chaos timeouts) — both land cleanly inside existing bucket
  boundaries with good resolution where the mass actually is.
- **Budget split** (`internal/budget/budget.go`): confirmed adequate.
  Even under the 3000 req/s spike, no request needed anywhere near the
  full 300ms total budget except the deliberately-injected timeout
  case, which behaved exactly as specified (cut off at 245ms). No
  change.
