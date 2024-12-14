package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/carlmjohnson/versioninfo"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/ipld/go-car"
	_ "github.com/joho/godotenv/autoload"
	cli "github.com/urfave/cli/v2"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/testing"
	"github.com/bluesky-social/indigo/util/cliutil"
	"github.com/bluesky-social/indigo/xrpc"
)

func main() {
	run(os.Args)
}

func run(args []string) {
	app := cli.App{
		Name:    "stress",
		Usage:   "load generation tool for PDS instances",
		Version: versioninfo.Short(),
	}

	app.Commands = []*cli.Command{
		postingCmd,
		genRepoCmd,
	}

	app.RunAndExitOnError()
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
			EnvVars: []string{"ATP_PDS_HOST"},
		},
		&cli.StringFlag{
			Name: "invite",
		},
	},
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		var (
			count      = cctx.Int("count")
			concurrent = cctx.Int("concurrent")
			quiet      = cctx.Bool("quiet")
			ctx        = cctx.Context
		)

		buf := make([]byte, 6)
		rand.Read(buf)
		id := hex.EncodeToString(buf)

		var invite *string
		if inv := cctx.String("invite"); inv != "" {
			invite = &inv
		}

		cfg, err := atproto.ServerDescribeServer(ctx, xrpcc)
		if err != nil {
			return err
		}

		domain := cfg.AvailableUserDomains[0]
		fmt.Println("domain: ", domain)

		email := fmt.Sprintf("user-%s@test.com", id)
		pass := "password"
		resp, err := atproto.ServerCreateAccount(ctx, xrpcc, &atproto.ServerCreateAccount_Input{
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

					res, err := atproto.RepoCreateRecord(ctx, xrpcc, &atproto.RepoCreateRecord_Input{
						Collection: "app.bsky.feed.post",
						Repo:       xrpcc.Auth.Did,
						Record: &lexutil.LexiconTypeDecoder{Val: &bsky.FeedPost{
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
			EnvVars: []string{"ATP_PDS_HOST"},
		},
	},
	ArgsUsage: "<car-file-path>",
	Action: func(cctx *cli.Context) error {
		fname := cctx.Args().First()
		if fname == "" {
			return cli.Exit("must provide car file path", 127)
		}

		l := cctx.Int("len")

		membs := blockstore.NewBlockstore(datastore.NewMapDatastore())

		ctx := cctx.Context

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
