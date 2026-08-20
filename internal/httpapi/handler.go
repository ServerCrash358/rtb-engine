// Package httpapi is the HTTP ingress: JSON codec, validation, and the
// error taxonomy from the build spec. Phase 0 calls a single bidder
// directly — no fan-out, no deadlines, no shedding yet.
package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/ServerCrash358/rtb-engine/internal/bidder"
	rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"
)

type Handler struct {
	Bidder *bidder.Client
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var reqJSON bidRequestJSON
	if err := json.NewDecoder(r.Body).Decode(&reqJSON); err != nil {
		http.Error(w, "malformed JSON", http.StatusBadRequest)
		return
	}

	if reqJSON.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if len(reqJSON.Items) != 1 {
		http.Error(w, "exactly one item required", http.StatusBadRequest)
		return
	}
	item := reqJSON.Items[0]
	if item.BidFloorCur != "" && item.BidFloorCur != "USD" {
		http.Error(w, "only USD supported", http.StatusBadRequest)
		return
	}

	protoReq := &rtbv1.BidRequest{
		Id:   reqJSON.ID,
		Tmax: reqJSON.Tmax,
		Items: []*rtbv1.Item{
			{
				Id:          item.ID,
				BidFloor:    item.BidFloor,
				BidFloorCur: "USD",
			},
		},
	}

	resp, err := h.Bidder.GetBid(context.Background(), protoReq)
	if err != nil {
		log.Printf("request_id=%s bidder=%s error=%v", reqJSON.ID, h.Bidder.SeatID, err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if len(resp.GetSeatbid()) == 0 || len(resp.GetSeatbid()[0].GetBid()) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	respJSON := toJSON(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(respJSON)
}

func toJSON(resp *rtbv1.BidResponse) bidResponseJSON {
	out := bidResponseJSON{ID: resp.GetId()}
	for _, sb := range resp.GetSeatbid() {
		sbJSON := seatbidJSON{Seat: sb.GetSeat()}
		for _, b := range sb.GetBid() {
			sbJSON.Bid = append(sbJSON.Bid, bidJSON{
				ID:     b.GetId(),
				ItemID: b.GetItemId(),
				Price:  b.GetPrice(),
				Cur:    b.GetCur(),
				SeatID: b.GetSeatId(),
			})
		}
		out.Seatbid = append(out.Seatbid, sbJSON)
	}
	return out
}
