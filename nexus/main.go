package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	nexus, err := NewNexus(NexusConfig{
		DBPath:    "./nexus.db",
		RelayHost: "wss://relay1.us-east.bsky.network",
	})
	if err != nil {
		log.Fatal(err)
	}

	fhCtx, fhCancel := context.WithCancel(context.Background())
	go func() {
		err := nexus.FirehoseConsumer.Run(fhCtx)
		if err != nil {
			log.Printf("Firehose error: %v", err)
		}
	}()

	go func() {
		if err := nexus.Start(context.Background(), ":8080"); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	fhCancel()

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := nexus.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}

	log.Println("Server stopped")
}
