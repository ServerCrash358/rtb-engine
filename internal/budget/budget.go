// Package budget defines the fixed time budgets that govern one auction
// request, per the build spec. These are constants, not config — do not
// make them tunable without re-deriving the sum invariant below.
package budget

import "time"

const (
	TotalBudget           = 300 * time.Millisecond // tmax
	IngressBudget         = 20 * time.Millisecond  // parse, validate, filter
	BidderBudget          = 245 * time.Millisecond // per-bidder context deadline
	ResponsePrepBudget    = 25 * time.Millisecond  // select winner, serialize
	JitterSlack           = 10 * time.Millisecond
	MinViableBidderWindow = 60 * time.Millisecond // shed floor; independent of BidderBudget
)
