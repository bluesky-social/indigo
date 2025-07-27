package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/bluesky-social/indigo/cmd/butterfly/identity"
	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
	"github.com/bluesky-social/indigo/cmd/butterfly/store"
	"github.com/urfave/cli/v2"
)

var discoverCommand = &cli.Command{
	Name:  "discover",
	Usage: "Discover repositories using ListRepos queries",
	Description: `The discover command uses the remote ListRepos method to discover and query
repositories from PDS instances and relays. It supports pagination and filtering
by collection.`,
	Flags: []cli.Flag{
		// Input source flags
		&cli.StringFlag{
			Name:     "input",
			Usage:    "Input mode: pds or relay",
			Required: true,
		},
		&cli.StringFlag{
			Name:  "pds",
			Usage: "PDS service URL (for pds mode)",
		},
		&cli.StringFlag{
			Name:  "relay",
			Usage: "Relay service URL (for relay mode)",
		},
		// Query parameters
		&cli.StringFlag{
			Name:  "collection",
			Usage: "Filter by collection (optional)",
		},
		&cli.StringFlag{
			Name:  "cursor",
			Usage: "Pagination cursor (optional)",
		},
		&cli.IntFlag{
			Name:  "limit",
			Value: 100,
			Usage: "Maximum number of repositories to return",
		},
		// Common flags
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
	Action: runDiscover,
}

func runDiscover(c *cli.Context) error {
	// Set up logger
	logger := log.New(os.Stderr, "butterfly discover: ", log.LstdFlags)

	// Get flags
	inputMode := c.String("input")
	pdsService := c.String("pds")
	relayService := c.String("relay")
	collection := c.String("collection")
	cursor := c.String("cursor")
	limit := c.Int("limit")
	storeMode := c.String("store")
	storageDir := c.String("storage-dir")
	dbPath := c.String("db")

	// Create remote based on input mode
	var r remote.Remote
	switch inputMode {
	case "pds":
		if pdsService == "" {
			return fmt.Errorf("pds mode requires -pds flag")
		}
		r = &remote.PdsRemote{Service: pdsService}
	case "relay":
		if relayService == "" {
			return fmt.Errorf("relay mode requires -relay flag")
		}
		r = &remote.RelayRemote{Service: relayService}
	default:
		return fmt.Errorf("unknown input mode for discover: %s (must be pds or relay)", inputMode)
	}

	// Create context
	ctx := context.Background()

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

	// Initialize store
	if err := s.Setup(ctx); err != nil {
		return fmt.Errorf("failed to setup store: %w", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			logger.Printf("failed to close store: %v", err)
		}
	}()

	// Set up identity resolver
	var resolver *identity.IdentityResolver = identity.NewIdentityResolver(identity.IdentityResolverConfig{
		Store: s,
	})

	// Prepare ListRepos parameters
	params := remote.ListReposParams{
		Collection: collection,
		Cursor:     cursor,
		Limit:      limit,
	}

	// Call ListRepos
	result, err := r.ListRepos(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to list repos: %w", err)
	}

	// Resolve all results
	fmt.Printf("Discovered %d repositories\n", len(result.Dids))
	for _, did := range result.Dids {
		_, err := resolver.ResolveDID(ctx, did)
		if err != nil {
			fmt.Printf("Failed to resolve %s: %v", did, err)
		}
	}

	return nil
}
