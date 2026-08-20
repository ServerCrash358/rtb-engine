// Spike: burst well above the semaphore ceiling. Exercises the shed path
// and produces the degradation evidence — the throughput ceiling is where
// req/s increases stop yielding completed auctions and only raise the
// shed rate.
//
// Run: k6 run loadtest/spike.js
import { check } from 'k6';
import { Rate } from 'k6/metrics';
import { postAuction } from './common.js';

const shedRate = new Rate('shed_rate');

const BASE_RATE = Number(__ENV.BASE_RATE || 20);
const SPIKE_RATE = Number(__ENV.SPIKE_RATE || 400); // deliberately above capacity

export const options = {
  scenarios: {
    spike: {
      executor: 'ramping-arrival-rate',
      startRate: BASE_RATE,
      timeUnit: '1s',
      preAllocatedVUs: SPIKE_RATE,
      maxVUs: SPIKE_RATE * 2,
      stages: [
        { target: BASE_RATE, duration: '10s' },  // establish baseline
        { target: SPIKE_RATE, duration: '5s' },  // sudden burst
        { target: SPIKE_RATE, duration: '20s' }, // hold above capacity
        { target: BASE_RATE, duration: '10s' },  // recovery
      ],
    },
  },
  // No thresholds here — shedding under spike is the expected, correct
  // behavior, not a failure.
};

export default function () {
  const res = postAuction();
  shedRate.add(res.status === 429);
  check(res, {
    'status is 200, 204, or 429': (r) => [200, 204, 429].includes(r.status),
  });
}
