// Volume: a fixed total number of requests spread across a VU pool, as
// fast as the engine can take them. Not one of the spec's four named
// scenarios -- a supplementary large-N run to see how the engine holds
// up over hundreds of thousands / millions of requests, not just over a
// fixed wall-clock window.
//
// Run: k6 run -e ITERATIONS=100000 -e VUS=200 loadtest/volume.js
import { check } from 'k6';
import { Rate } from 'k6/metrics';
import { postAuction } from './common.js';

const shedRate = new Rate('shed_rate');
const noContentRate = new Rate('no_content_rate');

const ITERATIONS = Number(__ENV.ITERATIONS || 100000);
const VUS = Number(__ENV.VUS || 200);

export const options = {
  scenarios: {
    volume: {
      executor: 'shared-iterations',
      vus: VUS,
      iterations: ITERATIONS,
      maxDuration: __ENV.MAX_DURATION || '30m',
    },
  },
};

export default function () {
  const res = postAuction();
  shedRate.add(res.status === 429);
  noContentRate.add(res.status === 204);
  check(res, {
    'status is 200, 204, or 429': (r) => [200, 204, 429].includes(r.status),
  });
}
