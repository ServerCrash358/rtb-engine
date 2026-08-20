// Package httpapi is the HTTP ingress: JSON codec, validation, and the
// error taxonomy from the build spec. Phase 1 fans a request out to all
// registered bidders and returns the winner — no deadlines, no shedding
// yet.
package httpapi

import (
	"encoding/json"
	"net/http"

	"golang.org/x/sync/semaphore"

	"github.com/ServerCrash358/rtb-engine/internal/auction"
	"github.com/ServerCrash358/rtb-engine/internal/bidder"
	rtbv1 "github.com/ServerCrash358/rtb-engine/proto/rtbv1"
)

type Handler struct {
	Bidders []*bidder.Client
	Sem     *semaphore.Weighted
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

	winner, ok := auction.Dispatch(r.Context(), h.Sem, h.Bidders, protoReq)
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	respJSON := toJSON(reqJSON.ID, winner)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(respJSON)
}

func toJSON(reqID string, winner *rtbv1.Bid) bidResponseJSON {
	return bidResponseJSON{
		ID: reqID,
		Seatbid: []seatbidJSON{
			{
				Seat: winner.GetSeatId(),
				Bid: []bidJSON{
					{
						ID:     winner.GetId(),
						ItemID: winner.GetItemId(),
						Price:  winner.GetPrice(),
						Cur:    winner.GetCur(),
						SeatID: winner.GetSeatId(),
					},
				},
			},
		},
	}
}
