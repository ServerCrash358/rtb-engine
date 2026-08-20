package auction

import (
	"context"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sync/semaphore"

	rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"
)

// fakeBidder is a deterministic, channel-controlled stand-in for a real
// gRPC bidder, per the build spec's testing guidance.
type fakeBidder struct {
	respondAfter time.Duration // wait this long, then respond
	block        bool          // wait forever (until ctx is done) instead
	price        float64
	seat         string
	err          error
}

func (f *fakeBidder) SeatID() string {
	return f.seat
}

func (f *fakeBidder) GetBid(ctx context.Context, req *rtbv1.BidRequest) (*rtbv1.BidResponse, error) {
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.respondAfter > 0 {
		select {
		case <-time.After(f.respondAfter):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return &rtbv1.BidResponse{
		Id: req.GetId(),
		Seatbid: []*rtbv1.Seatbid{
			{Seat: f.seat, Bid: []*rtbv1.Bid{{Price: f.price, SeatId: f.seat}}},
		},
	}, nil
}

func newSem() *semaphore.Weighted {
	return semaphore.NewWeighted(256)
}

func TestDispatch_PartialResults_SlowBidderExcluded(t *testing.T) {
	// Bidder A responds well within the window; bidder B never responds at
	// all. The overall ctx expires before B ever would, so we must collect
	// exactly A's result and move on — not block waiting for B.
	fast := &fakeBidder{respondAfter: 20 * time.Millisecond, price: 3.0, seat: "fast"}
	slow := &fakeBidder{block: true}

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	before := runtime.NumGoroutine()

	winner, ok, stats := Dispatch(ctx, newSem(), []Caller{fast, slow}, &rtbv1.BidRequest{
		Id:    "req-1",
		Items: []*rtbv1.Item{{Id: "item-1"}},
	})

	if !ok {
		t.Fatal("expected a winner")
	}
	if winner.GetSeatId() != "fast" {
		t.Fatalf("expected fast bidder to win, got seat %q", winner.GetSeatId())
	}
	if stats.Responded != 1 {
		t.Fatalf("expected exactly 1 response collected, got %d", stats.Responded)
	}

	// The blocked goroutine only exits once its ctx.Done() fires, which
	// happens at the outer deadline. Give it a moment to unwind, then
	// verify no leak.
	waitForGoroutineBaseline(t, before, 500*time.Millisecond)
}

func TestDispatch_AllShed_WhenSemaphoreExhausted(t *testing.T) {
	sem := semaphore.NewWeighted(1)
	sem.TryAcquire(1) // exhaust the only slot before dispatch even starts
	defer sem.Release(1)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	a := &fakeBidder{price: 5.0, seat: "a"}
	b := &fakeBidder{price: 5.0, seat: "b"}

	winner, ok, stats := Dispatch(ctx, sem, []Caller{a, b}, &rtbv1.BidRequest{
		Id:    "req-2",
		Items: []*rtbv1.Item{{Id: "item-1"}},
	})

	if ok {
		t.Fatalf("expected no winner, got %v", winner)
	}
	if stats.Shed != 2 {
		t.Fatalf("expected both bidders shed, got shed=%d", stats.Shed)
	}
	if stats.Responded != 0 {
		t.Fatalf("expected 0 responses, got %d", stats.Responded)
	}
}

func TestDispatch_TimeoutClassifiedSeparatelyFromResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	tooSlow := &fakeBidder{respondAfter: 200 * time.Millisecond, price: 9.0, seat: "toolate"}

	winner, ok, stats := Dispatch(ctx, newSem(), []Caller{tooSlow}, &rtbv1.BidRequest{
		Id:    "req-3",
		Items: []*rtbv1.Item{{Id: "item-1"}},
	})

	if ok {
		t.Fatalf("expected no winner from a bidder that never responds in time, got %v", winner)
	}
	if stats.Responded != 0 {
		t.Fatalf("expected 0 responses, got %d", stats.Responded)
	}
}

func TestDispatch_NoGoroutineLeakUnderBurst(t *testing.T) {
	runtime.GC()
	before := runtime.NumGoroutine()

	for i := 0; i < 200; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		blocked := &fakeBidder{block: true}
		responsive := &fakeBidder{respondAfter: 2 * time.Millisecond, price: 1.0, seat: "r"}
		Dispatch(ctx, newSem(), []Caller{blocked, responsive}, &rtbv1.BidRequest{
			Id:    "req-burst",
			Items: []*rtbv1.Item{{Id: "item-1"}},
		})
		cancel()
	}

	waitForGoroutineBaseline(t, before, 2*time.Second)
}

func waitForGoroutineBaseline(t *testing.T, before int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		runtime.GC()
		if after := runtime.NumGoroutine(); after <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine count did not return to baseline %d, still at %d", before, runtime.NumGoroutine())
}
