package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/lestrrat-go/jwx/jwa"
	"github.com/lestrrat-go/jwx/jwk"
	"github.com/polydawn/refmt/cbor"
	rejson "github.com/polydawn/refmt/json"
	"github.com/polydawn/refmt/shared"
	cli "github.com/urfave/cli/v2"
	api "github.com/whyrusleeping/gosky/api"
	atproto "github.com/whyrusleeping/gosky/api/atproto"
	apibsky "github.com/whyrusleeping/gosky/api/bsky"
	cliutil "github.com/whyrusleeping/gosky/cmd/gosky/util"
	"github.com/whyrusleeping/gosky/key"
	"github.com/whyrusleeping/gosky/repo"
)

func main() {
	app := cli.NewApp()

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:  "pds",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "auth",
			Value: "bsky.auth",
		},
	}
	app.Commands = []*cli.Command{
		actorGetSuggestionsCmd,
		createSessionCmd,
		deletePostCmd,
		didCmd,
		feedGetCmd,
		feedSetVoteCmd,
		newAccountCmd,
		postCmd,
		refreshAuthTokenCmd,
		syncCmd,
		listAllPostsCmd,
		deletePostCmd,
		getNotificationsCmd,
		followsCmd,
		resetPasswordCmd,
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
			Type:      "app.bsky.feed.post",
			Text:      text,
			CreatedAt: time.Now().Format("2006-01-02T15:04:05.000Z"),
		})
		if err != nil {
			return fmt.Errorf("failed to create post: %w", err)
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
		didCreateCmd,
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

var didCreateCmd = &cli.Command{
	Name: "create",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "recoverydid",
		},
		&cli.StringFlag{
			Name: "signingkey",
		},
		&cli.StringFlag{
			Name:    "plc",
			Value:   "https://plc.staging.bsky.dev",
			EnvVars: []string{"BSKY_PLC_URL"},
		},
	},
	Action: func(cctx *cli.Context) error {
		s := cliutil.GetPLCClient(cctx)

		handle := cctx.Args().Get(0)
		service := cctx.Args().Get(1)

		recoverydid := cctx.String("recoverydid")

		sigkey, err := loadKey(cctx.String("signingkey"))
		if err != nil {
			return err
		}

		fmt.Println("KEYDID: ", sigkey.DID())

		ndid, err := s.CreateDID(context.TODO(), sigkey, recoverydid, handle, service)
		if err != nil {
			return err
		}

		fmt.Println(ndid)
		return nil
	},
}

func loadKey(kfile string) (*key.Key, error) {
	kb, err := os.ReadFile(kfile)
	if err != nil {
		return nil, err
	}

	sk, err := jwk.ParseKey(kb)
	if err != nil {
		return nil, err
	}

	var spk ecdsa.PrivateKey
	if err := sk.Raw(&spk); err != nil {
		return nil, err
	}
	curve, ok := sk.Get("crv")
	if !ok {
		return nil, fmt.Errorf("need a curve set")
	}

	return &key.Key{
		Raw:  &spk,
		Type: string(curve.(jwa.EllipticCurveAlgorithm)),
	}, nil
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

func jsonPrint(i any) {
	b, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(b))
}

func prettyPrintPost(p *apibsky.FeedFeedViewPost) {
	fmt.Println(strings.Repeat("-", 60))
	rec := p.Post.Record.(map[string]any)
	fmt.Printf("%s (%s):\n", p.Post.Author.Handle, rec["createdAt"])
	fmt.Println(rec["text"])
}

var feedGetCmd = &cli.Command{
	Name: "feed",
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
	},
	Action: func(cctx *cli.Context) error {
		bsky, err := cliutil.GetBskyClient(cctx, true)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		raw := cctx.Bool("raw")

		author := cctx.String("author")
		if author != "" {
			if author == "self" {
				author = bsky.C.Auth.Did
			}

			tl, err := bsky.FeedGetAuthorFeed(ctx, author, 99, nil)
			if err != nil {
				return err
			}

			for i := len(tl.Feed) - 1; i >= 0; i-- {
				it := tl.Feed[i]
				if raw {
					jsonPrint(it)
				} else {
					prettyPrintPost(it)
				}
			}
		} else {
			algo := "reverse-chronological"
			tl, err := bsky.FeedGetTimeline(ctx, algo, cctx.Int("count"), nil)
			if err != nil {
				return err
			}

			for i := len(tl.Feed) - 1; i >= 0; i-- {
				it := tl.Feed[i]
				if raw {
					jsonPrint(it)
				} else {
					prettyPrintPost(it)
				}
			}
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
	Name:      "vote",
	ArgsUsage: "<post> [direction]",
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
		if len(parts) < 3 {
			return fmt.Errorf("invalid post uri: %q", arg)
		}
		last := parts[len(parts)-1]
		kind := parts[len(parts)-2]
		user := parts[2]

		dir := cctx.Args().Get(1)
		if dir == "" {
			dir = "up"
		}

		fmt.Println(user, kind, last)
		ctx := context.TODO()
		resp, err := api.RepoGetRecord[*api.PostRecord](atpc, ctx, user, kind, last)
		if err != nil {
			return fmt.Errorf("getting record: %w", err)
		}

		err = bskyc.FeedSetVote(ctx, &api.PostRef{Uri: resp.Uri, Cid: resp.Cid}, dir)
		if err != nil {
			return err
		}
		return nil

	},
}

var refreshAuthTokenCmd = &cli.Command{
	Name:  "refresh",
	Usage: "refresh your auth token and overwrite it with new auth info",
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
	Action: func(cctx *cli.Context) error {

		arg := cctx.Args().First()
		ctx := context.TODO()

		var repob []byte
		if strings.HasPrefix(arg, "did:") {
			atpc, err := cliutil.GetATPClient(cctx, true)
			if err != nil {
				return err
			}

			if arg == "" {
				arg = atpc.C.Auth.Did
			}

			rrb, err := atpc.SyncGetRepo(ctx, arg, nil)
			if err != nil {
				return err
			}
			repob = rrb
		} else {
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
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		bsky, err := cliutil.GetBskyClient(cctx, true)
		if err != nil {
			return err
		}

		notifs, err := apibsky.NotificationList(ctx, bsky.C, "", 50)
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

var followsCmd = &cli.Command{
	Name: "follows",
	Subcommands: []*cli.Command{
		followsAddCmd,
		followsListCmd,
	},
}

var followsAddCmd = &cli.Command{
	Name:  "add",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		atpc, err := cliutil.GetATPClient(cctx, true)
		if err != nil {
			return err
		}

		user := cctx.Args().First()

		follow := apibsky.GraphFollow{
			LexiconTypeID: "app.bsky.graph.follow",
			CreatedAt:     time.Now().Format(time.RFC3339),
			Subject: &apibsky.ActorRef{
				DeclarationCid: "bafyreid27zk7lbis4zw5fz4podbvbs4fc5ivwji3dmrwa6zggnj4bnd57u",
				Did:            user,
			},
		}

		resp, err := atpc.RepoCreateRecord(ctx, atpc.C.Auth.Did, "app.bsky.graph.follow", true, &follow)
		if err != nil {
			return err
		}

		fmt.Println(resp.Uri)

		return nil
	},
}

var followsListCmd = &cli.Command{
	Name: "list",
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

func cborToJson(data []byte) ([]byte, error) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("panic: ", r)
			fmt.Printf("bad blob: %x\n", data)
		}
	}()
	buf := new(bytes.Buffer)
	enc := rejson.NewEncoder(buf, rejson.EncodeOptions{})

	dec := cbor.NewDecoder(cbor.DecodeOptions{}, bytes.NewReader(data))
	err := shared.TokenPump{dec, enc}.Run()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

var resetPasswordCmd = &cli.Command{
	Name: "resetPassword",
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		atp, err := cliutil.GetATPClient(cctx, false)
		if err != nil {
			return err
		}

		email := cctx.Args().Get(0)

		err = atproto.AccountRequestPasswordReset(ctx, atp.C, &atproto.AccountRequestPasswordReset_Input{
			Email: email,
		})
		if err != nil {
			return err
		}

		inp := bufio.NewScanner(os.Stdin)
		fmt.Println("Enter recovery code from email:")
		inp.Scan()
		code := inp.Text()

		fmt.Println("Enter new password:")
		inp.Scan()
		npass := inp.Text()

		if err := atproto.AccountResetPassword(ctx, atp.C, &atproto.AccountResetPassword_Input{
			Password: npass,
			Token:    code,
		}); err != nil {
			return err
		}

		return nil
	},
}
