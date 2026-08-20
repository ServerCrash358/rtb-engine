// Command engine is the RTB exchange's HTTP ingress server.
// Phase 0: wires ingress to a single bidder over gRPC. No fan-out, no
// concurrency control, no timeouts — proving the wire path end-to-end.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/ServerCrash358/rtb-engine/internal/bidder"
	"github.com/ServerCrash358/rtb-engine/internal/config"
	"github.com/ServerCrash358/rtb-engine/internal/httpapi"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("engine: %v", err)
	}
	if len(cfg.Bidders) == 0 {
		log.Fatalf("engine: config must list at least one bidder")
	}

	// Phase 0 dials only the first configured bidder; fan-out to all
	// eligible bidders arrives in Phase 1.
	b := cfg.Bidders[0]
	client, err := bidder.Dial(b.SeatID, b.Endpoint)
	if err != nil {
		log.Fatalf("engine: %v", err)
	}
	defer client.Close()

	handler := &httpapi.Handler{Bidder: client}
	mux := http.NewServeMux()
	mux.Handle("/openrtb2/auction", handler)

	log.Printf("engine listening on %s, bidder=%s@%s", cfg.ListenAddr, b.SeatID, b.Endpoint)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("engine: serve: %v", err)
	}
}
