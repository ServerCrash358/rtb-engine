// Soak: baseline load sustained for 30+ minutes. Validates no goroutine
// leak by watching the rtb_goroutines gauge trend over the run (via
// Prometheus/Grafana, not k6 itself — k6 only generates the load).
//
// Run: k6 run loadtest/soak.js                      (30m default)
// Shorter dev run: k6 run -e DURATION=2m loadtest/soak.js
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';
import { postAuction } from './common.js';

const shedRate = new Rate('shed_rate');

const RATE = Number(__ENV.RATE || 20); // same profile as baseline, just longer
const DURATION = __ENV.DURATION || '30m';

export const options = {
  scenarios: {
    soak: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: RATE * 2,
      maxVUs: RATE * 4,
    },
  },
  thresholds: {
    shed_rate: ['rate<0.01'],
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
