package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amr-as90/jellyfin-recommendations/recommender"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[jellyfin-recs] ")

	cfg, err := recommender.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	log.Printf("Starting jellyfin-recommendations")
	log.Printf("  Server: %s", cfg.ServerURL)
	log.Printf("  Sync interval: %s", cfg.SyncInterval)
	log.Printf("  Collection prefix: %q", cfg.CollectionPrefix)
	log.Printf("  Dry run: %v", cfg.DryRun)
	log.Printf("  Excluded users: %v", cfg.ExcludeUsers)
	log.Printf("  Debug: %v", cfg.Debug)

	state := recommender.NewStateManager(cfg)

	// Graceful shutdown context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received %v, shutting down...", sig)
		cancel()
	}()

	// Initial sync
	if err := state.Sync(ctx); err != nil {
		log.Fatalf("Initial sync failed: %v", err)
	}
	log.Println("Initial sync complete")

	// Periodic sync ticker
	ticker := time.NewTicker(cfg.SyncInterval)
	defer ticker.Stop()

	log.Printf("Listening for changes (sync every %s)", cfg.SyncInterval)

	for {
		select {
		case <-ctx.Done():
			log.Println("Shutting down gracefully")
			return
		case <-ticker.C:
			if err := state.Sync(ctx); err != nil {
				log.Printf("Sync error: %v", err)
			} else {
				log.Println("Sync complete")
			}
		}
	}
}
