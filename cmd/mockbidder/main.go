// Command mockbidder is a gRPC server implementing the rtb.v1.Bidder service.
// Phase 0: always returns a single fixed-price bid. Delay/error/timeout/nobid
// injection lands in a later phase.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"

	rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"
)

type server struct {
	rtbv1.UnimplementedBidderServer
	seatID string
}

func (s *server) GetBid(ctx context.Context, req *rtbv1.BidRequest) (*rtbv1.BidResponse, error) {
	if len(req.GetItems()) == 0 {
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

func main() {
	listenAddr := flag.String("listen", ":50051", "listen address")
	seatID := flag.String("seat-id", "bidder-a", "seat id returned in bids")
	flag.Parse()

	lis, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("mockbidder: listen on %s: %v", *listenAddr, err)
	}

	grpcServer := grpc.NewServer()
	rtbv1.RegisterBidderServer(grpcServer, &server{seatID: *seatID})

	fmt.Printf("mockbidder[%s] listening on %s\n", *seatID, *listenAddr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("mockbidder: serve: %v", err)
	}
}
