package repo

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
)

// temporary/experimental method to parse and verify a firehose commit message.
//
// TODO: move to a separate 'sync' package? break up in to smaller components?
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

	commit, repo, err := LoadRepoFromCAR(ctx, bytes.NewReader([]byte(msg.Blocks)))
	if err != nil {
		return nil, err
	}

	if commit.Rev != rev.String() {
		return nil, fmt.Errorf("rev did not match commit")
	}
	if commit.DID != did.String() {
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
			// don't use the returned bytes, but do actually read them out of store (not just CID)
			_, val, err := repo.GetRecordBytes(ctx, nsid, rkey)
			if err != nil {
				return nil, err
			}
			if *c != *val {
				return nil, fmt.Errorf("record op doesn't match MST tree value")
			}
		}
	}

	// TODO: once firehose format is fully shipped, remove this
	for _, o := range msg.Ops {
		switch o.Action {
		case "delete":
			if o.Prev == nil {
				logger.Info("can't invert legacy op", "action", o.Action)
				return repo, nil
			}
		case "update":
			if o.Prev == nil {
				logger.Info("can't invert legacy op", "action", o.Action)
				return repo, nil
			}
		}
	}

	ops, err := parseCommitOps(msg.Ops)
	if err != nil {
		return nil, err
	}
	ops, err = NormalizeOps(ops)
	if err != nil {
		return nil, err
	}

	invTree := repo.MST.Copy()
	for _, op := range ops {
		if err := InvertOp(&invTree, &op); err != nil {
			// print the *non-inverted* tree
			//mst.DebugPrintTree(repo.MST.Root, 0)
			return nil, err
		}
	}
	computed, err := invTree.RootCID()
	if err != nil {
		return nil, err
	}
	if msg.PrevData != nil {
		c := (*cid.Cid)(msg.PrevData)
		if *computed != *c {
			return nil, fmt.Errorf("inverted tree root didn't match prevData")
		}
		logger.Debug("prevData matched", "prevData", c.String(), "computed", computed.String())
	} else {
		logger.Info("prevData was null; skipping tree root check")
	}

	return repo, nil
}

func parseCommitOps(ops []*comatproto.SyncSubscribeRepos_RepoOp) ([]Operation, error) {
	//out := make([]Operation, len(ops))
	out := []Operation{}
	for _, rop := range ops {
		switch rop.Action {
		case "create":
			if rop.Cid == nil || rop.Prev != nil {
				return nil, fmt.Errorf("invalid repoOp: create")
			}
			op := Operation{
				Path:  rop.Path,
				Prev:  nil,
				Value: (*cid.Cid)(rop.Cid),
			}
			out = append(out, op)
		case "delete":
			if rop.Cid != nil || rop.Prev == nil {
				return nil, fmt.Errorf("invalid repoOp: delete")
			}
			op := Operation{
				Path:  rop.Path,
				Prev:  (*cid.Cid)(rop.Prev),
				Value: nil,
			}
			out = append(out, op)
		case "update":
			if rop.Cid == nil || rop.Prev == nil {
				return nil, fmt.Errorf("invalid repoOp: update")
			}
			op := Operation{
				Path:  rop.Path,
				Prev:  (*cid.Cid)(rop.Prev),
				Value: (*cid.Cid)(rop.Cid),
			}
			out = append(out, op)
		default:
			return nil, fmt.Errorf("invalid repoOp action: %s", rop.Action)
		}
	}
	return out, nil
}

func VerifySyncMessage(ctx context.Context, dir identity.Directory, msg *comatproto.SyncSubscribeRepos_Sync) (*Commit, error) {
	return VerifyCommitSignatureFromCar(ctx, dir, []byte(msg.Blocks))
}

// temporary/experimental code showing how to verify a commit signature from firehose
//
// TODO: in real implementation, will want to merge this code with `VerifyCommitMessage` above, and have it hanging off some service struct with a configured `identity.Directory`
func VerifyCommitSignature(ctx context.Context, dir identity.Directory, msg *comatproto.SyncSubscribeRepos_Commit) error {
	_, err := VerifyCommitSignatureFromCar(ctx, dir, []byte(msg.Blocks))
	return err
}

func VerifyCommitSignatureFromCar(ctx context.Context, dir identity.Directory, car []byte) (*Commit, error) {
	commit, _, err := LoadCommitFromCAR(ctx, bytes.NewReader(car))
	if err != nil {
		return nil, err
	}

	did, err := syntax.ParseDID(commit.DID)
	if err != nil {
		return nil, err
	}

	ident, err := dir.LookupDID(ctx, did)
	if err != nil {
		return nil, err
	}
	pubkey, err := ident.PublicKey()
	if err != nil {
		return nil, err
	}

	err = commit.VerifySignature(pubkey)
	if err != nil {
		return nil, err
	}
	return commit, nil
}
