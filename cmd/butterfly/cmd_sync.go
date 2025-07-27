package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
	"github.com/bluesky-social/indigo/cmd/butterfly/store"
	"github.com/urfave/cli/v2"
)

var syncCommand = &cli.Command{
	Name:  "sync",
	Usage: "Sync repositories from various sources",
	Description: `The sync command fetches and syncs repository data from various sources including
CAR files, PDS instances, relays, and Jetstream. It supports different output modes
for storing or processing the synced data.`,
	Flags: []cli.Flag{
		// Input source flags
		&cli.StringFlag{
			Name:     "input",
			Usage:    "Input mode: carfile, pds, relay, or jetstream",
			Required: true,
		},
		&cli.StringFlag{
			Name:  "car",
			Usage: "Path to CAR file to read (for carfile mode)",
		},
		&cli.StringFlag{
			Name:  "pds",
			Usage: "PDS service URL (for pds mode)",
		},
		&cli.StringFlag{
			Name:  "relay",
			Usage: "Relay service URL (for relay mode)",
		},
		&cli.StringFlag{
			Name:  "jetstream",
			Usage: "Jetstream service URL (for jetstream mode)",
		},
		// Common flags
		&cli.StringFlag{
			Name:  "did",
			Usage: "DID to fetch (required for carfile/pds modes)",
		},
		&cli.StringFlag{
			Name:  "store",
			Value: "stdout",
			Usage: "Storage mode: stdout, tarfiles, or duckdb",
		},
		&cli.StringFlag{
			Name:  "storage-dir",
			Value: "./output",
			Usage: "Output directory for tarfiles mode",
		},
		&cli.StringFlag{
			Name:  "db",
			Value: "./butterfly.db",
			Usage: "Path to DuckDB database file",
		},
	},
	Action: runSync,
}

func runSync(c *cli.Context) error {
	// Set up logger
	logger := log.New(os.Stderr, "butterfly sync: ", log.LstdFlags)

	// Get flags
	inputMode := c.String("input")
	carFile := c.String("car")
	pdsService := c.String("pds")
	relayService := c.String("relay")
	jetService := c.String("jetstream")
	did := c.String("did")
	storeMode := c.String("store")
	storageDir := c.String("storage-dir")
	dbPath := c.String("db")

	// Create remote based on input mode
	var r remote.Remote
	switch inputMode {
	case "carfile":
		if carFile == "" || did == "" {
			return fmt.Errorf("carfile mode requires -car and -did flags")
		}
		r = &remote.CarfileRemote{Filepath: carFile}
	case "pds":
		if pdsService == "" || did == "" {
			return fmt.Errorf("pds mode requires -pds and -did flags")
		}
		r = &remote.PdsRemote{Service: pdsService}
	case "relay":
		if relayService == "" {
			return fmt.Errorf("relay mode requires -relay flag")
		}
		r = &remote.RelayRemote{Service: relayService}
	case "jetstream":
		if jetService == "" {
			return fmt.Errorf("jetstream mode requires -jetstream flag")
		}
		r = &remote.JetstreamRemote{Service: jetService}
	default:
		return fmt.Errorf("unknown input mode: %s", inputMode)
	}

	// Create store based on storage mode
	var s store.Store
	switch storeMode {
	case "stdout":
		s = &store.StdoutStore{Mode: store.StdoutStoreModeStats}
	case "tarfiles":
		s = store.NewTarfilesStore(storageDir)
	case "duckdb":
		s = store.NewDuckdbStore(dbPath)
	default:
		return fmt.Errorf("unknown storage mode: %s", storeMode)
	}

	// Create context
	ctx := context.Background()

	// Initialize store
	if err := s.Setup(ctx); err != nil {
		return fmt.Errorf("failed to setup store: %w", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			logger.Printf("failed to close store: %v", err)
		}
	}()

	// Handle different input modes
	switch inputMode {
	case "carfile", "pds":
		// These modes fetch a specific repository
		stream, err := r.FetchRepo(ctx, remote.FetchRepoParams{Did: did})
		if err != nil {
			return fmt.Errorf("failed to fetch repo: %w", err)
		}
		defer stream.Close()

		// Process the stream
		if err := s.BackfillRepo(ctx, did, stream); err != nil {
			return fmt.Errorf("failed to process stream: %w", err)
		}

	case "relay", "jetstream":
		// These modes subscribe to record streams
		params := remote.SubscribeRecordsParams{}
		if did != "" {
			params.Dids = []string{did}
		}

		stream, err := r.SubscribeRecords(ctx, params)
		if err != nil {
			return fmt.Errorf("failed to subscribe to records: %w", err)
		}
		defer stream.Close()

		// Process the stream continuously
		if err := s.ActiveSync(ctx, stream); err != nil {
			return fmt.Errorf("failed to process stream: %w", err)
		}
	}

	return nil
}
