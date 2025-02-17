package repo

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/repo/mst"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/ipfs/go-cid"
)

type CommitVerifier interface {
	VerifyCommit(ctx context.Context, commit *Commit) error
}

// temporary/experimental method to parse and verify a firehose commit message
func VerifyCommitMessage(ctx context.Context, msg *comatproto.SyncSubscribeRepos_Commit, verifier CommitVerifier) (*Repo, error) {

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

	commit, repo, err := LoadFromCAR(ctx, bytes.NewReader([]byte(msg.Blocks)))
	if err != nil {
		return nil, err
	}

	if commit.Rev != rev.String() {
		return nil, fmt.Errorf("rev did not match commit")
	}
	if commit.DID != did.String() {
		return nil, fmt.Errorf("rev did not match commit")
	}

	if verifier != nil {
		err = verifier.VerifyCommit(ctx, commit)
		if err != nil {
			return nil, err
		}
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

	logger.Info("success")
	return repo, nil
}

func ParseCommitOps(ops []*comatproto.SyncSubscribeRepos_RepoOp) ([]mst.Operation, error) {
	//out := make([]mst.Operation, len(ops))
	out := []mst.Operation{}
	for _, rop := range ops {
		switch rop.Action {
		case "create":
			if rop.Cid == nil || rop.Prev != nil {
				return nil, fmt.Errorf("invalid repoOp: create")
			}
			op := mst.Operation{
				Path:  rop.Path,
				Prev:  nil,
				Value: (*cid.Cid)(rop.Cid),
			}
			out = append(out, op)
		case "delete":
			if rop.Cid != nil || rop.Prev == nil {
				return nil, fmt.Errorf("invalid repoOp: delete")
			}
			op := mst.Operation{
				Path:  rop.Path,
				Prev:  (*cid.Cid)(rop.Prev),
				Value: nil,
			}
			out = append(out, op)
		case "update":
			if rop.Cid == nil || rop.Prev == nil {
				return nil, fmt.Errorf("invalid repoOp: update")
			}
			op := mst.Operation{
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

// DidDirectory the part of identity.Directory that we need
type DidDirectory interface {
	LookupDID(ctx context.Context, d syntax.DID) (*identity.Identity, error)
}

type basicCommitVerifier struct {
	directory DidDirectory
}

func (cv *basicCommitVerifier) VerifyCommit(ctx context.Context, commit *Commit) error {
	xdid, err := syntax.ParseDID(commit.DID)
	if err != nil {
		return fmt.Errorf("bad car DID, %w", err)
	}
	ident, err := cv.directory.LookupDID(ctx, xdid)
	if err != nil {
		// TODO: optionally allow not-found conditions to pass without signature check
		return fmt.Errorf("DID lookup failed, %w", err)
	}
	pk, err := ident.GetPublicKey("atproto")
	if err != nil {
		return fmt.Errorf("no atproto pubkey, %w", err)
	}
	err = commit.VerifySignature(pk)
	if err != nil {
		// TODO: if the DID document was stale, force re-fetch from source and re-try if pubkey has changed
		return fmt.Errorf("invalid signature, %w", err)
	}
	return nil
}

// NewBasicCommitVerifier creates a CommitVerifier which validates commit signatures using the atproto pubkey found in the DidDirectory ( aka identity.Directory )
// basicCommitVerifier.VerifyCommit() should probably be copy-paste extended by application code with more complex policies about signature verification, timeouts, retries, missing data, etc.
func NewBasicCommitVerifier(directory DidDirectory) CommitVerifier {
	return &basicCommitVerifier{directory: directory}
}
