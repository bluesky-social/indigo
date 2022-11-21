package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	cli "github.com/urfave/cli/v2"
	api "github.com/whyrusleeping/gosky/api"
	cliutil "github.com/whyrusleeping/gosky/cmd/gosky/util"
)

func main() {
	app := cli.NewApp()

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "pds",
			Value: "https://pds.staging.bsky.dev",
		},
		&cli.StringFlag{
			Name: "account",
		},
	}
	app.Commands = []*cli.Command{
		createSessionCmd,
		newAccountCmd,
		postCmd,
		didCmd,
		syncCmd,
		feedGetCmd,
		feedGetAuthorCmd,
		actorGetSuggestionsCmd,
	}

	app.RunAndExitOnError()
}

var newAccountCmd = &cli.Command{
	Name: "newAccount",
	Action: func(cctx *cli.Context) error {
		atp, err := cliutil.GetATPClient(cctx, false)
		if err != nil {
			return err
		}

		email := cctx.Args().Get(0)
		handle := cctx.Args().Get(1)
		password := cctx.Args().Get(2)

		var invite *string
		if inv := cctx.Args().Get(3); inv != "" {
			invite = &inv
		}

		acc, err := atp.CreateAccount(context.TODO(), email, handle, password, invite)
		if err != nil {
			return err
		}

		b, err := json.MarshalIndent(acc, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
		return nil
	},
}
var createSessionCmd = &cli.Command{
	Name: "createSession",
	Action: func(cctx *cli.Context) error {
		atp, err := cliutil.GetATPClient(cctx, false)
		if err != nil {
			return err
		}
		handle := cctx.Args().Get(0)
		password := cctx.Args().Get(1)

		ses, err := atp.CreateSession(context.TODO(), handle, password)
		if err != nil {
			return err
		}

		b, err := json.MarshalIndent(ses, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
		return nil
	},
}

var postCmd = &cli.Command{
	Name: "post",
	Action: func(cctx *cli.Context) error {
		atp, err := cliutil.GetATPClient(cctx, true)
		if err != nil {
			return err
		}

		auth := atp.C.Auth

		text := strings.Join(cctx.Args().Slice(), " ")

		resp, err := atp.RepoCreateRecord(context.TODO(), auth.Did, "app.bsky.feed.post", true, &api.PostRecord{
			Text:      text,
			CreatedAt: time.Now().Format(time.RFC3339),
		})
		if err != nil {
			return err
		}

		fmt.Println(resp.Cid)
		fmt.Println(resp.Uri)

		return nil
	},
}

var didCmd = &cli.Command{
	Name: "did",
	Subcommands: []*cli.Command{
		didGetCmd,
	},
}

var didGetCmd = &cli.Command{
	Name: "get",
	Action: func(cctx *cli.Context) error {
		s := cliutil.GetPLCClient(cctx)

		doc, err := s.GetDocument(cctx.Args().First())
		if err != nil {
			return err
		}

		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
		return nil
	},
}

var syncCmd = &cli.Command{
	Name: "sync",
	Subcommands: []*cli.Command{
		syncGetRepoCmd,
		syncGetRootCmd,
	},
}

var syncGetRepoCmd = &cli.Command{
	Name: "getRepo",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "raw",
		},
	},
	Action: func(cctx *cli.Context) error {
		atp, err := cliutil.GetATPClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		repobytes, err := atp.SyncGetRepo(ctx, cctx.Args().First(), nil)
		if err != nil {
			return err
		}

		if cctx.Bool("raw") {
			os.Stdout.Write(repobytes)
		} else {
			fmt.Printf("%x", repobytes)
		}

		return nil
	},
}

var syncGetRootCmd = &cli.Command{
	Name: "getRoot",
	Action: func(cctx *cli.Context) error {
		atp, err := cliutil.GetATPClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		root, err := atp.SyncGetRoot(ctx, cctx.Args().First())
		if err != nil {
			return err
		}

		fmt.Println(root)

		return nil
	},
}

var feedGetCmd = &cli.Command{
	Name: "getFeed",
	Action: func(cctx *cli.Context) error {
		bsky, err := cliutil.GetBskyClient(cctx, true)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		algo := "reverse-chronological"
		tl, err := bsky.FeedGetTimeline(ctx, algo, 99, nil)
		if err != nil {
			return err
		}

		fmt.Println(tl.Cursor)
		for _, it := range tl.Feed {
			b, err := json.MarshalIndent(it, "", "  ")
			if err != nil {
				return err
			}

			fmt.Println(string(b))

		}

		return nil

	},
}

var feedGetAuthorCmd = &cli.Command{
	Name: "getAuthorFeed",
	Action: func(cctx *cli.Context) error {
		bsky, err := cliutil.GetBskyClient(cctx, true)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		author := cctx.Args().First()
		if author == "" {
			author = bsky.C.Auth.Did
		}

		tl, err := bsky.FeedGetAuthorFeed(ctx, author, 99, nil)
		if err != nil {
			return err
		}

		fmt.Println(tl.Cursor)
		for _, it := range tl.Feed {
			b, err := json.MarshalIndent(it, "", "  ")
			if err != nil {
				return err
			}

			fmt.Println(string(b))

		}

		return nil

	},
}

var actorGetSuggestionsCmd = &cli.Command{
	Name: "actorGetSuggestions",
	Action: func(cctx *cli.Context) error {
		bsky, err := cliutil.GetBskyClient(cctx, true)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		author := cctx.Args().First()
		if author == "" {
			author = bsky.C.Auth.Did
		}

		resp, err := bsky.ActorGetSuggestions(ctx, 100, nil)
		if err != nil {
			return err
		}

		b, err := json.MarshalIndent(resp.Actors, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))

		return nil

	},
}
