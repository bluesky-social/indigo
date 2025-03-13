package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/parallel"
	lexutil "github.com/bluesky-social/indigo/lex/util"

	"github.com/carlmjohnson/versioninfo"
	"github.com/gorilla/websocket"
	"github.com/urfave/cli/v2"
)

var cmdFirehose = &cli.Command{
	Name:  "firehose",
	Usage: "stream repo and identity events",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "relay-host",
			Usage:   "method, hostname, and port of Relay instance (websocket)",
			Value:   "wss://bsky.network",
			EnvVars: []string{"ATP_RELAY_HOST", "RELAY_HOST"},
		},
		&cli.IntFlag{
			Name:  "cursor",
			Usage: "cursor to consume at",
		},
		&cli.StringSliceFlag{
			Name:    "collection",
			Aliases: []string{"c"},
			Usage:   "filter to specific record types (NSID)",
		},
		&cli.BoolFlag{
			Name:  "account-events",
			Usage: "only print account and identity events",
		},
		&cli.BoolFlag{
			Name:    "ops",
			Aliases: []string{"records"},
			Usage:   "instead of printing entire events, print individual record ops",
		},
	},
	Action: runFirehose,
}

type GoatFirehoseConsumer struct {
	// for pretty-printing events to stdout
	EventLogger  *slog.Logger
	OpsMode      bool
	AccountsOnly bool
	// filter to specified collections
	CollectionFilter []string
}

func runFirehose(cctx *cli.Context) error {
	ctx := context.Background()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	gfc := GoatFirehoseConsumer{
		EventLogger:      slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		OpsMode:          cctx.Bool("ops"),
		AccountsOnly:     cctx.Bool("account-events"),
		CollectionFilter: cctx.StringSlice("collection"),
	}

	var relayHost string
	if cctx.IsSet("relay-host") {
		if cctx.Args().Len() != 0 {
			return errors.New("error: unused positional args")
		}
		relayHost = cctx.String("relay-host")
	} else {
		if cctx.Args().Len() == 1 {
			relayHost = cctx.Args().First()
		} else if cctx.Args().Len() > 1 {
			return errors.New("can only have at most one relay-host")
		} else {
			relayHost = cctx.String("relay-host")
		}
	}

	dialer := websocket.DefaultDialer
	u, err := url.Parse(relayHost)
	if err != nil {
		return fmt.Errorf("invalid relayHost URI: %w", err)
	}
	u.Path = "xrpc/com.atproto.sync.subscribeRepos"
	if cctx.IsSet("cursor") {
		u.RawQuery = fmt.Sprintf("cursor=%d", cctx.Int("cursor"))
	}
	urlString := u.String()
	slog.Debug("GET", "url", urlString)
	con, _, err := dialer.Dial(urlString, http.Header{
		"User-Agent": []string{fmt.Sprintf("goat/%s", versioninfo.Short())},
	})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			slog.Debug("commit event", "did", evt.Repo, "seq", evt.Seq)
			if !gfc.AccountsOnly && !gfc.OpsMode {
				return gfc.handleCommitEvent(ctx, evt)
			} else if !gfc.AccountsOnly && gfc.OpsMode {
				return gfc.handleCommitEventOps(ctx, evt)
			}
			return nil
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			slog.Debug("sync event", "did", evt.Did, "seq", evt.Seq)
			if !gfc.AccountsOnly && !gfc.OpsMode {
				return gfc.handleSyncEvent(ctx, evt)
			}
			return nil
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			slog.Debug("identity event", "did", evt.Did, "seq", evt.Seq)
			if !gfc.OpsMode {
				return gfc.handleIdentityEvent(ctx, evt)
			}
			return nil
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			slog.Debug("account event", "did", evt.Did, "seq", evt.Seq)
			if !gfc.OpsMode {
				return gfc.handleAccountEvent(ctx, evt)
			}
			return nil
		},
	}

	scheduler := parallel.NewScheduler(
		1,
		100,
		relayHost,
		rsc.EventHandler,
	)
	slog.Info("starting firehose consumer", "relayHost", relayHost)
	return events.HandleRepoStream(ctx, con, scheduler, nil)
}

func (gfc *GoatFirehoseConsumer) handleIdentityEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Identity) error {
	out := make(map[string]interface{})
	out["type"] = "identity"
	out["payload"] = evt
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func (gfc *GoatFirehoseConsumer) handleAccountEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Account) error {
	out := make(map[string]interface{})
	out["type"] = "account"
	out["payload"] = evt
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func (gfc *GoatFirehoseConsumer) handleSyncEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Sync) error {
	commit, err := repo.LoadCommitFromCAR(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		return err
	}
	evt.Blocks = nil
	out := make(map[string]interface{})
	out["type"] = "sync"
	out["commit"] = commit.AsData()
	out["payload"] = evt
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// this is the simple version, when not in "records" mode: print the event as JSON, but don't include blocks
func (gfc *GoatFirehoseConsumer) handleCommitEvent(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {

	// apply collections filter
	if len(gfc.CollectionFilter) > 0 {
		keep := false
		for _, op := range evt.Ops {
			parts := strings.SplitN(op.Path, "/", 3)
			if len(parts) != 2 {
				slog.Error("invalid record path", "path", op.Path)
				return nil
			}
			collection := parts[0]
			for _, c := range gfc.CollectionFilter {
				if c == collection {
					keep = true
					break
				}
			}
			if keep == true {
				break
			}
		}
		if !keep {
			return nil
		}
	}

	evt.Blocks = nil
	out := make(map[string]interface{})
	out["type"] = "commit"
	out["payload"] = evt
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func (gfc *GoatFirehoseConsumer) handleCommitEventOps(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit) error {
	logger := slog.With("event", "commit", "did", evt.Repo, "rev", evt.Rev, "seq", evt.Seq)

	if evt.TooBig {
		logger.Warn("skipping tooBig events for now")
		return nil
	}

	_, rr, err := repo.LoadFromCAR(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		logger.Error("failed to read repo from car", "err", err)
		return nil
	}

	for _, op := range evt.Ops {
		collection, rkey, err := syntax.ParseRepoPath(op.Path)
		if err != nil {
			logger.Error("invalid path in repo op", "eventKind", op.Action, "path", op.Path)
			return nil
		}
		logger = logger.With("eventKind", op.Action, "collection", collection, "rkey", rkey)

		if len(gfc.CollectionFilter) > 0 {
			keep := false
			for _, c := range gfc.CollectionFilter {
				if collection.String() == c {
					keep = true
					break
				}
			}
			if keep == false {
				continue
			}
		}

		out := make(map[string]interface{})
		out["seq"] = evt.Seq
		out["rev"] = evt.Rev
		out["time"] = evt.Time
		out["collection"] = collection
		out["rkey"] = rkey

		switch op.Action {
		case "create", "update":
			coll, rkey, err := syntax.ParseRepoPath(op.Path)
			if err != nil {
				return err
			}
			// read the record bytes from blocks, and verify CID
			recBytes, rc, err := rr.GetRecordBytes(ctx, coll, rkey)
			if err != nil {
				logger.Error("reading record from event blocks (CAR)", "err", err)
				break
			}
			if op.Cid == nil || lexutil.LexLink(*rc) != *op.Cid {
				logger.Error("mismatch between commit op CID and record block", "recordCID", rc, "opCID", op.Cid)
				break
			}

			out["action"] = op.Action
			d, err := data.UnmarshalCBOR(recBytes)
			if err != nil {
				slog.Warn("failed to parse record CBOR")
				continue
			}
			out["cid"] = op.Cid.String()
			out["record"] = d
			b, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
		case "delete":
			out["action"] = "delete"
			b, err := json.Marshal(out)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
		default:
			logger.Error("unexpected record op kind")
		}
	}
	return nil
}
