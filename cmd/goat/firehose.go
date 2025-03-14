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
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/identity"
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
			Name:    "quiet",
			Aliases: []string{"q"},
			Usage:   "don't actually print events to stdout (eg, errors only)",
		},
		&cli.BoolFlag{
			Name:  "verify-basic",
			Usage: "parse events and do basic syntax and structure checks",
		},
		&cli.BoolFlag{
			Name:  "verify-sig",
			Usage: "verify account signatures on commits",
		},
		&cli.BoolFlag{
			Name:  "verify-mst",
			Usage: "run inductive verification of ops and MST structure",
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
	OpsMode      bool
	AccountsOnly bool
	Quiet        bool
	VerifyBasic  bool
	VerifySig    bool
	VerifyMST    bool
	// filter to specified collections
	CollectionFilter []string
	// for signature verification
	Dir identity.Directory
}

func runFirehose(cctx *cli.Context) error {
	ctx := context.Background()

	slog.SetDefault(configLogger(cctx, os.Stderr))

	// main thing is skipping handle verification
	bdir := identity.BaseDirectory{
		SkipHandleVerification: true,
		TryAuthoritativeDNS:    false,
		SkipDNSDomainSuffixes:  []string{".bsky.social"},
		UserAgent:              "goat/" + versioninfo.Short(),
	}
	cdir := identity.NewCacheDirectory(&bdir, 1_000_000, time.Hour*24, time.Minute*2, time.Minute*5)

	gfc := GoatFirehoseConsumer{
		OpsMode:          cctx.Bool("ops"),
		AccountsOnly:     cctx.Bool("account-events"),
		CollectionFilter: cctx.StringSlice("collection"),
		Quiet:            cctx.Bool("quiet"),
		VerifyBasic:      cctx.Bool("verify-basic"),
		VerifySig:        cctx.Bool("verify-sig"),
		VerifyMST:        cctx.Bool("verify-mst"),
		Dir:              &cdir,
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
	con, _, err := dialer.Dial(urlString, http.Header{
		"User-Agent": []string{fmt.Sprintf("goat/%s", versioninfo.Short())},
	})
	if err != nil {
		return fmt.Errorf("subscribing to firehose failed (dialing): %w", err)
	}

	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			//slog.Debug("commit event", "did", evt.Repo, "seq", evt.Seq)
			if !gfc.AccountsOnly && !gfc.OpsMode {
				return gfc.handleCommitEvent(ctx, evt)
			} else if !gfc.AccountsOnly && gfc.OpsMode {
				return gfc.handleCommitEventOps(ctx, evt)
			}
			return nil
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			//slog.Debug("sync event", "did", evt.Did, "seq", evt.Seq)
			if !gfc.AccountsOnly && !gfc.OpsMode {
				return gfc.handleSyncEvent(ctx, evt)
			}
			return nil
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			//slog.Debug("identity event", "did", evt.Did, "seq", evt.Seq)
			if !gfc.OpsMode {
				return gfc.handleIdentityEvent(ctx, evt)
			}
			return nil
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			//slog.Debug("account event", "did", evt.Did, "seq", evt.Seq)
			if !gfc.OpsMode {
				return gfc.handleAccountEvent(ctx, evt)
			}
			return nil
		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			if gfc.VerifyBasic {
				slog.Info("deprecated event type", "eventType", "handle", "did", evt.Did, "seq", evt.Seq)
			}
			return nil
		},
		RepoMigrate: func(evt *comatproto.SyncSubscribeRepos_Migrate) error {
			if gfc.VerifyBasic {
				slog.Info("deprecated event type", "eventType", "migrate", "did", evt.Did, "seq", evt.Seq)
			}
			return nil
		},
		RepoTombstone: func(evt *comatproto.SyncSubscribeRepos_Tombstone) error {
			if gfc.VerifyBasic {
				slog.Info("deprecated event type", "eventType", "handle", "did", evt.Did, "seq", evt.Seq)
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
	if gfc.VerifySig {
		did, err := syntax.ParseDID(evt.Did)
		if err != nil {
			return err
		}
		gfc.Dir.Purge(ctx, did.AtIdentifier())
	}
	if gfc.VerifyBasic {
		if _, err := syntax.ParseDID(evt.Did); err != nil {
			slog.Warn("invalid DID", "eventType", "identity", "did", evt.Did, "seq", evt.Seq)
		}
	}
	if gfc.Quiet {
		return nil
	}
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
	if gfc.VerifyBasic {
		if _, err := syntax.ParseDID(evt.Did); err != nil {
			slog.Warn("invalid DID", "eventType", "account", "did", evt.Did, "seq", evt.Seq)
		}
	}
	if gfc.Quiet {
		return nil
	}
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
	if gfc.VerifyBasic {
		if err := commit.VerifyStructure(); err != nil {
			slog.Warn("bad commit object", "eventType", "sync", "did", evt.Did, "seq", evt.Seq, "err", err)
		}
		if _, err := syntax.ParseDID(evt.Did); err != nil {
			slog.Warn("invalid DID", "eventType", "account", "did", evt.Did, "seq", evt.Seq)
		}
	}
	if gfc.Quiet {
		return nil
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

	if gfc.VerifyBasic || gfc.VerifySig || gfc.VerifyMST {

		logger := slog.With("eventType", "commit", "did", evt.Repo, "seq", evt.Seq, "rev", evt.Rev)

		did, err := syntax.ParseDID(evt.Repo)
		if err != nil {
			return err
		}

		commit, err := repo.LoadCommitFromCAR(ctx, bytes.NewReader(evt.Blocks))
		if err != nil {
			return err
		}

		if gfc.VerifySig {
			ident, err := gfc.Dir.LookupDID(ctx, did)
			if err != nil {
				return err
			}
			pubkey, err := ident.PublicKey()
			if err != nil {
				return err
			}
			logger = logger.With("pds", ident.PDSEndpoint())
			if err := commit.VerifySignature(pubkey); err != nil {
				logger.Warn("commit signature validation failed", "err", err)
			}
		}

		if len(evt.Blocks) == 0 {
			logger.Warn("commit message missing blocks")
		}

		if gfc.VerifyBasic {
			// the commit itself
			if err := commit.VerifyStructure(); err != nil {
				logger.Warn("bad commit object", "err", err)
			}
			// the event fields
			rev, err := syntax.ParseTID(evt.Rev)
			if err != nil {
				logger.Warn("bad TID syntax in commit rev", "err", err)
			}
			if rev.String() != commit.Rev {
				logger.Warn("event rev != commit rev", "commitRev", commit.Rev)
			}
			if did.String() != commit.DID {
				logger.Warn("event DID != commit DID", "commitDID", commit.DID)
			}
			_, err = syntax.ParseDatetime(evt.Time)
			if err != nil {
				logger.Warn("bad datetime syntax in commit time", "time", evt.Time, "err", err)
			}
			if evt.TooBig {
				logger.Warn("deprecated tooBig commit flag set")
			}
			if evt.Rebase {
				logger.Warn("deprecated rebase commit flag set")
			}
		}

		if gfc.VerifyMST {
			if evt.PrevData == nil {
				logger.Warn("prevData is nil, skipping MST check")
			} else {
				// TODO: break out this function in to smaller chunks
				if _, err := repo.VerifyCommitMessage(ctx, evt); err != nil {
					logger.Warn("failed to invert commit MST", "err", err)
				}
			}
		}
	}

	if gfc.Quiet {
		return nil
	}

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
			if !gfc.Quiet {
				fmt.Println(string(b))
			}
		case "delete":
			out["action"] = "delete"
			b, err := json.Marshal(out)
			if err != nil {
				return err
			}
			if !gfc.Quiet {
				fmt.Println(string(b))
			}
		default:
			logger.Error("unexpected record op kind")
		}
	}
	return nil
}
