package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/api"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/xrpc"
	cli "github.com/urfave/cli/v2"
)

func main() {
	app := cli.NewApp()

	app.Commands = []*cli.Command{
		postingCmd,
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

					res, err := atp.RepoCreateRecord(ctx, acc.Did, "app.bsky.feed.post", true, &api.PostRecord{
						Text:      hex.EncodeToString(buf),
						CreatedAt: time.Now().Format(time.RFC3339),
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
