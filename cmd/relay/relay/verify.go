package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relay/relay/models"
)

var (
	ErrFutureRev   = errors.New("commit revision in the future")
	ErrRevSequence = errors.New("commit revision out of order")
)

const futureRevTolerance = time.Minute * 5
const MaxMessageBlocksBytes = 2_000_000
const MaxCommitOps = 200

// High-level entrypoint for verifying #commit messages.
//
// Always verifies: loading commit and repo; field syntax; commit signature; future rev
//
// Strict verification: use of deprecated fields; MST inversion; all ops present in blocks
//
// Does not check: account/host matching; host-level sequence; account-level rev ordering; DID syntax
//
// `ident` arg may be nil (if resolution failed)
// `prevRepo` arg represents previous state, and is optional/nullable.
// `hostname` arg is piped through just for logging, not for validating account/host match
// returns an AccountRepo with empty UID, containing metadata about *this* commit
func (r *Relay) VerifyRepoCommit(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit, ident *identity.Identity, prevRepo *models.AccountRepo, hostname string) (*models.AccountRepo, error) {
	logger := r.Logger.With("host", hostname, "did", evt.Repo, "rev", evt.Rev)

	if len(evt.Blocks) > MaxMessageBlocksBytes {
		return nil, fmt.Errorf("blocks size (%d bytes) exceeds protocol limit", len(evt.Blocks))
	}

	if len(evt.Ops) > MaxCommitOps {
		return nil, fmt.Errorf("too many ops in commit: %d", len(evt.Ops))
	}

	// even in lenient/legacy mode (eg, tooBig), we need to verify commit
	commit, commitCID, err := repo.LoadCommitFromCAR(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		return nil, err
	}

	if err := r.VerifyCommitObject(ctx, commit, ident, hostname); err != nil {
		return nil, err
	}

	// consistency between event fields and commit fields
	if evt.Repo != commit.DID {
		return nil, fmt.Errorf("mismatched inner commit DID field: %s", commit.DID)
	}
	if evt.Rev != commit.Rev {
		return nil, fmt.Errorf("mismatched inner commit rev field: %s", commit.Rev)
	}

	err = r.VerifyCommitMessageStrict(ctx, evt, commit, prevRepo, hostname)
	if err != nil {
		if r.Config.LenientSyncValidation {
			logger.Warn("allowing commit message which failed strict validation", "problem", err)
		} else {
			return nil, err
		}
	}

	resp := models.AccountRepo{
		Rev:        commit.Rev,
		CommitCID:  commitCID.String(),
		CommitData: commit.Data.String(),
	}
	return &resp, nil
}

// the parts of basic verification which are common between #commit and #sync messages
func (r *Relay) VerifyCommitObject(ctx context.Context, commit *repo.Commit, ident *identity.Identity, hostname string) error {
	logger := r.Logger.With("host", hostname, "did", commit.DID, "rev", commit.Rev)

	// `VerifyStructure` checks that commit object field syntax is correct
	if err := commit.VerifyStructure(); err != nil {
		return err
	}

	// this re-parse is technically duplicate work
	rev, err := syntax.ParseTID(commit.Rev)
	if err != nil {
		return fmt.Errorf("commit rev syntax: %w", err)
	}
	if rev.Time().Compare(time.Now().Add(futureRevTolerance)) > 0 {
		return fmt.Errorf("%w: %s: %s", ErrFutureRev, rev, rev.Time().String())
	}

	// if identity is available, verify the signature
	if ident != nil {
		// NOTE: may eventually want to cache cryptographic key parsing
		pubkey, err := ident.PublicKey()
		if err != nil {
			return fmt.Errorf("commit verification: %w", err)
		}

		if err := commit.VerifySignature(pubkey); err != nil {
			return fmt.Errorf("commit verification: %w", err)
		}
	} else {
		logger.Warn("skipping commit signature validation", "reason", "ident unavailable")
	}
	return nil
}

func (r *Relay) VerifyCommitMessageStrict(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Commit, commit *repo.Commit, prevRepo *models.AccountRepo, hostname string) error {

	logger := r.Logger.With("host", hostname, "did", commit.DID, "rev", commit.Rev)

	// first check things which would skip MST inversion entirely
	if len(evt.Blocks) == 0 {
		return fmt.Errorf("commit messaging missing blocks")
	}
	if evt.TooBig {
		return fmt.Errorf("deprecated tooBig commit flag set")
	}
	if evt.PrevData == nil {
		return fmt.Errorf("missing prevData field")
	}
	if prevRepo != nil {
		if evt.PrevData.String() != prevRepo.CommitData {
			logger.Warn("commit with miss-matching prevData", "prevData", evt.PrevData, "prevRepo.CommitData", prevRepo.CommitData)
		}
		if evt.Since != nil && *evt.Since != prevRepo.Rev {
			logger.Warn("commit with miss-matching since", "since", evt.Since, "prevRepo.Rev", prevRepo.Rev)
		}
		if evt.Rev <= prevRepo.Rev {
			return fmt.Errorf("%w: %s before or equal to %s", ErrRevSequence, evt.Rev, prevRepo.Rev)
		}
	}

	// this parse is redundant with earlier parse; once lenient mode is removed we should do only a single pass
	origRepo, _, err := repo.LoadRepoFromCAR(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		return err
	}

	// TODO: break out this function in to smaller chunks
	_ = origRepo
	if _, err := repo.VerifyCommitMessage(ctx, evt); err != nil {
		logger.Warn("failed to invert commit MST", "err", err)
	}

	// finally less-important checks
	if evt.Rebase {
		return fmt.Errorf("deprecated rebase commit flag set")
	}
	_, err = syntax.ParseDatetime(evt.Time)
	if err != nil {
		return fmt.Errorf("commit timestamp syntax: %w", err)
	}
	return nil
}

// High-level entrypoint for verifying #sync messages.
//
// Always verifies: loading commit and repo; field syntax; commit signature; future rev
//
// Does not check: account/host matching; host-level sequence; account-level rev ordering; DID syntax
//
// `ident` arg may be nil (if resolution failed)
// `hostname` arg is piped through just for logging, not for validating account/host match
// returns an AccountRepo with empty UID, containing metadata about *this* commit
func (r *Relay) VerifyRepoSync(ctx context.Context, evt *comatproto.SyncSubscribeRepos_Sync, ident *identity.Identity, hostname string) (*models.AccountRepo, error) {
	//logger := r.Logger.With("host", hostname, "did", evt.Did, "rev", evt.Rev)

	if len(evt.Blocks) > MaxMessageBlocksBytes {
		return nil, fmt.Errorf("blocks size (%d bytes) exeeds protocol limit", len(evt.Blocks))
	}

	// even in lenient/legacy mode (eg, tooBig), we need to verify commit
	commit, commitCID, err := repo.LoadCommitFromCAR(ctx, bytes.NewReader(evt.Blocks))
	if err != nil {
		return nil, err
	}

	if err := r.VerifyCommitObject(ctx, commit, ident, hostname); err != nil {
		return nil, err
	}

	// consistency between event fields and commit fields
	if evt.Did != commit.DID {
		return nil, fmt.Errorf("mismatched inner commit DID field: %s", commit.DID)
	}
	if evt.Rev != commit.Rev {
		return nil, fmt.Errorf("mismatched inner commit rev field: %s", commit.Rev)
	}

	resp := models.AccountRepo{
		Rev:        commit.Rev,
		CommitCID:  commitCID.String(),
		CommitData: commit.Data.String(),
	}
	return &resp, nil
}
