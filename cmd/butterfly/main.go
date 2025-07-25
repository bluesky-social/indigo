package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
	"github.com/bluesky-social/indigo/cmd/butterfly/store"
)

func main() {
	// Command line flags
	var (
		carFile    = flag.String("car", "", "Path to CAR file to read")
		did        = flag.String("did", "", "DID to fetch (required)")
		outputMode = flag.String("output", "stats", "Output mode: stats or passthrough")
		help       = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help || *carFile == "" || *did == "" {
		fmt.Fprintf(os.Stderr, "Usage: butterfly -car <path> -did <did> [-output stats|passthrough]\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Set up logger
	logger := log.New(os.Stderr, "butterfly: ", log.LstdFlags)

	// Create remote
	r := &remote.CarfileRemote{Filepath: *carFile}

	// Create store based on output mode
	var s store.Store
	switch *outputMode {
	case "passthrough":
		s = &store.StdoutStore{Mode: store.StdoutStoreModePassthrough}
	case "stats":
		s = &store.StdoutStore{Mode: store.StdoutStoreModeStats}
	default:
		logger.Fatalf("unknown output mode: %s", *outputMode)
	}

	// Create context
	ctx := context.Background()

	// Initialize store
	if err := s.Setup(ctx); err != nil {
		logger.Fatalf("failed to setup store: %v", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			logger.Printf("failed to close store: %v", err)
		}
	}()

	// Fetch repository
	stream, err := r.FetchRepo(ctx, remote.FetchRepoParams{Did: *did})
	if err != nil {
		logger.Fatalf("failed to fetch repo: %v", err)
	}
	defer stream.Close()

	// Process the stream
	if err := s.Receive(ctx, stream); err != nil {
		logger.Fatalf("failed to process stream: %v", err)
	}
}
