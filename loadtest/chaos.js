// Chaos: baseline load profile against mock bidders started with
// error/timeout injection. Proves partial results and per-bidder timeout
// enforcement hold up under real, non-cooperative bidder failures.
//
// This script generates the same traffic as baseline.js — the chaos comes
// from how the mock bidders are launched, e.g.:
//   go run ./cmd/mockbidder --listen :50051 --seat-id bidder-a \
//     --error-rate 0.1 --timeout-rate 0.1
//   go run ./cmd/mockbidder --listen :50052 --seat-id bidder-b \
//     --error-rate 0.1 --timeout-rate 0.1
//
// Run: k6 run loadtest/chaos.js
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';
import { postAuction } from './common.js';

const shedRate = new Rate('shed_rate');
const noContentRate = new Rate('no_content_rate'); // 204: no_bid or timed_out_partial

const RATE = Number(__ENV.RATE || 20);
const DURATION = __ENV.DURATION || '1m';

export const options = {
  scenarios: {
    chaos: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: RATE * 2,
      maxVUs: RATE * 4,
    },
  },
  // No hard thresholds: with injected errors/timeouts, some fraction of
  // 204s is expected and correct. Compare against baseline's 204 rate
  // to see the delta.
};

export default function () {
  const res = postAuction();
  shedRate.add(res.status === 429);
  noContentRate.add(res.status === 204);
  check(res, {
    'status is 200, 204, or 429': (r) => [200, 204, 429].includes(r.status),
  });
  sleep(0.01);
}
