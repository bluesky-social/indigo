package main

import (
	"context"
	"fmt"
	"net/http"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/gorilla/websocket"
	cli "github.com/urfave/cli/v2"
)

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
