package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/version"
	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"

	_ "github.com/joho/godotenv/autoload"

	logging "github.com/ipfs/go-log"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/polydawn/refmt/cbor"
	rejson "github.com/polydawn/refmt/json"
	"github.com/polydawn/refmt/shared"
	cli "github.com/urfave/cli/v2"
	"github.com/whyrusleeping/go-did"
)

var log = logging.Logger("gosky")

func main() {
	run(os.Args)
}

func run(args []string) {

	app := cli.App{
		Name:    "gosky",
		Usage:   "client CLI for atproto and bluesky",
		Version: version.Version,
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "pds-host",
			Usage:   "method, hostname, and port of PDS instance",
			Value:   "https://bsky.social",
			EnvVars: []string{"ATP_PDS_HOST"},
		},
		&cli.StringFlag{
			Name:    "auth",
			Usage:   "path to JSON file with ATP auth info",
			Value:   "bsky.auth",
			EnvVars: []string{"ATP_AUTH_FILE"},
		},
	}
	app.Commands = []*cli.Command{
		actorGetSuggestionsCmd,
		createSessionCmd,
		debugCmd,
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
		readRepoStreamCmd,
		updateHandleCmd,
	}

	app.RunAndExitOnError()
}

var newAccountCmd = &cli.Command{
	Name: "newAccount",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
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

		acc, err := comatproto.AccountCreate(context.TODO(), xrpcc, &comatproto.AccountCreate_Input{
			Email:      email,
			Handle:     handle,
			InviteCode: invite,
			Password:   password,
		})
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
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}
		handle := cctx.Args().Get(0)
		password := cctx.Args().Get(1)

		ses, err := comatproto.SessionCreate(context.TODO(), xrpcc, &comatproto.SessionCreate_Input{
			Identifier: &handle,
			Password:   password,
		})
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
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		auth := xrpcc.Auth

		text := strings.Join(cctx.Args().Slice(), " ")

		resp, err := comatproto.RepoCreateRecord(context.TODO(), xrpcc, &comatproto.RepoCreateRecord_Input{
			Collection: "app.bsky.feed.post",
			Did:        auth.Did,
			Record: lexutil.LexiconTypeDecoder{&appbsky.FeedPost{
				Text:      text,
				CreatedAt: time.Now().Format("2006-01-02T15:04:05.000Z"),
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

var didCmd = &cli.Command{
	Name: "did",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "plc",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
	},
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

		fmt.Println("KEYDID: ", sigkey.Public().DID())

		ndid, err := s.CreateDID(context.TODO(), sigkey, recoverydid, handle, service)
		if err != nil {
			return err
		}

		fmt.Println(ndid)
		return nil
	},
}

func loadKey(kfile string) (*did.PrivKey, error) {
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

	var out string
	kts := string(curve.(jwa.EllipticCurveAlgorithm))
	switch kts {
	case "P-256":
		out = did.KeyTypeP256
	default:
		return nil, fmt.Errorf("unrecognized key type: %s", kts)
	}

	return &did.PrivKey{
		Raw:  &spk,
		Type: out,
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
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		repobytes, err := comatproto.SyncGetRepo(ctx, xrpcc, cctx.Args().First(), "", "")
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
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		root, err := comatproto.SyncGetHead(ctx, xrpcc, cctx.Args().First())
		if err != nil {
			return err
		}

		fmt.Println(root.Root)

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

			tl, err := appbsky.FeedGetAuthorFeed(ctx, xrpcc, author, "", 99)
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
	Name: "actorGetSuggestions",
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
	ArgsUsage: "<post> [direction]",
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

		dir := cctx.Args().Get(1)
		if dir == "" {
			dir = "up"
		}

		fmt.Println(did, collection, rkey)
		ctx := context.TODO()
		resp, err := comatproto.RepoGetRecord(ctx, xrpcc, "", collection, rkey, did)
		if err != nil {
			return fmt.Errorf("getting record: %w", err)
		}

		out, err := comatproto.RepoCreateRecord(ctx, xrpcc, &comatproto.RepoCreateRecord_Input{
			LexiconTypeID: "com.atproto.feed.like",
			Collection:    "com.atproto.feed.like",
			Did:           did,
			Record: lexutil.LexiconTypeDecoder{
				Val: &appbsky.FeedLike{
					CreatedAt: time.Now().Format(util.ISO8601),
					Subject:   &comatproto.RepoStrongRef{Uri: resp.Uri, Cid: *resp.Cid},
				},
			},
		})
		if err != nil {
			return err
		}
		_ = out
		return nil

	},
}

var refreshAuthTokenCmd = &cli.Command{
	Name:  "refresh",
	Usage: "refresh your auth token and overwrite it with new auth info",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		a := xrpcc.Auth
		a.AccessJwt = a.RefreshJwt

		ctx := context.TODO()
		nauth, err := comatproto.SessionRefresh(ctx, xrpcc)
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
			Did:        xrpcc.Auth.Did,
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

			rrb, err := comatproto.SyncGetRepo(ctx, xrpcc, arg, "", "")
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

		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		notifs, err := appbsky.NotificationListNotifications(ctx, xrpcc, "", 50)
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
			Did:        xrpcc.Auth.Did,
			Record:     lexutil.LexiconTypeDecoder{&follow},
		})
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
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
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

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		email := cctx.Args().Get(0)

		err = comatproto.AccountRequestPasswordReset(ctx, xrpcc, &comatproto.AccountRequestPasswordReset_Input{
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

		if err := comatproto.AccountResetPassword(ctx, xrpcc, &comatproto.AccountResetPassword_Input{
			Password: npass,
			Token:    code,
		}); err != nil {
			return err
		}

		return nil
	},
}

var updateHandleCmd = &cli.Command{
	Name: "updateHandle",
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		handle := cctx.Args().Get(0)

		err = comatproto.HandleUpdate(ctx, xrpcc, &comatproto.HandleUpdate_Input{
			Handle: handle,
		})
		if err != nil {
			return err
		}

		return nil
	},
}

var readRepoStreamCmd = &cli.Command{
	Name: "readStream",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "json",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT)
		defer stop()

		arg := cctx.Args().First()
		if !strings.Contains(arg, "subscribeAllRepos") {
			arg = arg + "/xrpc/com.atproto.sync.subscribeAllRepos"
		}
		if len(cctx.Args().Slice()) == 2 {
			arg = fmt.Sprintf("%s?cursor=%s", arg, cctx.Args().Get(1))
		}

		fmt.Println("dialing: ", arg)
		d := websocket.DefaultDialer
		con, _, err := d.Dial(arg, http.Header{})
		if err != nil {
			return fmt.Errorf("dial failure: %w", err)
		}

		jsonfmt := cctx.Bool("json")

		fmt.Println("Stream Started", time.Now().Format(time.RFC3339))
		defer func() {
			fmt.Println("Stream Exited", time.Now().Format(time.RFC3339))
		}()

		go func() {
			<-ctx.Done()
			_ = con.Close()
		}()

		return events.HandleRepoStream(ctx, con, &events.RepoStreamCallbacks{
			RepoAppend: func(evt *events.RepoAppend) error {
				if jsonfmt {
					b, err := json.Marshal(evt)
					if err != nil {
						return err
					}
					var out map[string]any
					if err := json.Unmarshal(b, &out); err != nil {
						return err
					}
					out["Blocks"] = fmt.Sprintf("[%d bytes]", len(evt.Blocks))

					b, err = json.Marshal(out)
					if err != nil {
						return err
					}
					fmt.Println(string(b))

				} else {
					pstr := "<nil>"
					if evt.Prev != nil {
						pstr = *evt.Prev
					}
					fmt.Printf("(%d) RepoAppend: %s (%s -> %s)\n", evt.Seq, evt.Repo, pstr, evt.Commit)
				}

				return nil
			},
			Info: func(info *events.InfoFrame) error {
				if jsonfmt {
					b, err := json.Marshal(info)
					if err != nil {
						return err
					}
					fmt.Println(string(b))
				} else {
					fmt.Printf("INFO: %s: %s\n", info.Info, info.Message)
				}

				return nil
			},
			Error: func(errf *events.ErrorFrame) error {
				return fmt.Errorf("error frame: %s: %s", errf.Error, errf.Message)
			},
		})
	},
}
