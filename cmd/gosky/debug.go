package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"

	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	"github.com/ipld/go-car/v2"
	cli "github.com/urfave/cli/v2"
)

var debugCmd = &cli.Command{
	Name:        "debug",
	Description: "a set of debugging utilities for atproto",
	Subcommands: []*cli.Command{
		inspectEventCmd,
	},
}

var inspectEventCmd = &cli.Command{
	Name: "inspect-event",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "host",
			Required: true,
		},
		&cli.BoolFlag{
			Name: "dump-raw-blocks",
		},
	},
	Action: func(cctx *cli.Context) error {
		n, err := strconv.Atoi(cctx.Args().First())
		if err != nil {
			return err
		}

		h := cctx.String("host")

		url := fmt.Sprintf("%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", h, n-1)
		d := websocket.DefaultDialer
		con, _, err := d.Dial(url, http.Header{})
		if err != nil {
			return fmt.Errorf("dial failure: %w", err)
		}

		var errFoundIt = fmt.Errorf("gotem")

		var match *comatproto.SyncSubscribeRepos_Commit

		ctx := context.TODO()
		err = events.HandleRepoStream(ctx, con, &events.RepoStreamCallbacks{
			RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
				n := int64(n)
				if evt.Seq == n {
					match = evt
					return errFoundIt
				}
				if evt.Seq > n {
					return fmt.Errorf("record not found in stream")
				}

				return nil
			},
			RepoInfo: func(evt *comatproto.SyncSubscribeRepos_Info) error {
				return nil
			},
			// TODO: all the other Repo* event types
			Error: func(evt *events.ErrorFrame) error {
				return fmt.Errorf("%s: %s", evt.Error, evt.Message)
			},
		})

		if err != errFoundIt {
			return err
		}

		b, err := json.MarshalIndent(match, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))

		br, err := car.NewBlockReader(bytes.NewReader(match.Blocks))
		if err != nil {
			return err
		}

		fmt.Println("\nSlice Dump:")
		fmt.Println("Root: ", br.Roots[0])
		for {
			blk, err := br.Next()
			if err != nil {
				if err == io.EOF {
					break
				}
				return err
			}

			fmt.Println(blk.Cid())
			if cctx.Bool("dump-raw-blocks") {
				fmt.Printf("%x\n", blk.RawData())
			}
		}

		r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(match.Blocks))
		if err != nil {
			return fmt.Errorf("opening repo from slice: %w", err)
		}

		fmt.Println("\nOps: ")
		for _, op := range match.Ops {
			switch repomgr.EventKind(op.Action) {
			case repomgr.EvtKindCreateRecord, repomgr.EvtKindUpdateRecord:
				rcid, _, err := r.GetRecord(ctx, op.Path)
				if err != nil {
					return fmt.Errorf("loading %q: %w", op.Path, err)
				}
				if rcid != cid.Cid(*op.Cid) {
					return fmt.Errorf("mismatch in record cid %s != %s", rcid, *op.Cid)
				}
				fmt.Printf("%s (%s): %s\n", op.Action, op.Path, *op.Cid)
			}
		}

		return nil
	},
}
