package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v2"
)

var cmdStatus = &cli.Command{
	Name:   "status",
	Usage:  "check auth session",
	Flags:  []cli.Flag{},
	Action: runStatus,
}

func runStatus(cctx *cli.Context) error {
	ctx := context.Background()

	client, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}
	fmt.Printf("DID: %s\n", client.Auth.Did)
	fmt.Printf("PDS: %s\n", client.Host)

	return nil
}
