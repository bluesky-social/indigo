package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/util/cliutil"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-libipfs/blocks"
	"github.com/ipld/go-car/v2"
	cli "github.com/urfave/cli/v2"
)

var debugCmd = &cli.Command{
	Name:  "debug",
	Usage: "a set of debugging utilities for atproto",
	Subcommands: []*cli.Command{
		inspectEventCmd,
		debugStreamCmd,
		debugFeedGenCmd,
		debugFeedViewCmd,
		compareStreamsCmd,
		debugGetRepoCmd,
		debugCompareReposCmd,
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

		seqScheduler := sequential.NewScheduler("debug-inspect-event", rsc.EventHandler)
		err = events.HandleRepoStream(ctx, con, seqScheduler, nil)
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
	LastSeq int64
	LastRev string
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

		url := fmt.Sprintf("%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", h, n)
		d := websocket.DefaultDialer
		con, _, err := d.Dial(url, http.Header{})
		if err != nil {
			return fmt.Errorf("dial failure: %w", err)
		}

		infos := make(map[string]*eventInfo)

		var lastSeq int64 = -1
		ctx := context.TODO()
		rsc := &events.RepoStreamCallbacks{
			RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {

				fmt.Printf("\rChecking seq: %d      ", evt.Seq)
				if lastSeq > 0 && evt.Seq != lastSeq+1 {
					fmt.Println("Gap in sequence numbers: ", lastSeq, evt.Seq)
				}
				lastSeq = evt.Seq

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

						if !evt.Rebase && cs != es {
							fmt.Printf("\nEvent at sequence %d has mismatch between slice prev and struct prev: %s != %s\n", evt.Seq, prev, evt.Prev)
						}
					}
				}

				cur, ok := infos[evt.Repo]
				if ok {
					if evt.Since != nil && cur.LastRev != *evt.Since {
						/*
							fmt.Println()
							fmt.Printf("Event at sequence %d, repo=%s had since=%s, but last rev we saw was %s (seq=%d)\n", evt.Seq, evt.Repo, evt.Since, cur.LastRev, cur.LastSeq)
						*/
					}
				}

				infos[evt.Repo] = &eventInfo{
					LastSeq: evt.Seq,
					LastRev: evt.Rev,
				}

				return nil
			},
			RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
				fmt.Printf("\rChecking seq: %d      ", evt.Seq)
				if lastSeq > 0 && evt.Seq != lastSeq+1 {
					fmt.Println("Gap in sequence numbers: ", lastSeq, evt.Seq)
				}
				lastSeq = evt.Seq
				return nil
			},
			RepoTombstone: func(evt *comatproto.SyncSubscribeRepos_Tombstone) error {
				fmt.Printf("\rChecking seq: %d      ", evt.Seq)
				if lastSeq > 0 && evt.Seq != lastSeq+1 {
					fmt.Println("Gap in sequence numbers: ", lastSeq, evt.Seq)
				}
				lastSeq = evt.Seq
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
		seqScheduler := sequential.NewScheduler("debug-stream", rsc.EventHandler)
		err = events.HandleRepoStream(ctx, con, seqScheduler, nil)
		if err != nil {
			return err
		}

		return nil
	},
}

var compareStreamsCmd = &cli.Command{
	Name: "compare-streams",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "host1",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "host2",
			Required: true,
		},
	},
	ArgsUsage: `<cursor>`,
	Action: func(cctx *cli.Context) error {
		h1 := cctx.String("host1")
		h2 := cctx.String("host2")

		url1 := fmt.Sprintf("%s/xrpc/com.atproto.sync.subscribeRepos", h1)
		url2 := fmt.Sprintf("%s/xrpc/com.atproto.sync.subscribeRepos", h2)

		d := websocket.DefaultDialer

		eventChans := []chan *comatproto.SyncSubscribeRepos_Commit{
			make(chan *comatproto.SyncSubscribeRepos_Commit, 2),
			make(chan *comatproto.SyncSubscribeRepos_Commit, 2),
		}

		buffers := []map[string][]*comatproto.SyncSubscribeRepos_Commit{
			make(map[string][]*comatproto.SyncSubscribeRepos_Commit),
			make(map[string][]*comatproto.SyncSubscribeRepos_Commit),
		}

		addToBuffer := func(n int, event *comatproto.SyncSubscribeRepos_Commit) {
			buffers[n][event.Repo] = append(buffers[n][event.Repo], event)
		}

		pll := func(ll *lexutil.LexLink) string {
			if ll == nil {
				return "<nil>"
			}
			return ll.String()
		}

		findMatchAndRemove := func(n int, event *comatproto.SyncSubscribeRepos_Commit) (*comatproto.SyncSubscribeRepos_Commit, error) {
			buf := buffers[n]
			slice, ok := buf[event.Repo]
			if !ok || len(slice) == 0 {
				return nil, nil
			}

			for i, ev := range slice {
				if ev.Commit == event.Commit {
					if pll(ev.Prev) != pll(event.Prev) {
						// same commit different prev??
						return nil, fmt.Errorf("matched event with same commit but different prev: (%d) %d - %d", n, ev.Seq, event.Seq)
					}
				}

				if i != 0 {
					fmt.Printf("detected skipped event: %d (%d)\n", slice[0].Seq, i)
				}

				slice = slice[i+1:]
				buf[event.Repo] = slice
				return ev, nil
			}

			return nil, fmt.Errorf("did not find matching event despite having events in buffer")
		}

		printCurrentDelta := func() {
			var a, b int
			for _, sl := range buffers[0] {
				a += len(sl)
			}
			for _, sl := range buffers[1] {
				b += len(sl)
			}

			fmt.Printf("%d %d\n", a, b)
		}

		printDetailedDelta := func() {
			for did, sl := range buffers[0] {
				osl := buffers[1][did]
				if len(osl) > 0 && len(sl) > 0 {
					fmt.Printf("%s had mismatched events on both streams (%d, %d)\n", did, len(sl), len(osl))
				}

			}
		}

		// Create two goroutines for reading events from two URLs
		for i, url := range []string{url1, url2} {
			go func(i int, url string) {
				con, _, err := d.Dial(url, http.Header{})
				if err != nil {
					log.Error("Dial failure", "i", i, "url", url, "err", err)
					os.Exit(1)
				}

				ctx := context.TODO()
				rsc := &events.RepoStreamCallbacks{
					RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
						eventChans[i] <- evt
						return nil
					},
					// TODO: all the other Repo* event types
					Error: func(evt *events.ErrorFrame) error {
						return fmt.Errorf("%s: %s", evt.Error, evt.Message)
					},
				}
				seqScheduler := sequential.NewScheduler(fmt.Sprintf("debug-stream-%d", i+1), rsc.EventHandler)
				if err := events.HandleRepoStream(ctx, con, seqScheduler, nil); err != nil {
					log.Error("HandleRepoStream failure", "i", i, "url", url, "err", err)
					os.Exit(1)
				}
			}(i, url)
		}

		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)

		// Compare events from the two URLs
		for {
			select {
			case event := <-eventChans[0]:
				partner, err := findMatchAndRemove(1, event)
				if err != nil {
					fmt.Println("checking for match failed: ", err)
					continue
				}
				if partner == nil {
					addToBuffer(0, event)
				} else {
					// the good case
					fmt.Println("Match found")
				}

			case event := <-eventChans[1]:
				partner, err := findMatchAndRemove(0, event)
				if err != nil {
					fmt.Println("checking for match failed: ", err)
					continue
				}
				if partner == nil {
					addToBuffer(1, event)
				} else {
					// the good case
					fmt.Println("Match found")
				}
			case <-ch:
				printDetailedDelta()
				/*
					b, err := json.Marshal(buffers)
					if err != nil {
						return err
					}

					fmt.Println(string(b))
				*/
				return nil
			}

			printCurrentDelta()
		}
	},
}

var debugFeedGenCmd = &cli.Command{
	Name:      "debug-feed",
	ArgsUsage: "<at-uri>",
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
var debugFeedViewCmd = &cli.Command{
	Name:  "view-feed",
	Usage: "<at-uri>",
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

		doc, err := didr.GetDocument(ctx, fgr.Did)
		if err != nil {
			return err
		}

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

		fgclient := &xrpc.Client{
			Host: ss.ServiceEndpoint,
		}

		cache, err := loadCache("postcache.json")
		if err != nil {
			return err
		}
		var cacheUpdate bool

		var cursor string
		getPage := func(curs string) ([]*bsky.FeedDefs_PostView, error) {
			skel, err := bsky.FeedGetFeedSkeleton(ctx, fgclient, cursor, uri, 30)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch feed skeleton: %w", err)
			}

			if skel.Cursor != nil {
				cursor = *skel.Cursor
			}

			var posts []*bsky.FeedDefs_PostView
			for _, fp := range skel.Feed {
				cached, ok := cache[fp.Post]
				if ok {
					posts = append(posts, cached)
					continue
				}
				fps, err := bsky.FeedGetPosts(ctx, xrpcc, []string{fp.Post})
				if err != nil {
					return nil, err
				}

				if len(fps.Posts) == 0 {
					fmt.Println("FAILED TO GET POST: ", fp.Post)
					continue
				}
				p := fps.Posts[0]
				rec := p.Record.Val.(*bsky.FeedPost)
				rec.Embed = nil // nil out embeds since they sometimes fail to json marshal...
				posts = append(posts, p)
				cache[fp.Post] = p
				cacheUpdate = true
			}

			return posts, nil
		}

		printPosts := func(posts []*bsky.FeedDefs_PostView) {
			for _, p := range posts {
				fp, ok := p.Record.Val.(*bsky.FeedPost)
				if !ok {
					fmt.Printf("ERROR: Post had invalid record type: %T\n", p.Record.Val)
					continue
				}
				text := fp.Text
				text = strings.Replace(text, "\n", " ", -1)
				if len(text) > 70 {
					text = text[:70] + "..."
				}

				dn := p.Author.Handle
				if p.Author.DisplayName != nil {
					dn = *p.Author.DisplayName
				}

				fmt.Printf("%s: %s\n", dn, text)
			}
		}

		seen := make(map[string]bool)
		for i := 1; i < 5; i++ {
			fmt.Printf("PAGE %d - cursor: %s\n", i, cursor)
			posts, err := getPage(cursor)
			if err != nil {
				return err
			}
			var alreadySeen int
			for _, p := range posts {
				if seen[p.Uri] {
					alreadySeen++
				}
				seen[p.Uri] = true
			}
			fmt.Printf("Already saw %d / %d posts in page 1\n", alreadySeen, len(posts))
			printPosts(posts)
			fmt.Println("")
			fmt.Println("")
		}

		if cacheUpdate {
			if err := saveCache("postcache.json", cache); err != nil {
				return err
			}
		}

		return nil
	},
}

func loadCache(filename string) (map[string]*bsky.FeedDefs_PostView, error) {
	var data map[string]*bsky.FeedDefs_PostView

	jsonFile, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*bsky.FeedDefs_PostView), nil
		}

		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	err = json.Unmarshal(byteValue, &data)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal json: %w", err)
	}

	return data, nil
}

func saveCache(filename string, data map[string]*bsky.FeedDefs_PostView) error {
	file, err := json.MarshalIndent(data, "", " ")
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}

	err = os.WriteFile(filename, file, 0644)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

var debugGetRepoCmd = &cli.Command{
	Name:      "get-repo",
	Flags:     []cli.Flag{},
	ArgsUsage: `<did>`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.TODO()

		repobytes, err := comatproto.SyncGetRepo(ctx, xrpcc, cctx.Args().First(), "")
		if err != nil {
			return fmt.Errorf("getting repo: %w", err)
		}

		rep, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(repobytes))
		if err != nil {
			return err
		}

		fmt.Println("Rev: ", rep.SignedCommit().Rev)
		var count int
		if err := rep.ForEach(ctx, "", func(k string, v cid.Cid) error {
			rec, err := rep.Blockstore().Get(ctx, v)
			if err != nil {
				return fmt.Errorf("getting record %q: %w", k, err)
			}

			count++
			_ = rec
			return nil
		}); err != nil {
			return err
		}
		fmt.Printf("scanned %d records\n", count)

		return nil
	},
}

var debugCompareReposCmd = &cli.Command{
	Name: "compare-repos",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "host-1",
			Usage: "method, hostname, and port of PDS instance",
			Value: "https://bsky.social",
		},
		&cli.StringFlag{
			Name:  "host-2",
			Usage: "method, hostname, and port of PDS instance",
			Value: "https://bgs.bsky.social",
		},
	},
	ArgsUsage: `<did>`,
	Action: func(cctx *cli.Context) error {
		ctx := cctx.Context
		did, err := syntax.ParseAtIdentifier(cctx.Args().First())
		if err != nil {
			return err
		}

		wg := sync.WaitGroup{}
		wg.Add(2)

		xrpc1 := xrpc.Client{
			Host: cctx.String("host-1"),
			Client: &http.Client{
				Timeout: 15 * time.Minute,
			},
		}

		if !cctx.IsSet("host-1") {
			dir := identity.DefaultDirectory()
			ident, err := dir.Lookup(ctx, *did)
			if err != nil {
				return err
			}

			xrpc1.Host = ident.PDSEndpoint()
		}

		xrpc2 := xrpc.Client{
			Host: cctx.String("host-2"),
			Client: &http.Client{
				Timeout: 15 * time.Minute,
			},
		}

		var rep1 *repo.Repo
		go func() {
			defer wg.Done()
			logger := log.With("host", cctx.String("host-1"))
			repo1bytes, err := comatproto.SyncGetRepo(ctx, &xrpc1, did.String(), "")
			if err != nil {
				logger.Error("getting repo", "err", err)
				os.Exit(1)
				return
			}

			rep1, err = repo.ReadRepoFromCar(ctx, bytes.NewReader(repo1bytes))
			if err != nil {
				logger.Error("reading repo", "err", err, "bytes", len(repo1bytes))
				os.Exit(1)
				return
			}
		}()

		var rep2 *repo.Repo
		go func() {
			defer wg.Done()
			logger := log.With("host", cctx.String("host-2"))
			repo2bytes, err := comatproto.SyncGetRepo(ctx, &xrpc2, did.String(), "")
			if err != nil {
				logger.Error("getting repo", "err", err)
				os.Exit(1)
				return
			}

			rep2, err = repo.ReadRepoFromCar(ctx, bytes.NewReader(repo2bytes))
			if err != nil {
				logger.Error("reading repo", "err", err, "bytes", len(repo2bytes))
				os.Exit(1)
				return
			}
		}()

		wg.Wait()

		cids1 := []cid.Cid{}
		blocks1 := []blocks.Block{}

		fmt.Println("Host 1 Results")
		fmt.Println("Rev: ", rep1.SignedCommit().Rev)
		var count int
		if err := rep1.ForEach(ctx, "", func(k string, v cid.Cid) error {
			cids1 = append(cids1, v)
			rec, err := rep1.Blockstore().Get(ctx, v)
			if err != nil {
				return fmt.Errorf("getting record %q: %w", k, err)
			}
			blocks1 = append(blocks1, rec)

			count++
			_ = rec
			return nil
		}); err != nil {
			return err
		}
		fmt.Printf("scanned %d records\n", count)

		cids2 := []cid.Cid{}
		blocks2 := []blocks.Block{}

		fmt.Println("\nHost 2 Results")
		fmt.Println("Rev: ", rep2.SignedCommit().Rev)
		count = 0
		if err := rep2.ForEach(ctx, "", func(k string, v cid.Cid) error {
			cids2 = append(cids2, v)
			rec, err := rep2.Blockstore().Get(ctx, v)
			if err != nil {
				return fmt.Errorf("getting record %q: %w", k, err)
			}
			blocks2 = append(blocks2, rec)

			count++
			_ = rec
			return nil
		}); err != nil {
			return err
		}
		fmt.Printf("scanned %d records\n", count)

		fmt.Println("\nComparing CIDs")
		hasBadCid := false
		for i, c1 := range cids1 {
			if c1 != cids2[i] {
				fmt.Printf("CID mismatch at index %d: %s != %s\n", i, c1, cids2[i])
				hasBadCid = true
			}
		}

		if !hasBadCid {
			fmt.Println("All CIDs match!")
		}

		fmt.Println("Comparing blocks")
		hasBadBlock := false
		for i, b1 := range blocks1 {
			if !bytes.Equal(b1.RawData(), blocks2[i].RawData()) {
				fmt.Printf("Block mismatch at index %d Host 1 Cid (%s) Host 2 Cid (%s)\n", i, b1.Cid().String(), blocks2[i].Cid().String())
				hasBadBlock = true
			}
		}

		if !hasBadBlock {
			fmt.Println("All blocks match!")
		}

		if hasBadBlock || hasBadCid {
			return fmt.Errorf("mismatched blocks or cids")
		}

		return nil
	},
}
