package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/bluesky-social/indigo/api/agnostic"

	"github.com/urfave/cli/v2"
)

var cmdBskyPrefs = &cli.Command{
	Name:  "prefs",
	Usage: "sub-commands for preferences",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:   "export",
			Usage:  "dump preferences out as JSON",
			Action: runBskyPrefsExport,
		},
		&cli.Command{
			Name:      "import",
			Usage:     "upload preferences from JSON file",
			ArgsUsage: `<file>`,
			Action:    runBskyPrefsImport,
		},
	},
}

func runBskyPrefsExport(cctx *cli.Context) error {
	ctx := context.Background()

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	// TODO: does indigo API code crash with unsupported preference '$type'? Eg "Lexicon decoder" with unsupported type.
	resp, err := agnostic.ActorGetPreferences(ctx, xrpcc)
	if err != nil {
		return fmt.Errorf("failed fetching old preferences: %w", err)
	}

	b, err := json.MarshalIndent(resp.Preferences, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))

	return nil
}

func runBskyPrefsImport(cctx *cli.Context) error {
	ctx := context.Background()
	prefsPath := cctx.Args().First()
	if prefsPath == "" {
		return fmt.Errorf("need to provide file path as an argument")
	}

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	prefsBytes, err := os.ReadFile(prefsPath)
	if err != nil {
		return err
	}

	var prefsArray []map[string]any
	if err = json.Unmarshal(prefsBytes, &prefsArray); err != nil {
		return err
	}

	err = agnostic.ActorPutPreferences(ctx, xrpcc, &agnostic.ActorPutPreferences_Input{
		Preferences: prefsArray,
	})
	if err != nil {
		return fmt.Errorf("failed fetching old preferences: %w", err)
	}

	return nil
}
