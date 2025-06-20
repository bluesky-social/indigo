package main

import (
	"context"
	"fmt"

	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	appgndr "github.com/gander-social/gander-indigo-sovereign/api/gndr"
	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"
	lexutil "github.com/gander-social/gander-indigo-sovereign/lex/util"

	"github.com/urfave/cli/v2"
)

var cmdBsky = &cli.Command{
	Name:  "gndr",
	Usage: "sub-commands for gndr app",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:      "post",
			Usage:     "create a post",
			ArgsUsage: `<text>`,
			Action:    runBskyPost,
		},
		cmdBskyPrefs,
	},
}

func runBskyPost(cctx *cli.Context) error {
	ctx := context.Background()
	text := cctx.Args().First()
	if text == "" {
		return fmt.Errorf("need to provide post text as argument")
	}

	xrpcc, err := loadAuthClient(ctx)
	if err == ErrNoAuthSession {
		return fmt.Errorf("auth required, but not logged in")
	} else if err != nil {
		return err
	}

	post := appgndr.FeedPost{
		Text:      text,
		CreatedAt: syntax.DatetimeNow().String(),
	}
	resp, err := comatproto.RepoCreateRecord(ctx, xrpcc, &comatproto.RepoCreateRecord_Input{
		Collection: "gndr.app.feed.post",
		Repo:       xrpcc.Auth.Did,
		Record:     &lexutil.LexiconTypeDecoder{Val: &post},
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s\t%s\n", resp.Uri, resp.Cid)
	aturi, err := syntax.ParseATURI(resp.Uri)
	if err != nil {
		return err
	}
	fmt.Printf("view post at: https://gndr.app/profile/%s/post/%s\n", aturi.Authority(), aturi.RecordKey())
	return nil
}
