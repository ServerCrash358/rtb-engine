// Package bidder manages gRPC connections to registered bidders.
// Phase 0: a single connection, dialed once and reused, no pooling logic yet.
package bidder

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"
)

// Client wraps one bidder's ClientConn. Created at startup, reused for the
// process lifetime — never dialed per-request.
type Client struct {
	SeatID string
	conn   *grpc.ClientConn
	stub   rtbv1.BidderClient
}

func Dial(seatID, endpoint string) (*Client, error) {
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("bidder: dial %s (%s): %w", seatID, endpoint, err)
	}
	return &Client{
		SeatID: seatID,
		conn:   conn,
		stub:   rtbv1.NewBidderClient(conn),
	}, nil
}

func (c *Client) GetBid(ctx context.Context, req *rtbv1.BidRequest) (*rtbv1.BidResponse, error) {
	return c.stub.GetBid(ctx, req)
}

func (c *Client) Close() error {
	return c.conn.Close()
}
