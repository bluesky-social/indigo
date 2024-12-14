package main

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
	lexutil "github.com/bluesky-social/indigo/lex/util"
)

var cmdBsky = &cli.Command{
	Name:  "bsky",
	Usage: "sub-commands for bsky app",
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
	ctx := cctx.Context
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

	post := bsky.FeedPost{
		Text:      text,
		CreatedAt: syntax.DatetimeNow().String(),
	}
	resp, err := atproto.RepoCreateRecord(ctx, xrpcc, &atproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
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
	fmt.Printf("view post at: https://bsky.app/profile/%s/post/%s\n", aturi.Authority(), aturi.RecordKey())
	return nil
}
