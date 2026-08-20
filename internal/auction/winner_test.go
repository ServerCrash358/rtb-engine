package auction

import (
	"testing"

	rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"
)

func bid(price float64) *rtbv1.Bid {
	return &rtbv1.Bid{Price: price}
}

func TestSelectWinner(t *testing.T) {
	tests := []struct {
		name       string
		candidates []candidate
		floor      float64
		wantPrice  float64
		wantOK     bool
	}{
		{
			name:       "no bids",
			candidates: nil,
			floor:      0,
			wantOK:     false,
		},
		{
			name:       "one bid above floor",
			candidates: []candidate{{Bid: bid(2.5), RecvOrder: 0}},
			floor:      1.0,
			wantPrice:  2.5,
			wantOK:     true,
		},
		{
			name: "many bids, highest wins",
			candidates: []candidate{
				{Bid: bid(1.0), RecvOrder: 0},
				{Bid: bid(3.0), RecvOrder: 1},
				{Bid: bid(2.0), RecvOrder: 2},
			},
			floor:     0,
			wantPrice: 3.0,
			wantOK:    true,
		},
		{
			name: "all below floor",
			candidates: []candidate{
				{Bid: bid(1.0), RecvOrder: 0},
				{Bid: bid(2.0), RecvOrder: 1},
			},
			floor:  5.0,
			wantOK: false,
		},
		{
			name: "exact tie, earliest response wins",
			candidates: []candidate{
				{Bid: bid(3.0), RecvOrder: 2},
				{Bid: bid(3.0), RecvOrder: 0},
				{Bid: bid(3.0), RecvOrder: 1},
			},
			floor:     0,
			wantPrice: 3.0,
			wantOK:    true,
		},
		{
			name: "nil bids in slice are skipped",
			candidates: []candidate{
				{Bid: nil, RecvOrder: 0},
				{Bid: bid(4.0), RecvOrder: 1},
			},
			floor:     0,
			wantPrice: 4.0,
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := selectWinner(tt.candidates, tt.floor)
			if ok != tt.wantOK {
				t.Fatalf("selectWinner() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got.GetPrice() != tt.wantPrice {
				t.Fatalf("selectWinner() price = %v, want %v", got.GetPrice(), tt.wantPrice)
			}
		})
	}
}

func TestSelectWinnerTieBreaksByRecvOrder(t *testing.T) {
	// Regression: winning bid on a tie must be the one with the lowest
	// RecvOrder, not the last one encountered in the slice.
	candidates := []candidate{
		{Bid: &rtbv1.Bid{Price: 5.0, SeatId: "late"}, RecvOrder: 3},
		{Bid: &rtbv1.Bid{Price: 5.0, SeatId: "early"}, RecvOrder: 1},
	}
	got, ok := selectWinner(candidates, 0)
	if !ok {
		t.Fatal("expected a winner")
	}
	if got.GetSeatId() != "early" {
		t.Fatalf("expected earliest responder to win tie, got seat %q", got.GetSeatId())
	}
}

func FuzzSelectWinner(f *testing.F) {
	f.Add(1.0, 2.0, 3.0, 0.0)
	f.Fuzz(func(t *testing.T, p1, p2, p3, floor float64) {
		candidates := []candidate{
			{Bid: bid(p1), RecvOrder: 0},
			{Bid: bid(p2), RecvOrder: 1},
			{Bid: bid(p3), RecvOrder: 2},
		}
		got, ok := selectWinner(candidates, floor)
		if !ok {
			for _, c := range candidates {
				if c.Bid.GetPrice() >= floor {
					t.Fatalf("selectWinner reported no winner but %v qualifies against floor %v", c.Bid.GetPrice(), floor)
				}
			}
			return
		}
		if got.GetPrice() < floor {
			t.Fatalf("selectWinner returned a bid below floor: %v < %v", got.GetPrice(), floor)
		}
		for _, c := range candidates {
			if c.Bid.GetPrice() > got.GetPrice() && c.Bid.GetPrice() >= floor {
				t.Fatalf("selectWinner missed a higher qualifying bid: %v > %v", c.Bid.GetPrice(), got.GetPrice())
			}
		}
	})
}
