// Package metrics defines the Prometheus collectors for the engine.
package metrics

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Dense below 300ms — that's where all interesting variation lives. Wrong
// boundaries produce a silently misleading p99, not an obvious error, so
// re-check against real distributions once load tests exist (Phase 4).
var latencyBucketsMS = []float64{5, 10, 25, 50, 75, 100, 150, 200, 250, 300, 500}

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "rtb_requests_total",
		Help: "Total auction requests by outcome.",
	}, []string{"outcome"})

	RequestDurationMS = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "rtb_request_duration_ms",
		Help:    "End-to-end auction request latency in milliseconds.",
		Buckets: latencyBucketsMS,
	})

	BidderDurationMS = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "rtb_bidder_duration_ms",
		Help:    "Per-bidder RPC latency in milliseconds.",
		Buckets: latencyBucketsMS,
	}, []string{"seat"})

	WinnerSelectionDurationMS = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "rtb_winner_selection_duration_ms",
		Help:    "Time spent selecting a winner from collected bids, in milliseconds.",
		Buckets: latencyBucketsMS,
	})

	BidderTimeoutsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "rtb_bidder_timeouts_total",
		Help: "Bidder calls that exceeded the per-bidder deadline.",
	}, []string{"seat"})

	BidderErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "rtb_bidder_errors_total",
		Help: "Bidder calls that failed for a non-timeout reason.",
	}, []string{"seat"})

	ShedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "rtb_shed_total",
		Help: "Dispatch attempts shed before completion, by reason.",
	}, []string{"reason"})

	SemaphoreInflight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rtb_semaphore_inflight",
		Help: "Current count of in-flight bidder RPCs bounded by the semaphore.",
	})
)

func init() {
	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "rtb_goroutines",
		Help: "Current goroutine count (runtime.NumGoroutine).",
	}, func() float64 {
		return float64(runtime.NumGoroutine())
	})
}
