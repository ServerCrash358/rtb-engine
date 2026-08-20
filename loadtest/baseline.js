// Baseline: steady load well below the semaphore ceiling. Establishes
// normal-operation p50/p95/p99 latency at ~0 shed rate.
//
// Run: k6 run loadtest/baseline.js
// Override: k6 run -e RATE=30 -e DURATION=2m loadtest/baseline.js
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';
import { postAuction } from './common.js';

const shedRate = new Rate('shed_rate');

const RATE = Number(__ENV.RATE || 20); // req/s, well under semaphore_ceiling / bidders_per_request
const DURATION = __ENV.DURATION || '1m';

export const options = {
  scenarios: {
    baseline: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: RATE * 2,
      maxVUs: RATE * 4,
    },
  },
  thresholds: {
    // Baseline should never shed. If it does, the ceiling is set too low
    // relative to real concurrent load — the exact "correct empirically"
    // signal Phase 4 is looking for.
    shed_rate: ['rate<0.01'],
    'http_req_duration': ['p(95)<300'],
  },
};

export default function () {
  const res = postAuction();
  shedRate.add(res.status === 429);
  check(res, {
    'status is 200, 204, or 429': (r) => [200, 204, 429].includes(r.status),
  });
  sleep(0.01);
}
