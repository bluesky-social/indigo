package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	_ "github.com/joho/godotenv/autoload"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/testing"
	"github.com/bluesky-social/indigo/util/cliutil"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/ipld/go-car"
	"github.com/urfave/cli/v3"
)

func main() {
	run(os.Args)
}

func run(args []string) {
	app := cli.Command{
		Name:    "stress",
		Usage:   "load generation tool for PDS instances",
		Version: versioninfo.Short(),
	}

	app.Commands = []*cli.Command{
		postingCmd,
		genRepoCmd,
	}

	if err := app.Run(context.Background(), args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(-1)
	}
}

var postingCmd = &cli.Command{
	Name: "posting",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "quiet",
		},
		&cli.IntFlag{
			Name:  "count",
			Value: 100,
		},
		&cli.IntFlag{
			Name:  "concurrent",
			Value: 1,
		},
		&cli.StringFlag{
			Name:    "pds-host",
			Usage:   "method, hostname, and port of PDS instance",
			Value:   "http://localhost:4849",
			Sources: cli.EnvVars("ATP_PDS_HOST"),
		},
		&cli.StringFlag{
			Name: "invite",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		xrpcc, err := getXrpcClient(cmd, false)
		if err != nil {
			return err
		}

		count := cmd.Int("count")
		concurrent := cmd.Int("concurrent")
		quiet := cmd.Bool("quiet")

		buf := make([]byte, 6)
		rand.Read(buf)
		id := hex.EncodeToString(buf)

		var invite *string
		if inv := cmd.String("invite"); inv != "" {
			invite = &inv
		}

		cfg, err := comatproto.ServerDescribeServer(ctx, xrpcc)
		if err != nil {
			return err
		}

		domain := cfg.AvailableUserDomains[0]
		fmt.Println("domain: ", domain)

		email := fmt.Sprintf("user-%s@test.com", id)
		pass := "password"
		resp, err := comatproto.ServerCreateAccount(ctx, xrpcc, &comatproto.ServerCreateAccount_Input{
			Email:      &email,
			Handle:     "user-" + id + domain,
			Password:   &pass,
			InviteCode: invite,
		})
		if err != nil {
			return err
		}

		xrpcc.Auth = &xrpc.AuthInfo{
			AccessJwt:  resp.AccessJwt,
			RefreshJwt: resp.RefreshJwt,
			Handle:     resp.Handle,
			Did:        resp.Did,
		}

		var wg sync.WaitGroup
		for con := 0; con < concurrent; con++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for i := 0; i < count; i++ {
					buf := make([]byte, 100)
					rand.Read(buf)

					res, err := comatproto.RepoCreateRecord(context.TODO(), xrpcc, &comatproto.RepoCreateRecord_Input{
						Collection: "app.bsky.feed.post",
						Repo:       xrpcc.Auth.Did,
						Record: &lexutil.LexiconTypeDecoder{Val: &appbsky.FeedPost{
							Text:      hex.EncodeToString(buf),
							CreatedAt: time.Now().Format(time.RFC3339),
						}},
					})
					if err != nil {
						fmt.Printf("errored on worker %d loop %d: %s\n", worker, i, err)
						return
					}

					if !quiet {
						fmt.Println(res.Cid, res.Uri)
					}
				}
			}(con)
		}

		wg.Wait()

		return nil
	},
}

var genRepoCmd = &cli.Command{
	Name: "gen-repo",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "len",
			Value: 50,
		},
		&cli.StringFlag{
			Name:    "pds-host",
			Usage:   "method, hostname, and port of PDS instance",
			Value:   "http://localhost:4849",
			Sources: cli.EnvVars("ATP_PDS_HOST"),
		},
	},
	ArgsUsage: "<car-file-path>",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fname := cmd.Args().First()
		if fname == "" {
			return cli.Exit("must provide car file path", 127)
		}

		l := cmd.Int("len")

		membs := blockstore.NewBlockstore(datastore.NewMapDatastore())

		r := repo.NewRepo(ctx, "did:plc:foobar", membs)

		root, err := testing.GenerateFakeRepo(r, l)
		if err != nil {
			return err
		}

		fi, err := os.Create(fname)
		if err != nil {
			return err
		}
		defer fi.Close()

		h := &car.CarHeader{
			Roots:   []cid.Cid{root},
			Version: 1,
		}
		hb, err := cbor.DumpObject(h)
		if err != nil {
			return err
		}

		_, err = carstore.LdWrite(fi, hb)
		if err != nil {
			return err
		}

		kc, _ := membs.AllKeysChan(ctx)
		for k := range kc {
			blk, err := membs.Get(ctx, k)
			if err != nil {
				return err
			}

			_, err = carstore.LdWrite(fi, k.Bytes(), blk.RawData())
			if err != nil {
				return err
			}
		}

		return nil
	},
}

func getXrpcClient(cmd *cli.Command, authreq bool) (*xrpc.Client, error) {
	h := "http://localhost:4989"
	if pdsurl := cmd.String("pds-host"); pdsurl != "" {
		h = pdsurl
	}

	auth, err := loadAuthFromEnv(cmd, authreq)
	if err != nil {
		return nil, fmt.Errorf("loading auth: %w", err)
	}

	return &xrpc.Client{
		Client: cliutil.NewHttpClient(),
		Host:   h,
		Auth:   auth,
	}, nil
}

func loadAuthFromEnv(cmd *cli.Command, req bool) (*xrpc.AuthInfo, error) {
	if a := cmd.String("auth"); a != "" {
		if ai, err := cliutil.ReadAuth(a); err != nil && req {
			return nil, err
		} else {
			return ai, nil
		}
	}

	val := os.Getenv("ATP_AUTH_FILE")
	if val == "" {
		if req {
			return nil, fmt.Errorf("no auth env present, ATP_AUTH_FILE not set")
		}

		return nil, nil
	}

	var auth xrpc.AuthInfo
	if err := json.Unmarshal([]byte(val), &auth); err != nil {
		return nil, err
	}

	return &auth, nil
}
