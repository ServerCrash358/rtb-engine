package auction

import rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"

// candidate pairs a bidder's bid with the order it was received in during
// fan-in. Ties break on recvOrder, not price alone.
type candidate struct {
	Bid       *rtbv1.Bid
	RecvOrder int
}

// selectWinner does a single linear pass over the candidates. No heap, no
// map: N is small enough (~5-50) that a scan beats heap-build + extract.
// Bids under floor are discarded engine-side. Ties go to the earliest
// response. Zero qualifying bids returns (nil, false).
func selectWinner(candidates []candidate, floor float64) (*rtbv1.Bid, bool) {
	var best *rtbv1.Bid
	bestRecv := -1

	for _, c := range candidates {
		if c.Bid == nil {
			continue
		}
		if c.Bid.GetPrice() < floor {
			continue
		}
		if best == nil ||
			c.Bid.GetPrice() > best.GetPrice() ||
			(c.Bid.GetPrice() == best.GetPrice() && c.RecvOrder < bestRecv) {
			best = c.Bid
			bestRecv = c.RecvOrder
		}
	}

	if best == nil {
		return nil, false
	}
	return best, true
}
