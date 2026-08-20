// Package auction implements fan-out to eligible bidders, fan-in of their
// responses, and winner selection.
package auction

import (
	"context"
	"errors"

	"golang.org/x/sync/semaphore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ServerCrash358/rtb-engine/internal/budget"
	rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"
)

// Caller is the subset of *bidder.Client that Dispatch needs. Satisfied
// implicitly by *bidder.Client; tests substitute channel-controlled fakes.
type Caller interface {
	GetBid(ctx context.Context, req *rtbv1.BidRequest) (*rtbv1.BidResponse, error)
}

// outcome classifies what happened to one bidder's dispatch attempt.
type outcome int

const (
	outcomeBid outcome = iota
	outcomeNoBid
	outcomeTimeout
	outcomeError
	outcomeShed // semaphore full; the request-level deadline_below_floor shed
	// happens before Dispatch is even called, so every per-bidder shed here
	// is reason=semaphore_full.
)

// Stats summarizes what happened across all eligible bidders, for the
// error-taxonomy decision in httpapi and for structured logging.
type Stats struct {
	Eligible  int
	Responded int // bid or explicit no-bid received within budget
	Shed      int // skipped: semaphore full
	Timeout   int // dispatched but exceeded the per-bidder deadline
	Error     int // dispatched but the RPC failed for a non-timeout reason
}

// dispatchResult is what one bidder goroutine sends back, tagged with its
// dispatch-position index so fan-in can place it without a map.
type dispatchResult struct {
	idx     int
	bid     *rtbv1.Bid // nil unless outcome == outcomeBid
	outcome outcome
}

// Dispatch fans a bid request out to every bidder under ctx's deadline,
// fans the responses back in over a buffered channel, and returns the
// winning bid plus per-outcome stats.
//
// Callers are expected to have already applied the request-level shed
// check (remaining time vs budget.MinViableBidderWindow) before calling
// Dispatch — that's an admission decision for the whole request, not a
// per-bidder one.
func Dispatch(ctx context.Context, sem *semaphore.Weighted, bidders []Caller, req *rtbv1.BidRequest) (*rtbv1.Bid, bool, Stats) {
	n := len(bidders)
	stats := Stats{Eligible: n}
	if n == 0 {
		return nil, false, stats
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

collect:
	for count := 0; count < n; {
		select {
		case r := <-resultCh:
			candidates[r.idx] = candidate{Bid: r.bid, RecvOrder: count}
			count++
			switch r.outcome {
			case outcomeBid, outcomeNoBid:
				stats.Responded++
			case outcomeShed:
				stats.Shed++
			case outcomeTimeout:
				stats.Timeout++
			case outcomeError:
				stats.Error++
			}
		case <-ctx.Done():
			// Deadline hit before every bidder responded. Whatever slots
			// are still zero-value are simply excluded by selectWinner —
			// this is the "partial results" path.
			break collect
		}
	}

	floor := floorFor(req)
	winner, ok := selectWinner(candidates, floor)
	return winner, ok, stats
}

func dispatchOne(ctx context.Context, sem *semaphore.Weighted, idx int, b Caller, req *rtbv1.BidRequest, out chan<- dispatchResult) {
	if !sem.TryAcquire(1) {
		out <- dispatchResult{idx: idx, outcome: outcomeShed}
		return
	}
	defer sem.Release(1)

	subCtx, cancel := context.WithTimeout(ctx, budget.BidderBudget)
	defer cancel()

	resp, err := b.GetBid(subCtx, req)
	if err != nil {
		if isDeadlineExceeded(subCtx, err) {
			out <- dispatchResult{idx: idx, outcome: outcomeTimeout}
			return
		}
		out <- dispatchResult{idx: idx, outcome: outcomeError}
		return
	}

	seatbids := resp.GetSeatbid()
	if len(seatbids) == 0 || len(seatbids[0].GetBid()) == 0 {
		out <- dispatchResult{idx: idx, outcome: outcomeNoBid}
		return
	}

	out <- dispatchResult{idx: idx, bid: seatbids[0].GetBid()[0], outcome: outcomeBid}
}

func isDeadlineExceeded(ctx context.Context, err error) bool {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	return status.Code(err) == codes.DeadlineExceeded
}

func floorFor(req *rtbv1.BidRequest) float64 {
	if len(req.GetItems()) == 0 {
		return 0
	}
	return req.GetItems()[0].GetBidFloor()
}
