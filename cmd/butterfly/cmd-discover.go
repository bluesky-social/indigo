package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
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
		// Output format
		&cli.StringFlag{
			Name:  "format",
			Value: "text",
			Usage: "Output format: text, json, or csv",
		},
		// Identity resolution
		&cli.BoolFlag{
			Name:  "resolve",
			Usage: "Resolve DIDs to handles using identity resolution",
		},
		&cli.BoolFlag{
			Name:  "cache",
			Usage: "Enable identity resolution caching",
			Value: true,
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
	format := c.String("format")
	resolve := c.Bool("resolve")
	enableCache := c.Bool("cache")

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

	// Set up identity resolver if requested
	var resolver *IdentityResolver
	if resolve {
		resolver = NewIdentityResolverWithConfig(IdentityResolverConfig{
			EnableCache: enableCache,
		})
	}

	// Format and output results
	switch format {
	case "text":
		return outputDiscoverText(result, resolver, logger)
	case "json":
		return outputDiscoverJSON(result, resolver)
	case "csv":
		return outputDiscoverCSV(result, resolver)
	default:
		return fmt.Errorf("unknown output format: %s", format)
	}
}

func outputDiscoverText(result *remote.ListReposResult, resolver *IdentityResolver, logger *log.Logger) error {
	fmt.Printf("Found %d repositories\n\n", len(result.Dids))

	for i, did := range result.Dids {
		fmt.Printf("%d. %s", i+1, did)

		// Resolve to handle if requested
		if resolver != nil {
			ctx := context.Background()
			ident, err := resolver.ResolveDID(ctx, did)
			if err != nil {
				logger.Printf("Failed to resolve %s: %v", did, err)
				fmt.Println()
			} else {
				fmt.Printf(" (%s)", ident.Handle)
				if pds, err := resolver.GetPDSEndpoint(ident); err == nil {
					fmt.Printf(" - PDS: %s", pds)
				}
				fmt.Println()
			}
		} else {
			fmt.Println()
		}
	}

	if result.Cursor != "" {
		fmt.Printf("\nNext cursor: %s\n", result.Cursor)
	}

	return nil
}

func outputDiscoverJSON(result *remote.ListReposResult, resolver *IdentityResolver) error {
	output := map[string]interface{}{
		"count":  len(result.Dids),
		"cursor": result.Cursor,
	}

	if resolver != nil {
		// Resolve DIDs and include additional info
		repos := make([]map[string]interface{}, 0, len(result.Dids))
		ctx := context.Background()

		for _, did := range result.Dids {
			repo := map[string]interface{}{
				"did": did,
			}

			if ident, err := resolver.ResolveDID(ctx, did); err == nil {
				repo["handle"] = ident.Handle.String()
				if pds, err := resolver.GetPDSEndpoint(ident); err == nil {
					repo["pds"] = pds
				}
			}

			repos = append(repos, repo)
		}
		output["repos"] = repos
	} else {
		output["dids"] = result.Dids
	}

	// Pretty print JSON
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func outputDiscoverCSV(result *remote.ListReposResult, resolver *IdentityResolver) error {
	// Print CSV header
	if resolver != nil {
		fmt.Println("did,handle,pds")
	} else {
		fmt.Println("did")
	}

	// Print rows
	ctx := context.Background()
	for _, did := range result.Dids {
		if resolver != nil {
			ident, err := resolver.ResolveDID(ctx, did)
			if err != nil {
				fmt.Printf("%s,,\n", did)
			} else {
				pds := ""
				if endpoint, err := resolver.GetPDSEndpoint(ident); err == nil {
					pds = endpoint
				}
				fmt.Printf("%s,%s,%s\n", did, ident.Handle, pds)
			}
		} else {
			fmt.Println(did)
		}
	}

	return nil
}
