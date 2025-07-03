package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gander-social/gander-indigo-sovereign/api/atproto"
	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	"github.com/gander-social/gander-indigo-sovereign/api/gndr"
	"github.com/gander-social/gander-indigo-sovereign/atproto/identity"
	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"
	"github.com/gander-social/gander-indigo-sovereign/events"
	"github.com/gander-social/gander-indigo-sovereign/events/schedulers/sequential"
	"github.com/gander-social/gander-indigo-sovereign/handles"
	lexutil "github.com/gander-social/gander-indigo-sovereign/lex/util"
	"github.com/gander-social/gander-indigo-sovereign/repo"
	"github.com/gander-social/gander-indigo-sovereign/util"
	"github.com/gander-social/gander-indigo-sovereign/util/cliutil"
	"github.com/gander-social/gander-indigo-sovereign/xrpc"
	"golang.org/x/time/rate"

	"github.com/gorilla/websocket"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/ipld/go-car"

	_ "github.com/joho/godotenv/autoload"

	"github.com/carlmjohnson/versioninfo"
	"github.com/polydawn/refmt/cbor"
	rejson "github.com/polydawn/refmt/json"
	"github.com/polydawn/refmt/shared"
	cli "github.com/urfave/cli/v2"
)

var log = slog.Default().With("system", "gosky")

func main() {
	run(os.Args)
}

func run(args []string) {

	app := cli.App{
		Name:    "gosky",
		Usage:   "client CLI for atproto and gander",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "pds-host",
			Usage:   "method, hostname, and port of PDS instance",
			Value:   "https://gndr.social",
			EnvVars: []string{"ATP_PDS_HOST"},
		},
		&cli.StringFlag{
			Name:    "auth",
			Usage:   "path to JSON file with ATP auth info",
			Value:   "gndr.auth",
			EnvVars: []string{"ATP_AUTH_FILE"},
		},
		&cli.StringFlag{
			Name:    "plc",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
	}

	_, _, err := cliutil.SetupSlog(cliutil.LogOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging setup error: %s\n", err.Error())
		os.Exit(1)
		return
	}

	app.Commands = []*cli.Command{
		accountCmd,
		adminCmd,
		gndrCmd,
		bgsAdminCmd,
		carCmd,
		debugCmd,
		didCmd,
		handleCmd,
		syncCmd,
		createFeedGeneratorCmd,
		getRecordCmd,
		listAllRecordsCmd,
		readRepoStreamCmd,
		parseRkey,
		listLabelsCmd,
		verifyUserCmd,
	}

	app.RunAndExitOnError()
}

func jsonPrint(i any) {
	b, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(b))
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
	err := shared.TokenPump{TokenSource: dec, TokenSink: enc}.Run()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type cachedHandle struct {
	Handle string
	Valid  time.Time
}

var readRepoStreamCmd = &cli.Command{
	Name:  "read-stream",
	Usage: "subscribe to a repo event stream",
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
		&cli.Float64Flag{
			Name:  "max-throughput",
			Usage: "limit event consumption to a given # of req/sec (debug utility)",
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
		hr := &handles.ProdHandleResolver{}
		resolveHandles := cctx.Bool("resolve-handles")

		cache, _ := lru.New[string, *cachedHandle](10000)
		resolveDid := func(ctx context.Context, did string) (string, error) {
			ch, ok := cache.Get(did)
			if ok {
				if time.Now().Before(ch.Valid) {
					return ch.Handle, nil
				}
			}

			h, _, err := handles.ResolveDidToHandle(ctx, didr, hr, did)
			if err != nil {
				return "", err
			}

			cache.Add(did, &cachedHandle{
				Handle: h,
				Valid:  time.Now().Add(time.Minute * 10),
			})

			return h, nil
		}
		var limiter *rate.Limiter
		if cctx.Float64("max-throughput") > 0 {
			limiter = rate.NewLimiter(rate.Limit(cctx.Float64("max-throughput")), 1)
		}

		rsc := &events.RepoStreamCallbacks{
			RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
				if limiter != nil {
					limiter.Wait(ctx)
				}

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
					var handle string
					if resolveHandles {
						h, err := resolveDid(ctx, evt.Repo)
						if err != nil {
							fmt.Println("failed to resolve handle: ", err)
						} else {
							handle = h
						}
					}
					fmt.Printf("(%d) RepoAppend: %s %s (%s)\n", evt.Seq, evt.Repo, handle, evt.Commit.String())

					if unpack {
						recs, err := unpackRecords(evt.Blocks, evt.Ops)
						if err != nil {
							fmt.Fprintln(os.Stderr, "failed to unpack records: ", err)
						}

						for _, rec := range recs {
							switch rec := rec.(type) {
							case *gndr.FeedPost:
								fmt.Printf("\tPost: %q\n", strings.Replace(rec.Text, "\n", " ", -1))
							}
						}

					}
				}

				return nil
			},
			RepoSync: func(sync *comatproto.SyncSubscribeRepos_Sync) error {
				if jsonfmt {
					b, err := json.Marshal(sync)
					if err != nil {
						return err
					}
					fmt.Println(string(b))
				} else {
					fmt.Printf("(%d) Sync: %s\n", sync.Seq, sync.Did)
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
		seqScheduler := sequential.NewScheduler(con.RemoteAddr().String(), rsc.EventHandler)
		return events.HandleRepoStream(ctx, con, seqScheduler, log)
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

	r, err := repo.OpenRepo(ctx, bstore, carr.Header.Roots[0])
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
	Name:  "get-record",
	Usage: "fetch a single record for a given repo",
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

			rrb, err := comatproto.SyncGetRepo(ctx, xrpcc, rfi, "")
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
		} else if strings.HasPrefix(cctx.Args().First(), "https://gndr.app") {
			xrpcc, err := cliutil.GetXrpcClient(cctx, false)
			if err != nil {
				return err
			}

			parts := strings.Split(cctx.Args().First(), "/")
			if len(parts) < 4 {
				return fmt.Errorf("invalid post url")
			}
			rkey := parts[len(parts)-1]
			did := parts[len(parts)-3]

			var collection string
			switch parts[len(parts)-2] {
			case "post":
				collection = "gndr.app.feed.post"
			case "profile":
				collection = "gndr.app.actor.profile"
				did = rkey
				rkey = "self"
			case "feed":
				collection = "gndr.app.feed.generator"
			default:
				return fmt.Errorf("unrecognized link")
			}

			atid, err := syntax.ParseAtIdentifier(did)
			if err != nil {
				return err
			}

			resp, err := identity.DefaultDirectory().Lookup(ctx, *atid)
			if err != nil {
				return err
			}

			xrpcc.Host = resp.PDSEndpoint()

			out, err := comatproto.RepoGetRecord(ctx, xrpcc, "", collection, did, rkey)
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
			return fmt.Errorf("get record failed: %w", err)
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
	Name:  "create-feed-gen",
	Usage: "create a feed generator record",
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

		rec := &lexutil.LexiconTypeDecoder{Val: &gndr.FeedGenerator{
			CreatedAt:   time.Now().Format(util.ISO8601),
			Description: desc,
			Did:         did,
			DisplayName: name,
		}}

		ex, err := atproto.RepoGetRecord(ctx, xrpcc, "", "gndr.app.feed.generator", xrpcc.Auth.Did, rkey)
		if err == nil {
			resp, err := atproto.RepoPutRecord(ctx, xrpcc, &atproto.RepoPutRecord_Input{
				SwapRecord: ex.Cid,
				Collection: "gndr.app.feed.generator",
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
				Collection: "gndr.app.feed.generator",
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

var listAllRecordsCmd = &cli.Command{
	Name:  "list",
	Usage: "print all of the records for a repo or local CAR file",
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

		collection := "gndr.app.feed.post"
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

var parseRkey = &cli.Command{
	Name:  "parse-rkey",
	Usage: "get the timestamp out of a record key",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "format",
			Value: "rfc3339",
			Usage: "output format (rfc3339 or unix)",
		},
	},
	ArgsUsage: `<rkey>`,
	Action: func(cctx *cli.Context) error {
		arg := cctx.Args().First()
		if arg == "" {
			return cli.Exit("must specify record key", 127)
		}

		tid, err := syntax.ParseTID(arg)
		if err != nil {
			return cli.Exit(fmt.Errorf("failed to parse record key (%s) as a TID: %w", arg, err), 127)
		}

		switch cctx.String("format") {
		case "rfc3339":
			fmt.Println(tid.Time().Format(time.RFC3339Nano))
		case "unix":
			fmt.Println(tid.Time().Unix())
		default:
			return cli.Exit(fmt.Errorf("unknown format: %s", cctx.String("format")), 127)
		}
		return nil
	},
}

var listLabelsCmd = &cli.Command{
	Name:  "list-labels",
	Usage: "list labels",
	Flags: []cli.Flag{
		&cli.DurationFlag{
			Name:  "since",
			Value: time.Hour,
		},
	},
	Action: func(cctx *cli.Context) error {

		ctx := context.TODO()

		delta := cctx.Duration("since")
		since := time.Now().Add(-1 * delta).UnixMilli()

		xrpcc := &xrpc.Client{
			Host: "https://mod.gndr.app",
		}

		for {
			out, err := atproto.TempFetchLabels(ctx, xrpcc, 100, since)
			if err != nil {
				return err
			}

			for _, l := range out.Labels {
				b, err := json.MarshalIndent(l, "", "  ")
				if err != nil {
					return err
				}

				fmt.Println(string(b))
			}

			if len(out.Labels) > 0 {
				last := out.Labels[len(out.Labels)-1]
				dt, err := syntax.ParseDatetime(last.Cts)
				if err != nil {
					return fmt.Errorf("invalid cts: %w", err)
				}
				since = dt.Time().UnixMilli()
			} else {
				break
			}
		}
		return nil
	},
}

var verifyUserCmd = &cli.Command{
	Name:  "verify-user",
	Usage: "create a feed generator record",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		ctx := context.TODO()
		arg := cctx.Args().First()

		idf, err := syntax.ParseAtIdentifier(arg)
		if err != nil {
			return err
		}

		ident, err := identity.DefaultDirectory().Lookup(ctx, *idf)
		if err != nil {
			return err
		}

		profrec, err := atproto.RepoGetRecord(ctx, xrpcc, "", "gndr.app.actor.profile", ident.DID.String(), "self")
		if err != nil {
			return err
		}

		ap, ok := profrec.Value.Val.(*gndr.ActorProfile)
		if !ok {
			return fmt.Errorf("got wrong record type back")
		}

		var dn string
		if ap.DisplayName != nil {
			dn = *ap.DisplayName
		}

		rec := &lexutil.LexiconTypeDecoder{Val: &gndr.GraphVerification{
			CreatedAt:   time.Now().Format(util.ISO8601),
			DisplayName: dn,
			Handle:      ident.Handle.String(),
			Subject:     ident.DID.String(),
		}}

		resp, err := atproto.RepoCreateRecord(ctx, xrpcc, &atproto.RepoCreateRecord_Input{
			Collection: "gndr.app.graph.verification",
			Repo:       xrpcc.Auth.Did,
			Record:     rec,
		})
		if err != nil {
			return err
		}

		fmt.Println(resp.Uri)

		return nil
	},
}
