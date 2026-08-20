// Command engine is the RTB exchange's HTTP ingress server.
// Phase 1: fans each request out to every registered bidder under a fixed
// semaphore, fans responses back in, and returns the winner. Still no
// deadlines or shedding — that's Phase 2.
package main

import (
	"flag"
	"log"
	"net/http"

	"golang.org/x/sync/semaphore"

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

	// Dialed once at startup, reused for the process lifetime.
	clients := make([]*bidder.Client, 0, len(cfg.Bidders))
	for _, b := range cfg.Bidders {
		client, err := bidder.Dial(b.SeatID, b.Endpoint)
		if err != nil {
			log.Fatalf("engine: %v", err)
		}
		defer client.Close()
		clients = append(clients, client)
	}

	sem := semaphore.NewWeighted(int64(cfg.SemaphoreCeiling))

	handler := &httpapi.Handler{Bidders: clients, Sem: sem}
	mux := http.NewServeMux()
	mux.Handle("/openrtb2/auction", handler)

	log.Printf("engine listening on %s, bidders=%d, semaphore_ceiling=%d", cfg.ListenAddr, len(clients), cfg.SemaphoreCeiling)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("engine: serve: %v", err)
	}
}
