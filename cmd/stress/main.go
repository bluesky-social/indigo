package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/testing"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	cbor "github.com/ipfs/go-ipld-cbor"
	"github.com/ipld/go-car"
	cli "github.com/urfave/cli/v2"
)

func main() {
	app := cli.NewApp()

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
			Name:  "pds",
			Value: "http://localhost:4989",
		},
	},
	Action: func(cctx *cli.Context) error {
		atp, err := cliutil.GetATPClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		buf := make([]byte, 6)
		rand.Read(buf)
		id := hex.EncodeToString(buf)

		var invite *string
		acc, err := atp.CreateAccount(ctx, fmt.Sprintf("user-%s@test.com", id), "user-"+id+".test", "password", invite)
		if err != nil {
			return err
		}

		quiet := cctx.Bool("quiet")

		atp.C.Auth = &xrpc.AuthInfo{
			Did:       acc.Did,
			AccessJwt: acc.AccessJwt,
			Handle:    acc.Handle,
		}

		count := cctx.Int("count")
		concurrent := cctx.Int("concurrent")

		var wg sync.WaitGroup
		for con := 0; con < concurrent; con++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for i := 0; i < count; i++ {
					buf := make([]byte, 100)
					rand.Read(buf)

					res, err := comatproto.RepoCreateRecord(context.TODO(), atp.C, &comatproto.RepoCreateRecord_Input{
						Collection: "app.bsky.feed.post",
						Did:        acc.Did,
						Record: lexutil.LexiconTypeDecoder{&appbsky.FeedPost{
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
			Name:  "pds",
			Value: "http://localhost:4989",
		},
	},
	Action: func(cctx *cli.Context) error {
		fname := cctx.Args().First()

		l := cctx.Int("len")

		membs := blockstore.NewBlockstore(datastore.NewMapDatastore())

		ctx := context.Background()

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
