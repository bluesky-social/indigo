package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/bluesky-social/indigo/api/atproto"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"

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
		debugFeedGenCmd,
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
	ArgsUsage: `<cursor>`,
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
		rsc := &events.RepoStreamCallbacks{
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
		}

		err = events.HandleRepoStream(ctx, con, &events.SequentialScheduler{rsc.EventHandler})
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

func cidStr(c *lexutil.LexLink) string {
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
	ArgsUsage: `<cursor>`,
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
		rsc := &events.RepoStreamCallbacks{
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
		}
		err = events.HandleRepoStream(ctx, con, &events.SequentialScheduler{rsc.EventHandler})
		if err != nil {
			return err
		}

		return nil
	},
}

var debugFeedGenCmd = &cli.Command{
	Name: "debug-feed",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, true)
		if err != nil {
			return err
		}

		didr := cliutil.GetDidResolver(cctx)

		uri := cctx.Args().First()
		puri, err := util.ParseAtUri(uri)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		out, err := atproto.RepoGetRecord(ctx, xrpcc, "", puri.Collection, puri.Did, puri.Rkey)
		if err != nil {
			return fmt.Errorf("getting record: %w", err)
		}

		fgr, ok := out.Value.Val.(*bsky.FeedGenerator)
		if !ok {
			return fmt.Errorf("invalid feedgen record")
		}

		fmt.Println("Feed DID is: ", fgr.Did)
		doc, err := didr.GetDocument(ctx, fgr.Did)
		if err != nil {
			return err
		}

		fmt.Println("Got service did document:")
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))

		var ss *did.Service
		for _, s := range doc.Service {
			if s.ID.String() == "#bsky_fg" {
				cp := s
				ss = &cp
				break
			}
		}

		if ss == nil {
			return fmt.Errorf("No '#bsky_fg' service entry found in feedgens DID document")
		}

		fmt.Println("Service endpoint is: ", ss.ServiceEndpoint)

		fgclient := &xrpc.Client{
			Host: ss.ServiceEndpoint,
		}

		desc, err := bsky.FeedDescribeFeedGenerator(ctx, fgclient)
		if err != nil {
			return err
		}

		fmt.Printf("Found %d feeds at discovered endpoint\n", len(desc.Feeds))
		var found bool
		for _, f := range desc.Feeds {
			fmt.Println("Feed: ", f.Uri)
			if f.Uri == uri {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf("specified feed was not present in linked feedGenerators 'describe' method output")
		}

		skel, err := bsky.FeedGetFeedSkeleton(ctx, fgclient, "", uri, 30)
		if err != nil {
			return fmt.Errorf("failed to fetch feed skeleton: %w", err)
		}

		if len(skel.Feed) > 30 {
			return fmt.Errorf("feedgen not respecting limit param (returned %d posts)", len(skel.Feed))
		}

		if len(skel.Feed) == 0 {
			return fmt.Errorf("feedgen response is empty (might be expected since we aren't authed)")
		}

		fmt.Println("Feed response looks good!")

		seen := make(map[string]bool)
		for _, p := range skel.Feed {
			seen[p.Post] = true
		}

		curs := skel.Cursor
		for i := 0; i < 10 && curs != nil; i++ {
			fmt.Println("Response had cursor: ", *curs)
			nresp, err := bsky.FeedGetFeedSkeleton(ctx, fgclient, *curs, uri, 10)
			if err != nil {
				return fmt.Errorf("fetching paginated feed failed: %w", err)
			}

			fmt.Printf("Got %d posts from cursored query\n", len(nresp.Feed))

			if len(nresp.Feed) > 10 {
				return fmt.Errorf("got more posts than we requested")
			}

			for _, p := range nresp.Feed {
				if seen[p.Post] {
					return fmt.Errorf("duplicate post in response: %s", p.Post)
				}

				seen[p.Post] = true
			}

			if len(nresp.Feed) == 0 || nresp.Cursor == nil {
				break
			}

			curs = nresp.Cursor
		}

		return nil
	},
}
