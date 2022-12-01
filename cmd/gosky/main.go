package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	cli "github.com/urfave/cli/v2"
	api "github.com/whyrusleeping/gosky/api"
	cliutil "github.com/whyrusleeping/gosky/cmd/gosky/util"
	"github.com/whyrusleeping/gosky/repo"
)

func main() {
	app := cli.NewApp()

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "pds",
			Value: "https://pds.staging.bsky.dev",
		},
		&cli.StringFlag{
			Name: "auth",
		},
	}
	app.Commands = []*cli.Command{
		actorGetSuggestionsCmd,
		createSessionCmd,
		deletePostCmd,
		didCmd,
		feedGetAuthorCmd,
		feedGetCmd,
		feedSetVoteCmd,
		graphGetFollowsCmd,
		newAccountCmd,
		postCmd,
		refreshAuthTokenCmd,
		syncCmd,
		listAllPostsCmd,
		deletePostCmd,
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

		doc, err := s.GetDocument(context.TODO(), cctx.Args().First())
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
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "count",
			Value: 100,
		},
	},
	Action: func(cctx *cli.Context) error {
		bsky, err := cliutil.GetBskyClient(cctx, true)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		algo := "reverse-chronological"
		tl, err := bsky.FeedGetTimeline(ctx, algo, cctx.Int("count"), nil)
		if err != nil {
			return err
		}

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

var feedSetVoteCmd = &cli.Command{
	Name: "feedSetVote",
	Action: func(cctx *cli.Context) error {
		atpc, err := cliutil.GetATPClient(cctx, true)
		if err != nil {
			return err
		}

		bskyc, err := cliutil.GetBskyClient(cctx, true)
		if err != nil {
			return err
		}

		arg := cctx.Args().First()

		parts := strings.Split(arg, "/")
		last := parts[len(parts)-1]
		kind := parts[len(parts)-2]
		user := parts[2]

		ctx := context.TODO()
		resp, err := api.RepoGetRecord[*api.PostRecord](atpc, ctx, user, kind, last)
		if err != nil {
			return err
		}

		err = bskyc.FeedSetVote(ctx, &api.PostRef{Uri: resp.Uri, Cid: resp.Cid}, cctx.Args().Get(1))
		if err != nil {
			return err
		}
		return nil

	},
}

var graphGetFollowsCmd = &cli.Command{
	Name: "graphGetFollows",
	Action: func(cctx *cli.Context) error {
		bskyc, err := cliutil.GetBskyClient(cctx, true)
		if err != nil {
			return err
		}

		user := cctx.Args().First()
		if user == "" {
			user = bskyc.C.Auth.Did
		}

		ctx := context.TODO()
		resp, err := bskyc.GraphGetFollows(ctx, user, 100, nil)
		if err != nil {
			return err
		}

		for _, f := range resp.Follows {
			fmt.Println(f.Did, f.Handle)
		}

		return nil
	},
}

var refreshAuthTokenCmd = &cli.Command{
	Name: "refresh",
	Action: func(cctx *cli.Context) error {
		atpc, err := cliutil.GetATPClient(cctx, true)
		if err != nil {
			return err
		}

		a := atpc.C.Auth
		a.AccessJwt = a.RefreshJwt

		ctx := context.TODO()
		nauth, err := atpc.SessionRefresh(ctx)
		if err != nil {
			return err
		}

		b, err := json.Marshal(nauth)
		if err != nil {
			return err
		}

		if err := os.WriteFile(cctx.String("auth"), b, 0600); err != nil {
			return err
		}

		return nil
	},
}

var deletePostCmd = &cli.Command{
	Name: "delete",
	Action: func(cctx *cli.Context) error {
		atpc, err := cliutil.GetATPClient(cctx, true)
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

		return atpc.RepoDeleteRecord(context.TODO(), atpc.C.Auth.Did, schema, rkey)
	},
}

var listAllPostsCmd = &cli.Command{
	Name: "list",
	Action: func(cctx *cli.Context) error {
		atpc, err := cliutil.GetATPClient(cctx, true)
		if err != nil {
			return err
		}

		did := cctx.Args().First()
		if did == "" {
			did = atpc.C.Auth.Did
		}

		ctx := context.TODO()
		repob, err := atpc.SyncGetRepo(ctx, did, nil)
		if err != nil {
			return err
		}

		rr, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(repob))
		if err != nil {
			return err
		}

		if err := rr.ForEach(ctx, "app.bsky.feed.post", func(k string, v cid.Cid) error {
			if !strings.HasPrefix(k, "app.bsky.feed.post/") {
				return repo.ErrDoneIterating
			}

			fmt.Println(k)
			return nil
		}); err != nil {
			return err
		}

		return nil
	},
}
