// Command mockbidder is a gRPC server implementing the rtb.v1.Bidder
// service, with delay/error/timeout/no-bid injection for exercising the
// engine's deadline and shed-rule handling.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ServerCrash358/rtb-engine/internal/budget"
	rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"
)

type server struct {
	rtbv1.UnimplementedBidderServer
	seatID      string
	delayMean   float64 // ms
	delayStddev float64 // ms
	errorRate   float64
	timeoutRate float64
	nobidRate   float64
}

func (s *server) GetBid(ctx context.Context, req *rtbv1.BidRequest) (*rtbv1.BidResponse, error) {
	delay := lognormalDelay(s.delayMean, s.delayStddev)
	if rand.Float64() < s.timeoutRate {
		// Deliberately exceed the engine's per-bidder deadline.
		delay = budget.BidderBudget + 100*time.Millisecond
	}

	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if rand.Float64() < s.errorRate {
		return nil, status.Error(codes.Internal, "mockbidder: injected error")
	}

	if len(req.GetItems()) == 0 || rand.Float64() < s.nobidRate {
		return &rtbv1.BidResponse{Id: req.GetId()}, nil
	}
	item := req.GetItems()[0]

	bid := &rtbv1.Bid{
		Id:     req.GetId() + "-bid",
		ItemId: item.GetId(),
		Price:  item.GetBidFloor() + 1.0,
		Cur:    "USD",
		SeatId: s.seatID,
	}

	return &rtbv1.BidResponse{
		Id: req.GetId(),
		Seatbid: []*rtbv1.Seatbid{
			{Seat: s.seatID, Bid: []*rtbv1.Bid{bid}},
		},
	}, nil
}

// lognormalDelay draws a duration from a lognormal distribution
// parameterized so its mean and stddev (in ms) match the inputs, rather
// than a fixed constant delay.
func lognormalDelay(meanMS, stddevMS float64) time.Duration {
	if meanMS <= 0 {
		return 0
	}
	if stddevMS <= 0 {
		return time.Duration(meanMS * float64(time.Millisecond))
	}
	variance := stddevMS * stddevMS
	sigma2 := math.Log(1 + variance/(meanMS*meanMS))
	sigma := math.Sqrt(sigma2)
	mu := math.Log(meanMS) - sigma2/2
	ms := math.Exp(rand.NormFloat64()*sigma + mu)
	return time.Duration(ms * float64(time.Millisecond))
}

func main() {
	listenAddr := flag.String("listen", ":50051", "listen address")
	seatID := flag.String("seat-id", "bidder-a", "seat id returned in bids")
	delayMean := flag.Float64("delay-mean", 30, "lognormal response delay mean, ms")
	delayStddev := flag.Float64("delay-stddev", 15, "lognormal response delay stddev, ms")
	errorRate := flag.Float64("error-rate", 0, "fraction of calls returning a gRPC error")
	timeoutRate := flag.Float64("timeout-rate", 0, "fraction of calls deliberately exceeding the bidder budget")
	nobidRate := flag.Float64("nobid-rate", 0, "fraction of calls returning an empty seatbid")
	flag.Parse()

	lis, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("mockbidder: listen on %s: %v", *listenAddr, err)
	}

	grpcServer := grpc.NewServer()
	rtbv1.RegisterBidderServer(grpcServer, &server{
		seatID:      *seatID,
		delayMean:   *delayMean,
		delayStddev: *delayStddev,
		errorRate:   *errorRate,
		timeoutRate: *timeoutRate,
		nobidRate:   *nobidRate,
	})

	fmt.Printf("mockbidder[%s] listening on %s (delay=%.0fms±%.0fms error=%.2f timeout=%.2f nobid=%.2f)\n",
		*seatID, *listenAddr, *delayMean, *delayStddev, *errorRate, *timeoutRate, *nobidRate)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("mockbidder: serve: %v", err)
	}
}
