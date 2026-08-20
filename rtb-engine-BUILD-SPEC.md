# RTB Engine — Build Spec

Go real-time bidding exchange. Accepts a bid request over HTTP/JSON, fans out to N bidders over gRPC under a hard latency budget, selects a winner, returns it. Sheds load deterministically when saturated.

All decisions below are final. Do not re-litigate them. Rationale lives in `rtb-engine-architecture.md`.

---

## 1. Constants

```go
const (
    TotalBudget            = 300 * time.Millisecond // tmax
    IngressBudget          = 20 * time.Millisecond  // parse, validate, filter
    BidderBudget           = 245 * time.Millisecond // per-bidder context deadline
    ResponsePrepBudget     = 25 * time.Millisecond  // select winner, serialize
    JitterSlack            = 10 * time.Millisecond
    MinViableBidderWindow  = 60 * time.Millisecond  // shed floor; independent of BidderBudget
)
```

Sums to exactly 300ms. `MinViableBidderWindow` is a separate constant — never derive it from `BidderBudget`.

---

## 2. Scope

**In:** single-hop exchange → N bidders → winner → response. Bounded in-flight RPCs. Context-deadline propagation. Partial results. Deadline-aware load shedding. Prometheus metrics + structured logs.

**Out:** pending/loss/billing notices, PMP deals, macros, `schain`, multi-hop chains, bidder auth, fraud detection, distributed tracing, bidder health registry, circuit breakers, `sync.Pool`.

**v1 restrictions (enforce at validation, reject with 400):**
- Exactly **one `Item`** per `BidRequest`. Multi-item is Phase 5.
- **USD only.** Reject any non-USD `cur`.

---

## 3. Repo layout

```
cmd/
  engine/main.go          // HTTP ingress server
  mockbidder/main.go      // gRPC bidder with delay + error injection
internal/
  auction/                // dispatch, fan-in, winner selection
  bidder/                 // gRPC client pool, one ClientConn per bidder
  config/                 // YAML loader
  httpapi/                // ingress handler, JSON codec, error taxonomy
  metrics/                // Prometheus collectors
proto/rtbv1/              // generated from rtb.proto
deploy/                   // prometheus.yml, grafana dashboard JSON
loadtest/                 // k6 scripts
```

---

## 4. Transport

**Ingress: HTTP POST + JSON** at `POST /openrtb2/auction`. Matches real OpenRTB. Lets k6 drive without a gRPC extension.

**Egress: gRPC unary + Protobuf.** One `grpc.ClientConn` per bidder, created at startup, reused for process lifetime. Never per-request.

Set `MaxConcurrentStreams` on mock bidder servers above the semaphore ceiling. HTTP/2 stream limits otherwise become a hidden second ceiling that queues inside the gRPC client, invisible to metrics.

### Error taxonomy (ingress)

| Code | Condition |
|---|---|
| 200 | Winner selected |
| 204 | Valid auction, zero qualifying bids |
| 400 | Malformed JSON, multi-item, non-USD, missing required field |
| 429 | Shed (semaphore full or deadline below floor) |
| 500 | Internal error |

---

## 5. Protobuf schema

```protobuf
syntax = "proto3";
package rtb.v1;
option go_package = "github.com/ServerCrash358/rtb-engine/proto/rtbv1";

message Item {
  string id = 1;
  double bid_floor = 2;      // CPM; 0 = no floor. Enforced ENGINE-SIDE.
  string bid_floor_cur = 3;  // v1: "USD" only
}

message BidRequest {
  string id = 1;             // correlation ID: every log line, metric label, bidder call
  repeated Item items = 2;   // v1: exactly one
  int32 tmax = 3;            // INFORMATIONAL ONLY — gRPC deadline is authoritative
}

message Bid {
  string id = 1;
  string item_id = 2;
  double price = 3;
  string cur = 4;            // v1: "USD" only
  string seat_id = 5;
}

message Seatbid {
  string seat = 1;
  repeated Bid bid = 2;
}

message BidResponse {
  string id = 1;             // must echo BidRequest.id
  repeated Seatbid seatbid = 2;  // empty = no bid
}

service Bidder {
  rpc GetBid(BidRequest) returns (BidResponse);
}
```

No `deadline_unix_ms` — absolute epoch across a process boundary is a clock-skew hazard, and gRPC already propagates a relative deadline via `grpc-timeout` from the context.

No `allowed_seats` — never actionable by the bidder, and leaks competitor identity.

---

## 6. Concurrency

### Bounded in-flight RPCs

`golang.org/x/sync/semaphore.Weighted`, **fixed size**. Not a worker-goroutine pool, not dynamically resized.

```
ceiling = expected_concurrent_requests × avg_eligible_bidders_per_request
```

Start at 256. Correct empirically in Phase 4.

The bound exists for **predictable admission control and backpressure**, not goroutine memory cost.

### Shed rule — no queue

```go
if remaining := time.Until(deadline); remaining < MinViableBidderWindow {
    return shed()
}
if !sem.TryAcquire(1) {
    return shed()
}
defer sem.Release(1)
```

Fast-reject only. No FIFO queue, no re-check loop, no dequeue-time shedding.

### Fan-out

1. Filter M registered bidders → N eligible (seat allowlist from config). Synchronous, pre-dispatch.
2. Derive per-bidder context: `context.WithTimeout(reqCtx, BidderBudget)`.
3. Spawn one goroutine per eligible bidder. Pass `ctx` as the **first parameter** — never capture via closure.
4. Pass the same `ctx` into the gRPC stub call. This is what aborts in-flight I/O; a `select` on `ctx.Done()` alone does not.

### Fan-in

- **Buffered channel, capacity N.** Guarantees every bidder goroutine completes its send and exits even after fan-in moves on. Unbuffered leaks.
- Collect via `select` on the result channel and `ctx.Done()`, plus a response counter. First to fire ends collection.
- Write results into a **pre-sized slice indexed by dispatch position.** No hash map — bidders are known at dispatch.

### Winner selection

```go
func selectWinner(bids []Bid, floor float64) (Bid, bool)
```

Single linear pass. No heap, no map. N ≈ 5–50; a linear scan beats heap-build + extract on wall-clock at that size.

- Discard `price < floor` **engine-side**.
- Ties → **earliest response wins** (by receive index, already available from the slice).
- Zero qualifying bids → `(Bid{}, false)` → HTTP 204.

### Synchronization

- No `sync.WaitGroup` on the auction path — the buffered channel is the sync mechanism.
- `WaitGroup` only for graceful shutdown drain.
- Metrics: rely on the Prometheus client's internal atomics. No hand-rolled mutex.
- No shared mutable state on the hot path. Bidder registry is read-only after startup.

### Graceful shutdown

On SIGTERM: stop accepting ingress → drain in-flight auctions up to `TotalBudget` → close all `ClientConn`s → exit.

---

## 7. Config

`--config path/to/config.yaml`

```yaml
listen_addr: ":8080"
metrics_addr: ":9090"
semaphore_ceiling: 256
bidders:
  - seat_id: "bidder-a"
    endpoint: "localhost:50051"
  - seat_id: "bidder-b"
    endpoint: "localhost:50052"
```

---

## 8. Mock bidder

`cmd/mockbidder` — gRPC server implementing `Bidder`.

Flags:
- `--listen` address
- `--seat-id`
- `--delay-mean`, `--delay-stddev` — lognormal response delay, not a fixed constant
- `--error-rate` — fraction of calls returning a gRPC error
- `--timeout-rate` — fraction deliberately exceeding `BidderBudget`
- `--nobid-rate` — fraction returning an empty `seatbid`

Also expose a **test-only mode** where the response blocks on a channel the test controls directly, for deterministic interleaving in unit tests.

---

## 9. Observability

**Prometheus metrics:**

| Metric | Type | Labels |
|---|---|---|
| `rtb_requests_total` | counter | `outcome` |
| `rtb_request_duration_ms` | histogram | — |
| `rtb_bidder_duration_ms` | histogram | `seat` |
| `rtb_winner_selection_duration_ms` | histogram | — |
| `rtb_bidder_timeouts_total` | counter | `seat` |
| `rtb_bidder_errors_total` | counter | `seat` |
| `rtb_shed_total` | counter | `reason` |
| `rtb_goroutines` | gauge | — |
| `rtb_semaphore_inflight` | gauge | — |

`outcome` ∈ `{completed, no_bid, timed_out_partial, shed, error}`
`reason` ∈ `{semaphore_full, deadline_below_floor}`

**Histogram buckets (ms):** `5, 10, 25, 50, 75, 100, 150, 200, 250, 300, 500`
Dense below 300ms — that's where all interesting variation lives. Re-check against real distributions in Phase 4; wrong boundaries produce a silently misleading p99, not an obvious error.

**Structured log, one line per request:**
`request_id`, `eligible_bidder_count`, `responded_bidder_count`, `winner_seat_id`, `winner_price`, `total_latency_ms`, `outcome`

`request_id` is the **sole** correlation key across logs and metrics — no tracing spans. Every per-bidder log line and metric must carry it explicitly.

**Grafana panels:** request latency p50/p95/p99 · throughput · timeout + shed rate · goroutine count · semaphore utilization · per-bidder latency breakdown.

---

## 10. Testing

- `go test -race ./...` as the standard command, wired into CI as a required check.
- **Unit, table-driven:** `selectWinner` — no bids, one bid, many bids, all below floor, exact tie, non-USD rejection.
- **Fuzz:** `go test -fuzz` on `selectWinner` with randomized bid sets including deliberate ties.
- **Deterministic concurrency tests:** use the mock bidder's channel-controlled mode. Example assertion: bidder A responds at 50ms, bidder B never responds, context expires at 245ms → exactly 1 response collected, 0 leaked goroutines.
- **Leak check:** `runtime.NumGoroutine()` before/after a burst of concurrent auctions, asserting return to baseline.

---

## 11. Load testing

**Tool: k6.** HTTP ingress removes the gRPC-support question entirely.

| Scenario | Purpose |
|---|---|
| Baseline | Steady load below ceiling. Produces normal-operation p50/p95/p99 at ~0 shed rate. |
| Soak | Baseline sustained 30+ min. Validates no goroutine leak via the gauge trend. |
| Spike | Burst above ceiling. Exercises the shed path; produces the degradation evidence. |
| Chaos | Baseline + mock bidder error/timeout injection. Proves partial results and per-bidder timeout enforcement. |

Report p50/p95/p99, error rate, timeout rate, shed rate, and throughput ceiling (where req/s increases stop yielding completed auctions and only raise shed rate).

**Environment: local machine, modest specs.** The load generator competes with the engine and mock bidders for CPU, so the throughput ceiling measures the box, not the engine. Set `GOMAXPROCS` explicitly per process. Quote **relative** improvements, never absolute req/s implying production infrastructure. Quote p99 from the soak run only — short runs have too few samples for a stable p99.

---

## 12. Phases

| Phase | Deliverable |
|---|---|
| **0** | HTTP ingress + JSON codec + one mock bidder + gRPC unary call. No concurrency, no timeouts. Prove the wire path end-to-end. |
| **1** | Fan-out/fan-in with fixed semaphore, buffered channel, linear winner selection. No timeouts yet. |
| **2** | Context deadlines, per-bidder sub-contexts, partial results, shed rule, full budget breakdown, error taxonomy. |
| **3** | Prometheus + structured logs + Grafana. Must be live before load testing. |
| **4** | k6 harness, four scenarios, empirical correction of ceiling / buckets / budget split. |
| **5** | Stretch: dynamic semaphore sizing, multi-item auctions, bidder health registry + circuit breaker — **only if Phase 4 surfaces evidence justifying each.** |
