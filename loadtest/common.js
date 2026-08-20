// Shared request-building helpers for all four load scenarios.
import http from 'k6/http';

export const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

let seq = 0;

export function auctionPayload() {
  seq += 1;
  return JSON.stringify({
    id: `k6-${__VU}-${__ITER}-${seq}-${Date.now()}`,
    items: [{ id: 'item-1', bid_floor: 1.0, bid_floor_cur: 'USD' }],
    tmax: 300,
  });
}

export function postAuction() {
  return http.post(`${BASE_URL}/openrtb2/auction`, auctionPayload(), {
    headers: { 'Content-Type': 'application/json' },
    // Give the HTTP client more room than the engine's own 300ms budget so
    // k6 never times out the request before the engine responds (429/204
    // included) — we want to measure the engine's behavior, not k6's.
    timeout: '2s',
  });
}
