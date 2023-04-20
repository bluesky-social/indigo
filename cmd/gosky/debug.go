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
	"github.com/bluesky-social/indigo/lex/util"
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
		debugStreamCmd,
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

type eventInfo struct {
	LastCid cid.Cid
	LastSeq int64
}

func cidStr(c *util.LexLink) string {
	if c == nil {
		return "<nil>"
	}

	return c.String()
}

var debugStreamCmd = &cli.Command{
	Name: "debug-stream",
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

		infos := make(map[string]*eventInfo)

		ctx := context.TODO()
		err = events.HandleRepoStream(ctx, con, &events.RepoStreamCallbacks{
			RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {

				fmt.Printf("\rChecking seq: %d      ", evt.Seq)

				if !evt.TooBig {
					r, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Blocks))
					if err != nil {
						fmt.Printf("\nEvent at sequence %d had an invalid repo slice: %s\n", evt.Seq, err)
						return nil
					} else {
						prev, err := r.PrevCommit(ctx)
						if err != nil {
							return err
						}

						var cs, es string
						if prev != nil {
							cs = prev.String()
						}

						if evt.Prev != nil {
							es = evt.Prev.String()
						}

						if cs != es {
							fmt.Printf("\nEvent at sequence %d has mismatch between slice prev and struct prev: %s != %s\n", evt.Seq, prev, evt.Prev)
						}
					}
				}

				cur, ok := infos[evt.Repo]
				if ok {
					if cur.LastCid.String() != cidStr(evt.Prev) {
						fmt.Println()
						fmt.Printf("Event at sequence %d, repo=%s had prev=%s head=%s, but last commit we saw was %s (seq=%d)\n", evt.Seq, evt.Repo, cidStr(evt.Prev), evt.Commit.String(), cur.LastCid, cur.LastSeq)
					}
				}

				infos[evt.Repo] = &eventInfo{
					LastCid: cid.Cid(evt.Commit),
					LastSeq: evt.Seq,
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
		if err != nil {
			return err
		}

		return nil
	},
}

// TODO: WIP - turns out to be more complicated than i initially thought
var streamCompareCmd = &cli.Command{
	Name:  "diff-stream",
	Flags: []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		d := websocket.DefaultDialer

		hosta := cctx.Args().Get(0)
		hostb := cctx.Args().Get(1)

		cona, _, err := d.Dial(fmt.Sprintf("%s/xrpc/com.atproto.sync.subscribeRepos", hosta), http.Header{})
		if err != nil {
			return fmt.Errorf("dial failure: %w", err)
		}

		conb, _, err := d.Dial(fmt.Sprintf("%s/xrpc/com.atproto.sync.subscribeRepos", hostb), http.Header{})
		if err != nil {
			return fmt.Errorf("dial failure: %w", err)
		}

		sd := &streamDiffer{}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			err = events.HandleRepoStream(ctx, cona, &events.RepoStreamCallbacks{
				RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
					sd.PushA(&events.XRPCStreamEvent{
						RepoCommit: evt,
					})
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
			if err != nil {
				log.Errorf("stream A failed: %s", err)
			}
		}()

		go func() {
			err = events.HandleRepoStream(ctx, conb, &events.RepoStreamCallbacks{
				RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
					sd.PushB(&events.XRPCStreamEvent{
						RepoCommit: evt,
					})
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
			if err != nil {
				log.Errorf("stream A failed: %s", err)
			}
		}()

		select {}

		return nil
	},
}

type streamDiffer struct {
	Aevts []*events.XRPCStreamEvent
	Bevts []*events.XRPCStreamEvent
}

func (sd *streamDiffer) PushA(evt *events.XRPCStreamEvent) {
	ix := findEvt(evt, sd.Bevts)
	if ix < 0 {
		sd.Aevts = append(sd.Aevts, evt)
		return
	}

	switch evtOp(evt) {
	case "#commit":
		e := evt.RepoCommit
		oe := sd.Bevts[ix].RepoCommit

		if len(e.Blocks) != len(oe.Blocks) {
			fmt.Printf("seq %d (A) and seq %d (B) have different carslice lengths: %d != %d", e.Seq, oe.Seq, len(e.Blocks), len(oe.Blocks))
		}
	default:
	}

}

func (sd *streamDiffer) PushB(evt *events.XRPCStreamEvent) {

}

func evtOp(evt *events.XRPCStreamEvent) string {
	switch {
	case evt.Error != nil:
		return "ERROR"
	case evt.RepoCommit != nil:
		return "#commit"
	case evt.RepoHandle != nil:
		return "#handle"
	case evt.RepoInfo != nil:
		return "#info"
	case evt.RepoMigrate != nil:
		return "#migrate"
	case evt.RepoTombstone != nil:
		return "#tombstone"
	default:
		return "unknown"
	}
}

func sameCommit(a, b *comatproto.SyncSubscribeRepos_Commit) bool {
	return a.Repo == b.Repo && cidStr(a.Prev) == cidStr(b.Prev)
}

func findEvt(evt *events.XRPCStreamEvent, list []*events.XRPCStreamEvent) int {
	evtop := evtOp(evt)

	for i, oe := range list {
		if evtop != evtOp(oe) {
			continue
		}

		switch {
		case evt.RepoCommit != nil:
			if sameCommit(evt.RepoCommit, oe.RepoCommit) {
				return i
			}
		case evt.RepoHandle != nil:
			panic("not handling handle updates yet")
		case evt.RepoMigrate != nil:
			panic("not handling repo migrates yet")
		default:
			panic("unhandled event type: " + evtop)
		}
	}

	return -1
}
