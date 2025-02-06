package repo

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/repo/mst"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
)

// temporary/experimental method to parse and verify a firehose commit message
func VerifyCommitMessage(ctx context.Context, msg *comatproto.SyncSubscribeRepos_Commit) (*Repo, error) {

	logger := slog.Default().With("did", msg.Repo, "rev", msg.Rev, "seq", msg.Seq, "time", msg.Time)

	did, err := syntax.ParseDID(msg.Repo)
	if err != nil {
		return nil, err
	}
	rev, err := syntax.ParseTID(msg.Rev)
	if err != nil {
		return nil, err
	}
	_, err = syntax.ParseDatetime(msg.Time)
	if err != nil {
		return nil, err
	}

	if msg.TooBig {
		logger.Warn("event with tooBig flag set")
	}
	if msg.Rebase {
		logger.Warn("event with rebase flag set")
	}

	repo, err := LoadFromCAR(ctx, bytes.NewReader([]byte(msg.Blocks)))
	if err != nil {
		return nil, err
	}

	if repo.Commit.Rev != rev.String() {
		return nil, fmt.Errorf("rev did not match commit")
	}
	if repo.Commit.DID != did.String() {
		return nil, fmt.Errorf("rev did not match commit")
	}
	// TODO: check that commit CID matches root? re-compute?

	// load out all the records
	for _, op := range msg.Ops {
		if (op.Action == "create" || op.Action == "update") && op.Cid != nil {
			c := (*cid.Cid)(op.Cid)
			nsid, rkey, err := syntax.ParseRepoPath(op.Path)
			if err != nil {
				return nil, fmt.Errorf("invalid repo path in ops list: %w", err)
			}
			val, err := repo.GetRecordCID(ctx, nsid, rkey)
			if err != nil {
				return nil, err
			}
			if *c != *val {
				return nil, fmt.Errorf("record op doesn't match MST tree value")
			}
			_, err = repo.GetRecordBytes(ctx, nsid, rkey)
			if err != nil {
				return nil, err
			}
		}
	}

	// TODO: once firehose format is updated, remove this
	for _, o := range msg.Ops {
		if o.Action != "create" {
			logger.Info("can't invert legacy op", "action", o.Action)
			return repo, nil
		}
	}

	ops, err := ParseCommitOps(msg.Ops)
	if err != nil {
		return nil, err
	}
	ops, err = mst.NormalizeOps(ops)
	if err != nil {
		return nil, err
	}

	invTree := repo.MST.Copy()
	for _, op := range ops {
		if err := mst.InvertOp(&invTree, &op); err != nil {
			// print the *non-inverted* tree
			//mst.DebugPrintTree(repo.MST.Root, 0)
			return nil, err
		}
	}
	// TODO: compare against previous commit for this repo?
	_, err = invTree.RootCID()

	logger.Info("success")
	return repo, nil
}

func ParseCommitOps(ops []*comatproto.SyncSubscribeRepos_RepoOp) ([]mst.Operation, error) {
	//out := make([]mst.Operation, len(ops))
	out := []mst.Operation{}
	for _, rop := range ops {
		switch rop.Action {
		case "create":
			if rop.Cid != nil {
				op := mst.Operation{
					Path:  rop.Path,
					Prev:  nil,
					Value: (*cid.Cid)(rop.Cid),
				}
				out = append(out, op)
			} else {
				return nil, fmt.Errorf("invalid repoOp: create missing CID")
			}
		case "delete":
			return nil, fmt.Errorf("unhandled delete repoOp")
		case "update":
			return nil, fmt.Errorf("unhandled update repoOp")
		default:
			return nil, fmt.Errorf("invalid repoOp action: %s", rop.Action)
		}
	}
	return out, nil
}
