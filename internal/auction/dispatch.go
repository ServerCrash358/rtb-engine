// Package auction implements fan-out to eligible bidders, fan-in of their
// responses, and winner selection.
package auction

import (
	"context"

	"golang.org/x/sync/semaphore"

	"github.com/ServerCrash358/rtb-engine/internal/bidder"
	rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"
)

// dispatchResult is what one bidder goroutine sends back, tagged with its
// dispatch-position index so fan-in can place it without a map.
type dispatchResult struct {
	idx int
	bid *rtbv1.Bid // nil = no qualifying bid (no-bid, error, or sem full)
}

// Dispatch fans a bid request out to every bidder, fans the responses back
// in over a buffered channel, and returns the winning bid.
//
// No deadlines yet (Phase 2): each bidder call runs on ctx as given. The
// semaphore only bounds in-flight RPC count; a bidder that can't acquire it
// is treated as a no-bid rather than causing a request-level shed, since
// shedding is defined against a remaining-deadline check that doesn't exist
// until Phase 2.
func Dispatch(ctx context.Context, sem *semaphore.Weighted, bidders []*bidder.Client, req *rtbv1.BidRequest) (*rtbv1.Bid, bool) {
	n := len(bidders)
	if n == 0 {
		return nil, false
	}

	resultCh := make(chan dispatchResult, n)

	for i, b := range bidders {
		go dispatchOne(ctx, sem, i, b, req, resultCh)
	}

	// Pre-sized slice indexed by dispatch position; recvOrder is recorded
	// separately as each response actually arrives, for tie-breaking.
	candidates := make([]candidate, n)
	for i := 0; i < n; i++ {
		candidates[i].RecvOrder = -1
	}

	for count := 0; count < n; count++ {
		r := <-resultCh
		candidates[r.idx] = candidate{Bid: r.bid, RecvOrder: count}
	}

	floor := floorFor(req)
	return selectWinner(candidates, floor)
}

func dispatchOne(ctx context.Context, sem *semaphore.Weighted, idx int, b *bidder.Client, req *rtbv1.BidRequest, out chan<- dispatchResult) {
	if !sem.TryAcquire(1) {
		out <- dispatchResult{idx: idx}
		return
	}
	defer sem.Release(1)

	resp, err := b.GetBid(ctx, req)
	if err != nil {
		out <- dispatchResult{idx: idx}
		return
	}

	seatbids := resp.GetSeatbid()
	if len(seatbids) == 0 || len(seatbids[0].GetBid()) == 0 {
		out <- dispatchResult{idx: idx}
		return
	}

	out <- dispatchResult{idx: idx, bid: seatbids[0].GetBid()[0]}
}

func floorFor(req *rtbv1.BidRequest) float64 {
	if len(req.GetItems()) == 0 {
		return 0
	}
	return req.GetItems()[0].GetBidFloor()
}
