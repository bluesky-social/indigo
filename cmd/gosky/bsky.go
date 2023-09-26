package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/util/cliutil"

	"github.com/ipfs/go-cid"
	cli "github.com/urfave/cli/v2"
)

var followsCmd = &cli.Command{
	Name:  "follows",
	Usage: "sub-commands for bsky follows",
	Subcommands: []*cli.Command{
		followsAddCmd,
		followsListCmd,
	},
}

var followsAddCmd = &cli.Command{
	Name:      "add",
	Flags:     []cli.Flag{},
	ArgsUsage: `<user>`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		user := cctx.Args().First()

		follow := appbsky.GraphFollow{
			LexiconTypeID: "app.bsky.graph.follow",
			CreatedAt:     time.Now().Format(time.RFC3339),
			Subject:       user,
		}

		resp, err := comatproto.RepoCreateRecord(context.TODO(), xrpcc, &comatproto.RepoCreateRecord_Input{
			Collection: "app.bsky.graph.follow",
			Repo:       xrpcc.Auth.Did,
			Record:     &lexutil.LexiconTypeDecoder{&follow},
		})
		if err != nil {
			return err
		}

		fmt.Println(resp.Uri)

		return nil
	},
}

var followsListCmd = &cli.Command{
	Name:      "list",
	Usage:     "print list of follows for account",
	ArgsUsage: `[actor]`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		user := cctx.Args().First()
		if user == "" {
			user = xrpcc.Auth.Did
		}

		ctx := context.TODO()
		resp, err := appbsky.GraphGetFollows(ctx, xrpcc, user, "", 100)
		if err != nil {
			return err
		}

		for _, f := range resp.Follows {
			fmt.Println(f.Did, f.Handle)
		}

		return nil
	},
}

var postCmd = &cli.Command{
	Name:      "post",
	Usage:     "create a post record",
	ArgsUsage: `<text>`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		auth := xrpcc.Auth

		text := strings.Join(cctx.Args().Slice(), " ")

		resp, err := comatproto.RepoCreateRecord(context.TODO(), xrpcc, &comatproto.RepoCreateRecord_Input{
			Collection: "app.bsky.feed.post",
			Repo:       auth.Did,
			Record: &lexutil.LexiconTypeDecoder{&appbsky.FeedPost{
				Text:      text,
				CreatedAt: time.Now().Format(util.ISO8601),
			}},
		})
		if err != nil {
			return fmt.Errorf("failed to create post: %w", err)
		}

		fmt.Println(resp.Cid)
		fmt.Println(resp.Uri)

		return nil
	},
}

func prettyPrintPost(p *appbsky.FeedDefs_FeedViewPost, uris bool) {
	fmt.Println(strings.Repeat("-", 60))
	rec := p.Post.Record.Val.(*appbsky.FeedPost)
	fmt.Printf("%s (%s)", p.Post.Author.Handle, rec.CreatedAt)
	if uris {
		fmt.Println(" -- ", p.Post.Uri)
	} else {
		fmt.Println(":")
	}
	fmt.Println(rec.Text)
}

var feedGetCmd = &cli.Command{
	Name:  "feed",
	Usage: "fetch bsky feed",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "count",
			Value: 100,
		},
		&cli.StringFlag{
			Name:  "author",
			Usage: "specify handle of user to list their authored feed",
		},
		&cli.BoolFlag{
			Name:  "raw",
			Usage: "print out feed in raw json",
		},
		&cli.BoolFlag{
			Name:  "uris",
			Usage: "include URIs in pretty print output",
		},
	},
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		raw := cctx.Bool("raw")

		uris := cctx.Bool("uris")

		author := cctx.String("author")
		if author != "" {
			if author == "self" {
				author = xrpcc.Auth.Did
			}

			tl, err := appbsky.FeedGetAuthorFeed(ctx, xrpcc, author, "", "", 99)
			if err != nil {
				return err
			}

			for i := len(tl.Feed) - 1; i >= 0; i-- {
				it := tl.Feed[i]
				if raw {
					jsonPrint(it)
				} else {
					prettyPrintPost(it, uris)
				}
			}
		} else {
			algo := "reverse-chronological"
			tl, err := appbsky.FeedGetTimeline(ctx, xrpcc, algo, "", int64(cctx.Int("count")))
			if err != nil {
				return err
			}

			for i := len(tl.Feed) - 1; i >= 0; i-- {
				it := tl.Feed[i]
				if raw {
					jsonPrint(it)
				} else {
					prettyPrintPost(it, uris)
				}
			}
		}

		return nil

	},
}

var actorGetSuggestionsCmd = &cli.Command{
	Name:      "actorGetSuggestions",
	ArgsUsage: "[author]",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		author := cctx.Args().First()
		if author == "" {
			author = xrpcc.Auth.Did
		}

		resp, err := appbsky.ActorGetSuggestions(ctx, xrpcc, "", 100)
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

var feedSetVoteCmd = &cli.Command{
	Name:      "vote",
	Usage:     "create bsky 'like' record",
	ArgsUsage: "<post>",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		arg := cctx.Args().First()

		parts := strings.Split(arg, "/")
		if len(parts) < 3 {
			return fmt.Errorf("invalid post uri: %q", arg)
		}
		rkey := parts[len(parts)-1]
		collection := parts[len(parts)-2]
		did := parts[2]

		fmt.Println(did, collection, rkey)
		ctx := context.TODO()
		resp, err := comatproto.RepoGetRecord(ctx, xrpcc, "", collection, did, rkey)
		if err != nil {
			return fmt.Errorf("getting record: %w", err)
		}

		out, err := comatproto.RepoCreateRecord(ctx, xrpcc, &comatproto.RepoCreateRecord_Input{
			Collection: "app.bsky.feed.like",
			Repo:       xrpcc.Auth.Did,
			Record: &lexutil.LexiconTypeDecoder{
				Val: &appbsky.FeedLike{
					CreatedAt: time.Now().Format(util.ISO8601),
					Subject:   &comatproto.RepoStrongRef{Uri: resp.Uri, Cid: *resp.Cid},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("creating vote failed: %w", err)
		}
		_ = out
		return nil

	},
}

var deletePostCmd = &cli.Command{
	Name:      "delete",
	ArgsUsage: `<rkey>`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		rkey := cctx.Args().First()

		if rkey == "" {
			return fmt.Errorf("must specify rkey of post to delete")
		}

		schema := "app.bsky.feed.post"
		if strings.Contains(rkey, "/") {
			parts := strings.Split(rkey, "/")
			schema = parts[0]
			rkey = parts[1]
		}

		return comatproto.RepoDeleteRecord(context.TODO(), xrpcc, &comatproto.RepoDeleteRecord_Input{
			Repo:       xrpcc.Auth.Did,
			Collection: schema,
			Rkey:       rkey,
		})
	},
}

var listAllPostsCmd = &cli.Command{
	Name: "list",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "all",
		},
		&cli.BoolFlag{
			Name: "values",
		},
		&cli.BoolFlag{
			Name: "cids",
		},
	},
	ArgsUsage: `<did>|<repo-path>`,
	Action: func(cctx *cli.Context) error {

		arg := cctx.Args().First()
		ctx := context.TODO()

		var repob []byte
		if strings.HasPrefix(arg, "did:") {
			xrpcc, err := cliutil.GetXrpcClient(cctx, true)
			if err != nil {
				return err
			}

			if arg == "" {
				arg = xrpcc.Auth.Did
			}

			rrb, err := comatproto.SyncGetRepo(ctx, xrpcc, arg, "")
			if err != nil {
				return err
			}
			repob = rrb
		} else {
			if len(arg) == 0 {
				return cli.Exit("must specify DID string or repo path", 127)
			}
			fb, err := os.ReadFile(arg)
			if err != nil {
				return err
			}

			repob = fb
		}

		rr, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(repob))
		if err != nil {
			return err
		}

		collection := "app.bsky.feed.post"
		if cctx.Bool("all") {
			collection = ""
		}
		vals := cctx.Bool("values")
		cids := cctx.Bool("cids")

		if err := rr.ForEach(ctx, collection, func(k string, v cid.Cid) error {
			if !strings.HasPrefix(k, collection) {
				return repo.ErrDoneIterating
			}

			fmt.Print(k)
			if cids {
				fmt.Println(" - ", v)
			} else {
				fmt.Println()
			}
			if vals {
				b, err := rr.Blockstore().Get(ctx, v)
				if err != nil {
					return err
				}

				convb, err := cborToJson(b.RawData())
				if err != nil {
					return err
				}
				fmt.Println(string(convb))
			}
			return nil
		}); err != nil {
			return err
		}

		return nil
	},
}

var getNotificationsCmd = &cli.Command{
	Name:  "notifs",
	Usage: "fetch bsky notifications (requires auth)",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		notifs, err := appbsky.NotificationListNotifications(ctx, xrpcc, "", 50, "")
		if err != nil {
			return err
		}

		for _, n := range notifs.Notifications {
			b, err := json.Marshal(n)
			if err != nil {
				return err
			}

			fmt.Println(string(b))
		}

		return nil
	},
}
