package httpapi

// JSON wire types for the HTTP ingress. Mirror the protobuf schema in
// proto/rtbv1/rtb.proto field-for-field.

type itemJSON struct {
	ID          string  `json:"id"`
	BidFloor    float64 `json:"bid_floor"`
	BidFloorCur string  `json:"bid_floor_cur"`
}

type bidRequestJSON struct {
	ID    string     `json:"id"`
	Items []itemJSON `json:"items"`
	Tmax  int32      `json:"tmax"`
}

type bidJSON struct {
	ID     string  `json:"id"`
	ItemID string  `json:"item_id"`
	Price  float64 `json:"price"`
	Cur    string  `json:"cur"`
	SeatID string  `json:"seat_id"`
}

type seatbidJSON struct {
	Seat string    `json:"seat"`
	Bid  []bidJSON `json:"bid"`
}

type bidResponseJSON struct {
	ID      string        `json:"id"`
	Seatbid []seatbidJSON `json:"seatbid"`
}
