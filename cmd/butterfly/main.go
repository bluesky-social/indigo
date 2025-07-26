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
		// Input source flags
		inputMode      = flag.String("input", "", "Input mode: carfile, pds, relay, or jetstream (required)")
		carFile        = flag.String("car", "", "Path to CAR file to read (for carfile mode)")
		pdsService     = flag.String("pds", "", "PDS service URL (for pds mode)")
		relayService   = flag.String("relay", "", "Relay service URL (for relay mode)")
		jetService     = flag.String("jetstream", "", "Jetstream service URL (for jetstream mode)")
		
		// Common flags
		did        = flag.String("did", "", "DID to fetch (required for carfile/pds modes)")
		outputMode = flag.String("output", "stats", "Output mode: stats, passthrough, tarfiles, or duckdb")
		outputDir  = flag.String("output-dir", "./output", "Output directory for tarfiles mode")
		dbPath     = flag.String("db", "./butterfly.db", "Path to DuckDB database file")
		help       = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *help || *inputMode == "" {
		fmt.Fprintf(os.Stderr, "Usage: butterfly -input <mode> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Input modes:\n")
		fmt.Fprintf(os.Stderr, "  carfile    Read from a CAR file (-car <path> -did <did>)\n")
		fmt.Fprintf(os.Stderr, "  pds        Fetch from a PDS (-pds <url> -did <did>)\n")
		fmt.Fprintf(os.Stderr, "  relay      Subscribe to a relay (-relay <url>)\n")
		fmt.Fprintf(os.Stderr, "  jetstream  Subscribe to Jetstream (-jetstream <url>)\n\n")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// Set up logger
	logger := log.New(os.Stderr, "butterfly: ", log.LstdFlags)

	// Create remote based on input mode
	var r remote.Remote
	switch *inputMode {
	case "carfile":
		if *carFile == "" || *did == "" {
			logger.Fatalf("carfile mode requires -car and -did flags")
		}
		r = &remote.CarfileRemote{Filepath: *carFile}
	case "pds":
		if *pdsService == "" || *did == "" {
			logger.Fatalf("pds mode requires -pds and -did flags")
		}
		r = &remote.PdsRemote{Service: *pdsService}
	case "relay":
		if *relayService == "" {
			logger.Fatalf("relay mode requires -relay flag")
		}
		r = &remote.RelayRemote{Service: *relayService}
	case "jetstream":
		if *jetService == "" {
			logger.Fatalf("jetstream mode requires -jetstream flag")
		}
		r = &remote.JetstreamRemote{Service: *jetService}
	default:
		logger.Fatalf("unknown input mode: %s", *inputMode)
	}

	// Create store based on output mode
	var s store.Store
	switch *outputMode {
	case "passthrough":
		s = &store.StdoutStore{Mode: store.StdoutStoreModePassthrough}
	case "stats":
		s = &store.StdoutStore{Mode: store.StdoutStoreModeStats}
	case "tarfiles":
		s = store.NewTarfilesStore(*outputDir)
	case "duckdb":
		s = store.NewDuckdbStore(*dbPath)
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

	// Handle different input modes
	switch *inputMode {
	case "carfile", "pds":
		// These modes fetch a specific repository
		stream, err := r.FetchRepo(ctx, remote.FetchRepoParams{Did: *did})
		if err != nil {
			logger.Fatalf("failed to fetch repo: %v", err)
		}
		defer stream.Close()

		// Process the stream
		if err := s.BackfillRepo(ctx, *did, stream); err != nil {
			logger.Fatalf("failed to process stream: %v", err)
		}

	case "relay", "jetstream":
		// These modes subscribe to record streams
		params := remote.SubscribeRecordsParams{}
		if *did != "" {
			params.Dids = []string{*did}
		}
		
		stream, err := r.SubscribeRecords(ctx, params)
		if err != nil {
			logger.Fatalf("failed to subscribe to records: %v", err)
		}
		defer stream.Close()

		// Process the stream continuously
		if err := s.ActiveSync(ctx, stream); err != nil {
			logger.Fatalf("failed to process stream: %v", err)
		}
	}
}
