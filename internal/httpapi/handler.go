// Package httpapi is the HTTP ingress: JSON codec, validation, deadline
// admission, and the error taxonomy from the build spec.
package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/ServerCrash358/rtb-engine/internal/auction"
	"github.com/ServerCrash358/rtb-engine/internal/budget"
	rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"
)

type Handler struct {
	Bidders []auction.Caller
	Sem     *semaphore.Weighted
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	deadline := start.Add(budget.TotalBudget)
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var reqJSON bidRequestJSON
	if err := json.NewDecoder(r.Body).Decode(&reqJSON); err != nil {
		http.Error(w, "malformed JSON", http.StatusBadRequest)
		return
	}

	if reqJSON.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if len(reqJSON.Items) != 1 {
		http.Error(w, "exactly one item required", http.StatusBadRequest)
		return
	}
	item := reqJSON.Items[0]
	if item.BidFloorCur != "" && item.BidFloorCur != "USD" {
		http.Error(w, "only USD supported", http.StatusBadRequest)
		return
	}

	// Request-level admission: is there enough time left to even attempt
	// dispatch? This is independent of the per-bidder semaphore check,
	// which happens inside auction.Dispatch.
	if remaining := time.Until(deadline); remaining < budget.MinViableBidderWindow {
		h.logOutcome(reqJSON.ID, start, auction.Stats{}, nil, "shed")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	protoReq := &rtbv1.BidRequest{
		Id:   reqJSON.ID,
		Tmax: reqJSON.Tmax,
		Items: []*rtbv1.Item{
			{
				Id:          item.ID,
				BidFloor:    item.BidFloor,
				BidFloorCur: "USD",
			},
		},
	}

	winner, ok, stats := auction.Dispatch(ctx, h.Sem, h.Bidders, protoReq)

	if ok {
		h.logOutcome(reqJSON.ID, start, stats, winner, "completed")
		respJSON := toJSON(reqJSON.ID, winner)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(respJSON)
		return
	}

	// No winner. Distinguish "every bidder call was shed" (429 — the
	// engine never really tried) from a genuine empty/degraded auction
	// (204 — bidders were reached, none qualified).
	if stats.Eligible > 0 && stats.Shed == stats.Eligible {
		h.logOutcome(reqJSON.ID, start, stats, nil, "shed")
		w.WriteHeader(http.StatusTooManyRequests)
		return
	}

	outcome := "no_bid"
	if stats.Timeout > 0 {
		outcome = "timed_out_partial"
	}
	h.logOutcome(reqJSON.ID, start, stats, nil, outcome)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) logOutcome(reqID string, start time.Time, stats auction.Stats, winner *rtbv1.Bid, outcome string) {
	seat, price := "", 0.0
	if winner != nil {
		seat, price = winner.GetSeatId(), winner.GetPrice()
	}
	log.Printf(
		"request_id=%s eligible_bidder_count=%d responded_bidder_count=%d winner_seat_id=%s winner_price=%v total_latency_ms=%d outcome=%s",
		reqID, stats.Eligible, stats.Responded, seat, price, time.Since(start).Milliseconds(), outcome,
	)
}

func toJSON(reqID string, winner *rtbv1.Bid) bidResponseJSON {
	return bidResponseJSON{
		ID: reqID,
		Seatbid: []seatbidJSON{
			{
				Seat: winner.GetSeatId(),
				Bid: []bidJSON{
					{
						ID:     winner.GetId(),
						ItemID: winner.GetItemId(),
						Price:  winner.GetPrice(),
						Cur:    winner.GetCur(),
						SeatID: winner.GetSeatId(),
					},
				},
			},
		},
	}
}
