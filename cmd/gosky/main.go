package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/api"
	"github.com/bluesky-social/indigo/api/atproto"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/util/cliutil"
	"github.com/bluesky-social/indigo/util/version"
	"github.com/bluesky-social/indigo/xrpc"
	lru "github.com/hashicorp/golang-lru"

	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/ipld/go-car"

	_ "github.com/joho/godotenv/autoload"
	_ "go.uber.org/automaxprocs"

	logging "github.com/ipfs/go-log"
	"github.com/polydawn/refmt/cbor"
	rejson "github.com/polydawn/refmt/json"
	"github.com/polydawn/refmt/shared"
	cli "github.com/urfave/cli/v2"
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
		&cli.StringFlag{
			Name:    "plc",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
	}
	app.Commands = []*cli.Command{
		actorGetSuggestionsCmd,
		bgsAdminCmd,
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
		handleCmd,
		getRecordCmd,
		createInviteCmd,
		adminCmd,
		createFeedGeneratorCmd,
		rebaseRepoCmd,
	}

	app.RunAndExitOnError()
}

var newAccountCmd = &cli.Command{
	Name:      "newAccount",
	ArgsUsage: `<email> <handle> <password> [inviteCode]`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		args, err := needArgs(cctx, "email", "handle", "password")
		if err != nil {
			return err
		}
		email, handle, password := args[0], args[1], args[2]

		var invite *string
		if inv := cctx.Args().Get(3); inv != "" {
			invite = &inv
		}

		acc, err := comatproto.ServerCreateAccount(context.TODO(), xrpcc, &comatproto.ServerCreateAccount_Input{
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
	Name:      "createSession",
	ArgsUsage: `<handle> <password>`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}
		args, err := needArgs(cctx, "handle", "password")
		if err != nil {
			return err
		}
		handle, password := args[0], args[1]

		ses, err := comatproto.ServerCreateSession(context.TODO(), xrpcc, &comatproto.ServerCreateSession_Input{
			Identifier: handle,
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
	Name:      "post",
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

var didCmd = &cli.Command{
	Name:  "did",
	Flags: []cli.Flag{},
	Subcommands: []*cli.Command{
		didGetCmd,
		didCreateCmd,
		didKeyCmd,
	},
}

var didGetCmd = &cli.Command{
	Name:      "get",
	ArgsUsage: `<did>`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "handle",
			Usage: "resolve did to handle and print",
		},
	},
	Action: func(cctx *cli.Context) error {
		s := cliutil.GetDidResolver(cctx)

		did := cctx.Args().First()

		if cctx.Bool("handle") {
			xrpcc, err := cliutil.GetXrpcClient(cctx, false)
			if err != nil {
				return err
			}

			phr := &api.ProdHandleResolver{}
			h, _, err := api.ResolveDidToHandle(context.TODO(), xrpcc, s, phr, did)
			if err != nil {
				return err
			}

			fmt.Println(h)
			return nil
		}

		doc, err := s.GetDocument(context.TODO(), did)
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
	Name:      "create",
	ArgsUsage: `<handle> <service>`,
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

		args, err := needArgs(cctx, "handle", "service")
		if err != nil {
			return err
		}
		handle, service := args[0], args[1]

		recoverydid := cctx.String("recoverydid")

		sigkey, err := cliutil.LoadKeyFromFile(cctx.String("signingkey"))
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

var didKeyCmd = &cli.Command{
	Name: "didKey",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "keypath",
		},
	},
	Action: func(cctx *cli.Context) error {
		sigkey, err := cliutil.LoadKeyFromFile(cctx.String("keypath"))
		if err != nil {
			return err
		}
		fmt.Println(sigkey.Public().DID())
		return nil
	},
}

var syncCmd = &cli.Command{
	Name: "sync",
	Subcommands: []*cli.Command{
		syncGetRepoCmd,
		syncGetRootCmd,
		syncListReposCmd,
	},
}

var syncGetRepoCmd = &cli.Command{
	Name: "getRepo",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "raw",
		},
	},
	ArgsUsage: `<did>`,
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
	Name:      "getRoot",
	ArgsUsage: `<did>`,
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
		nauth, err := comatproto.ServerRefreshSession(ctx, xrpcc)
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

			rrb, err := comatproto.SyncGetRepo(ctx, xrpcc, arg, "", "")
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

var followsCmd = &cli.Command{
	Name: "follows",
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
	Name:      "resetPassword",
	ArgsUsage: `<email>`,
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		args, err := needArgs(cctx, "email")
		if err != nil {
			return err
		}
		email := args[0]

		err = comatproto.ServerRequestPasswordReset(ctx, xrpcc, &comatproto.ServerRequestPasswordReset_Input{
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

		if err := comatproto.ServerResetPassword(ctx, xrpcc, &comatproto.ServerResetPassword_Input{
			Password: npass,
			Token:    code,
		}); err != nil {
			return err
		}

		return nil
	},
}

var handleCmd = &cli.Command{
	Name: "handle",
	Subcommands: []*cli.Command{
		resolveHandleCmd,
		updateHandleCmd,
	},
}

var resolveHandleCmd = &cli.Command{
	Name:      "resolve",
	ArgsUsage: `<handle>`,
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		args, err := needArgs(cctx, "handle")
		if err != nil {
			return err
		}
		handle := args[0]

		phr := &api.ProdHandleResolver{}
		out, err := phr.ResolveHandleToDid(ctx, handle)
		if err != nil {
			return err
		}

		fmt.Println(out)

		return nil
	},
}

var updateHandleCmd = &cli.Command{
	Name:      "update",
	ArgsUsage: `<handle>`,
	Action: func(cctx *cli.Context) error {
		ctx := context.TODO()

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		args, err := needArgs(cctx, "handle")
		if err != nil {
			return err
		}
		handle := args[0]

		err = comatproto.IdentityUpdateHandle(ctx, xrpcc, &comatproto.IdentityUpdateHandle_Input{
			Handle: handle,
		})
		if err != nil {
			return err
		}

		return nil
	},
}

type cachedHandle struct {
	Handle string
	Valid  time.Time
}

var readRepoStreamCmd = &cli.Command{
	Name: "readStream",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "json",
		},
		&cli.BoolFlag{
			Name: "unpack",
		},
		&cli.BoolFlag{
			Name: "resolve-handles",
		},
	},
	ArgsUsage: `[<repo> [cursor]]`,
	Action: func(cctx *cli.Context) error {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT)
		defer stop()

		arg := cctx.Args().First()
		if !strings.Contains(arg, "subscribeRepos") {
			arg = arg + "/xrpc/com.atproto.sync.subscribeRepos"
		}
		if len(cctx.Args().Slice()) == 2 {
			arg = fmt.Sprintf("%s?cursor=%s", arg, cctx.Args().Get(1))
		}

		fmt.Fprintln(os.Stderr, "dialing: ", arg)
		d := websocket.DefaultDialer
		con, _, err := d.Dial(arg, http.Header{})
		if err != nil {
			return fmt.Errorf("dial failure: %w", err)
		}

		jsonfmt := cctx.Bool("json")
		unpack := cctx.Bool("unpack")

		fmt.Fprintln(os.Stderr, "Stream Started", time.Now().Format(time.RFC3339))
		defer func() {
			fmt.Fprintln(os.Stderr, "Stream Exited", time.Now().Format(time.RFC3339))
		}()

		go func() {
			<-ctx.Done()
			_ = con.Close()
		}()

		didr := cliutil.GetDidResolver(cctx)
		hr := &api.ProdHandleResolver{}
		resolveHandles := cctx.Bool("resolve-handles")

		cache, _ := lru.New(10000)
		resolveDid := func(ctx context.Context, did string) (string, error) {
			val, ok := cache.Get(did)
			if ok {
				ch := val.(*cachedHandle)
				if time.Now().Before(ch.Valid) {
					return ch.Handle, nil
				}
			}

			h, _, err := api.ResolveDidToHandle(ctx, &xrpc.Client{Host: "*"}, didr, hr, did)
			if err != nil {
				return "", err
			}

			cache.Add(did, &cachedHandle{
				Handle: h,
				Valid:  time.Now().Add(time.Minute * 10),
			})

			return h, nil
		}

		rsc := &events.RepoStreamCallbacks{
			RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
				if jsonfmt {
					b, err := json.Marshal(evt)
					if err != nil {
						return err
					}
					var out map[string]any
					if err := json.Unmarshal(b, &out); err != nil {
						return err
					}
					out["blocks"] = fmt.Sprintf("[%d bytes]", len(evt.Blocks))

					if unpack {
						recs, err := unpackRecords(evt.Blocks, evt.Ops)
						if err != nil {
							fmt.Fprintln(os.Stderr, "failed to unpack records: ", err)
						}
						out["records"] = recs
					}

					b, err = json.Marshal(out)
					if err != nil {
						return err
					}
					fmt.Println(string(b))
				} else {
					pstr := "<nil>"
					if evt.Prev != nil && evt.Prev.Defined() {
						pstr = evt.Prev.String()
					}
					var handle string
					if resolveHandles {
						h, err := resolveDid(ctx, evt.Repo)
						if err != nil {
							fmt.Println("failed to resolve handle: ", err)
						} else {
							handle = h
						}
					}
					fmt.Printf("(%d) RepoAppend: %s %s (%s -> %s)\n", evt.Seq, evt.Repo, handle, pstr, evt.Commit.String())

					if unpack {
						recs, err := unpackRecords(evt.Blocks, evt.Ops)
						if err != nil {
							fmt.Fprintln(os.Stderr, "failed to unpack records: ", err)
						}

						for _, rec := range recs {
							switch rec := rec.(type) {
							case *bsky.FeedPost:
								fmt.Printf("\tPost: %q\n", strings.Replace(rec.Text, "\n", " ", -1))
							}
						}

					}
				}

				return nil
			},
			RepoHandle: func(handle *comatproto.SyncSubscribeRepos_Handle) error {
				if jsonfmt {
					b, err := json.Marshal(handle)
					if err != nil {
						return err
					}
					fmt.Println(string(b))
				} else {
					fmt.Printf("(%d) RepoHandle: %s (changed to: %s)\n", handle.Seq, handle.Did, handle.Handle)
				}

				return nil

			},
			RepoInfo: func(info *comatproto.SyncSubscribeRepos_Info) error {
				if jsonfmt {
					b, err := json.Marshal(info)
					if err != nil {
						return err
					}
					fmt.Println(string(b))
				} else {
					fmt.Printf("INFO: %s: %v\n", info.Name, info.Message)
				}

				return nil
			},
			// TODO: all the other event types
			Error: func(errf *events.ErrorFrame) error {
				return fmt.Errorf("error frame: %s: %s", errf.Error, errf.Message)
			},
		}
		return events.HandleRepoStream(ctx, con, &events.SequentialScheduler{rsc.EventHandler})
	},
}

func unpackRecords(blks []byte, ops []*atproto.SyncSubscribeRepos_RepoOp) ([]any, error) {
	ctx := context.TODO()

	bstore := blockstore.NewBlockstore(datastore.NewMapDatastore())
	carr, err := car.NewCarReader(bytes.NewReader(blks))
	if err != nil {
		return nil, err
	}

	for {
		blk, err := carr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if err := bstore.Put(ctx, blk); err != nil {
			return nil, err
		}
	}

	r, err := repo.OpenRepo(ctx, bstore, carr.Header.Roots[0], false)
	if err != nil {
		return nil, err
	}

	var out []any
	for _, op := range ops {
		if op.Action == "create" {
			_, rec, err := r.GetRecord(ctx, op.Path)
			if err != nil {
				return nil, err
			}

			out = append(out, rec)
		}
	}

	return out, nil
}

var getRecordCmd = &cli.Command{
	Name: "getRecord",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name: "repo",
		},
		&cli.BoolFlag{
			Name: "raw",
		},
	},
	ArgsUsage: `<rpath>`,
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		rfi := cctx.String("repo")

		var repob []byte
		if strings.HasPrefix(rfi, "did:") {
			xrpcc, err := cliutil.GetXrpcClient(cctx, false)
			if err != nil {
				return err
			}

			rrb, err := comatproto.SyncGetRepo(ctx, xrpcc, rfi, "", "")
			if err != nil {
				return err
			}
			repob = rrb
		} else if strings.HasPrefix(cctx.Args().First(), "at://") {
			xrpcc, err := cliutil.GetXrpcClient(cctx, false)
			if err != nil {
				return err
			}

			puri, err := util.ParseAtUri(cctx.Args().First())
			if err != nil {
				return err
			}

			out, err := comatproto.RepoGetRecord(ctx, xrpcc, "", puri.Collection, puri.Did, puri.Rkey)
			if err != nil {
				return err
			}

			b, err := json.MarshalIndent(out.Value.Val, "", "  ")
			if err != nil {
				return err
			}

			fmt.Println(string(b))
			return nil
		} else {
			fb, err := os.ReadFile(rfi)
			if err != nil {
				return err
			}

			repob = fb
		}

		rr, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(repob))
		if err != nil {
			return err
		}

		rc, rec, err := rr.GetRecord(ctx, cctx.Args().First())
		if err != nil {
			return err
		}

		if cctx.Bool("raw") {
			blk, err := rr.Blockstore().Get(ctx, rc)
			if err != nil {
				return err
			}

			fmt.Printf("%x\n", blk.RawData())
			return nil
		}

		b, err := json.Marshal(rec)
		if err != nil {
			return err
		}

		fmt.Println(string(b))

		return nil
	},
}

var createInviteCmd = &cli.Command{
	Name: "createInvites",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "admin-password",
			EnvVars:  []string{"ATP_AUTH_ADMIN_PASSWORD"},
			Required: true,
		},
		&cli.IntFlag{
			Name:  "useCount",
			Value: 1,
		},
		&cli.IntFlag{
			Name:  "num",
			Value: 1,
		},
		&cli.StringFlag{
			Name: "bulk",
		},
	},
	ArgsUsage: "[handle]",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		adminKey := cctx.String("admin-password")

		count := cctx.Int("useCount")
		num := cctx.Int("num")

		phr := &api.ProdHandleResolver{}
		if bulkfi := cctx.String("bulk"); bulkfi != "" {
			xrpcc.AdminToken = &adminKey
			dids, err := readDids(bulkfi)
			if err != nil {
				return err
			}

			for i, d := range dids {
				if !strings.HasPrefix(d, "did:plc:") {
					out, err := phr.ResolveHandleToDid(context.TODO(), d)
					if err != nil {
						return fmt.Errorf("failed to resolve %q: %w", d, err)
					}

					dids[i] = out
				}
			}

			_, err = comatproto.ServerCreateInviteCodes(context.TODO(), xrpcc, &comatproto.ServerCreateInviteCodes_Input{
				UseCount:    int64(count),
				ForAccounts: dids,
				CodeCount:   int64(num),
			})
			if err != nil {
				return err
			}

			return nil
		}

		var usrdid []string
		if forUser := cctx.Args().Get(0); forUser != "" {
			if !strings.HasPrefix(forUser, "did:") {
				resp, err := phr.ResolveHandleToDid(context.TODO(), forUser)
				if err != nil {
					return fmt.Errorf("resolving handle: %w", err)
				}

				usrdid = []string{resp}
			} else {
				usrdid = []string{forUser}
			}
		}

		xrpcc.AdminToken = &adminKey
		resp, err := comatproto.ServerCreateInviteCodes(context.TODO(), xrpcc, &comatproto.ServerCreateInviteCodes_Input{
			UseCount:    int64(count),
			ForAccounts: usrdid,
			CodeCount:   int64(num),
		})
		if err != nil {
			return fmt.Errorf("creating codes: %w", err)
		}

		for _, c := range resp.Codes {
			for _, cc := range c.Codes {
				fmt.Println(cc)
			}
		}

		return nil
	},
}

func readDids(f string) ([]string, error) {
	fi, err := os.Open(f)
	if err != nil {
		return nil, err
	}

	defer fi.Close()

	scan := bufio.NewScanner(fi)
	var out []string
	for scan.Scan() {
		out = append(out, strings.Split(scan.Text(), " ")[0])
	}

	return out, nil
}

var syncListReposCmd = &cli.Command{
	Name: "listRepos",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		var curs string
		for {
			out, err := comatproto.SyncListRepos(context.TODO(), xrpcc, curs, 1000)
			if err != nil {
				return err
			}

			if len(out.Repos) == 0 {
				break
			}

			for _, r := range out.Repos {
				fmt.Println(r.Did)
			}

			if out.Cursor == nil {
				break
			}

			curs = *out.Cursor
		}

		return nil
	},
}

func needArgs(cctx *cli.Context, name ...string) ([]string, error) {
	var out []string
	for i, n := range name {
		v := cctx.Args().Get(i)
		if v == "" {
			return nil, cli.Exit(fmt.Sprintf("argument %q required at position %d", n, i+1), 127)
		}
		out = append(out, v)
	}
	return out, nil
}

var createFeedGeneratorCmd = &cli.Command{
	Name: "createFeedGen",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "name",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "did",
			Required: true,
		},
		&cli.StringFlag{
			Name: "description",
		},
		&cli.StringFlag{
			Name: "display-name",
		},
	},
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		rkey := cctx.String("name")
		name := rkey
		if dn := cctx.String("display-name"); dn != "" {
			name = dn
		}

		did := cctx.String("did")

		var desc *string
		if d := cctx.String("description"); d != "" {
			desc = &d
		}

		ctx := context.TODO()

		rec := &lexutil.LexiconTypeDecoder{&bsky.FeedGenerator{
			CreatedAt:   time.Now().Format(util.ISO8601),
			Description: desc,
			Did:         did,
			DisplayName: name,
		}}

		ex, err := atproto.RepoGetRecord(ctx, xrpcc, "", "app.bsky.feed.generator", xrpcc.Auth.Did, rkey)
		if err == nil {
			resp, err := atproto.RepoPutRecord(ctx, xrpcc, &atproto.RepoPutRecord_Input{
				SwapRecord: ex.Cid,
				Collection: "app.bsky.feed.generator",
				Repo:       xrpcc.Auth.Did,
				Rkey:       rkey,
				Record:     rec,
			})
			if err != nil {
				return err
			}

			fmt.Println(resp.Uri)
		} else {
			resp, err := atproto.RepoCreateRecord(ctx, xrpcc, &atproto.RepoCreateRecord_Input{
				Collection: "app.bsky.feed.generator",
				Repo:       xrpcc.Auth.Did,
				Rkey:       &rkey,
				Record:     rec,
			})
			if err != nil {
				return err
			}

			fmt.Println(resp.Uri)
		}

		return nil
	},
}

var rebaseRepoCmd = &cli.Command{
	Name:  "rebase",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		did := xrpcc.Auth.Did

		if err := atproto.RepoRebaseRepo(context.Background(), xrpcc, &atproto.RepoRebaseRepo_Input{
			Repo: did,
		}); err != nil {
			return err
		}

		return nil
	},
}
