package bgs

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	atrepo "github.com/bluesky-social/indigo/atproto/repo"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/cmd/relayered/models"
	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel"
)

const defaultMaxRevFuture = time.Hour

func NewValidator(directory identity.Directory, inductionTraceLog *slog.Logger) *Validator {
	maxRevFuture := defaultMaxRevFuture // TODO: configurable
	ErrRevTooFarFuture := fmt.Errorf("new rev is > %s in the future", maxRevFuture)

	return &Validator{
		userLocks:         make(map[models.Uid]*userLock),
		log:               slog.Default().With("system", "validator"),
		inductionTraceLog: inductionTraceLog,
		directory:         directory,

		maxRevFuture:           maxRevFuture,
		ErrRevTooFarFuture:     ErrRevTooFarFuture,
		AllowSignatureNotFound: true, // TODO: configurable
	}
}

// Validator contains the context and code necessary to validate #commit and #sync messages
type Validator struct {
	lklk      sync.Mutex
	userLocks map[models.Uid]*userLock

	log               *slog.Logger
	inductionTraceLog *slog.Logger

	directory identity.Directory

	// maxRevFuture is added to time.Now() for a limit of clock skew we'll accept a `rev` in the future for
	maxRevFuture time.Duration

	// ErrRevTooFarFuture is the error we return
	// held here because we fmt.Errorf() once with our configured maxRevFuture into the message
	ErrRevTooFarFuture error

	// AllowSignatureNotFound enables counting messages without findable public key to pass through with a warning counter
	// TODO: refine this for what kind of 'not found' we accept.
	AllowSignatureNotFound bool
}

type NextCommitHandler interface {
	HandleCommit(ctx context.Context, host *models.PDS, uid models.Uid, did string, commit *atproto.SyncSubscribeRepos_Commit) error
}

type userLock struct {
	lk      sync.Mutex
	waiters atomic.Int32
}

// lockUser re-serializes access per-user after events may have been fanned out to many worker threads by events/schedulers/parallel
func (val *Validator) lockUser(ctx context.Context, user models.Uid) func() {
	ctx, span := otel.Tracer("validator").Start(ctx, "userLock")
	defer span.End()

	val.lklk.Lock()

	ulk, ok := val.userLocks[user]
	if !ok {
		ulk = &userLock{}
		val.userLocks[user] = ulk
	}

	ulk.waiters.Add(1)

	val.lklk.Unlock()

	ulk.lk.Lock()

	return func() {
		val.lklk.Lock()
		defer val.lklk.Unlock()

		ulk.lk.Unlock()

		nv := ulk.waiters.Add(-1)

		if nv == 0 {
			delete(val.userLocks, user)
		}
	}
}

func (val *Validator) HandleCommit(ctx context.Context, host *models.PDS, account *Account, commit *atproto.SyncSubscribeRepos_Commit, prevRoot *AccountPreviousState) (newRoot *cid.Cid, err error) {
	uid := account.GetUid()
	unlock := val.lockUser(ctx, uid)
	defer unlock()
	repoFragment, err := val.VerifyCommitMessage(ctx, host, commit, prevRoot)
	if err != nil {
		return nil, err
	}
	newRootCid, err := repoFragment.MST.RootCID()
	if err != nil {
		return nil, err
	}
	return newRootCid, nil
}

type revOutOfOrderError struct {
	dt time.Duration
}

func (roooe *revOutOfOrderError) Error() string {
	return fmt.Sprintf("new rev is before previous rev by %s", roooe.dt.String())
}

var ErrNewRevBeforePrevRev = &revOutOfOrderError{}

func (val *Validator) VerifyCommitMessage(ctx context.Context, host *models.PDS, msg *atproto.SyncSubscribeRepos_Commit, prevRoot *AccountPreviousState) (*atrepo.Repo, error) {
	hostname := host.Host
	hasWarning := false
	commitVerifyStarts.Inc()
	logger := slog.Default().With("did", msg.Repo, "rev", msg.Rev, "seq", msg.Seq, "time", msg.Time)

	did, err := syntax.ParseDID(msg.Repo)
	if err != nil {
		commitVerifyErrors.WithLabelValues(hostname, "did").Inc()
		return nil, err
	}
	rev, err := syntax.ParseTID(msg.Rev)
	if err != nil {
		commitVerifyErrors.WithLabelValues(hostname, "tid").Inc()
		return nil, err
	}
	if prevRoot != nil {
		prevRev := prevRoot.GetRev()
		curTime := rev.Time()
		prevTime := prevRev.Time()
		if curTime.Before(prevTime) {
			commitVerifyErrors.WithLabelValues(hostname, "revb").Inc()
			dt := prevTime.Sub(curTime)
			return nil, &revOutOfOrderError{dt}
		}
	}
	if rev.Time().After(time.Now().Add(val.maxRevFuture)) {
		commitVerifyErrors.WithLabelValues(hostname, "revf").Inc()
		return nil, val.ErrRevTooFarFuture
	}
	_, err = syntax.ParseDatetime(msg.Time)
	if err != nil {
		commitVerifyErrors.WithLabelValues(hostname, "time").Inc()
		return nil, err
	}

	if msg.TooBig {
		//logger.Warn("event with tooBig flag set")
		commitVerifyWarnings.WithLabelValues(hostname, "big").Inc()
		val.inductionTraceLog.Warn("commit tooBig", "seq", msg.Seq, "pdsHost", host.Host, "repo", msg.Repo)
		hasWarning = true
	}
	if msg.Rebase {
		//logger.Warn("event with rebase flag set")
		commitVerifyWarnings.WithLabelValues(hostname, "reb").Inc()
		val.inductionTraceLog.Warn("commit rebase", "seq", msg.Seq, "pdsHost", host.Host, "repo", msg.Repo)
		hasWarning = true
	}

	commit, repoFragment, err := atrepo.LoadRepoFromCAR(ctx, bytes.NewReader([]byte(msg.Blocks)))
	if err != nil {
		commitVerifyErrors.WithLabelValues(hostname, "car").Inc()
		return nil, err
	}

	if commit.Rev != rev.String() {
		commitVerifyErrors.WithLabelValues(hostname, "rev").Inc()
		return nil, fmt.Errorf("rev did not match commit")
	}
	if commit.DID != did.String() {
		commitVerifyErrors.WithLabelValues(hostname, "did2").Inc()
		return nil, fmt.Errorf("rev did not match commit")
	}

	err = val.VerifyCommitSignature(ctx, commit, hostname, &hasWarning)
	if err != nil {
		// signature errors are metrics counted inside VerifyCommitSignature()
		return nil, err
	}

	// load out all the records
	for _, op := range msg.Ops {
		if (op.Action == "create" || op.Action == "update") && op.Cid != nil {
			c := (*cid.Cid)(op.Cid)
			nsid, rkey, err := syntax.ParseRepoPath(op.Path)
			if err != nil {
				commitVerifyErrors.WithLabelValues(hostname, "opp").Inc()
				return nil, fmt.Errorf("invalid repo path in ops list: %w", err)
			}
			val, err := repoFragment.GetRecordCID(ctx, nsid, rkey)
			if err != nil {
				commitVerifyErrors.WithLabelValues(hostname, "rcid").Inc()
				return nil, err
			}
			if *c != *val {
				commitVerifyErrors.WithLabelValues(hostname, "opc").Inc()
				return nil, fmt.Errorf("record op doesn't match MST tree value")
			}
			_, _, err = repoFragment.GetRecordBytes(ctx, nsid, rkey)
			if err != nil {
				commitVerifyErrors.WithLabelValues(hostname, "rec").Inc()
				return nil, err
			}
		}
	}

	// TODO: once firehose format is fully shipped, remove this
	for _, o := range msg.Ops {
		switch o.Action {
		case "delete":
			if o.Prev == nil {
				logger.Debug("can't invert legacy op", "action", o.Action)
				val.inductionTraceLog.Warn("commit delete op", "seq", msg.Seq, "pdsHost", host.Host, "repo", msg.Repo)
				commitVerifyOkish.WithLabelValues(hostname, "del").Inc()
				return repoFragment, nil
			}
		case "update":
			if o.Prev == nil {
				logger.Debug("can't invert legacy op", "action", o.Action)
				val.inductionTraceLog.Warn("commit update op", "seq", msg.Seq, "pdsHost", host.Host, "repo", msg.Repo)
				commitVerifyOkish.WithLabelValues(hostname, "up").Inc()
				return repoFragment, nil
			}
		}
	}

	if msg.PrevData != nil {
		c := (*cid.Cid)(msg.PrevData)
		if prevRoot != nil {
			if *c != prevRoot.GetCid() {
				commitVerifyWarnings.WithLabelValues(hostname, "pr").Inc()
				val.inductionTraceLog.Warn("commit prevData mismatch", "seq", msg.Seq, "pdsHost", host.Host, "repo", msg.Repo)
				hasWarning = true
			}
		} else {
			// see counter below for okish "new"
		}

		// check internal consistency that claimed previous root matches the rest of this message
		ops, err := ParseCommitOps(msg.Ops)
		if err != nil {
			commitVerifyErrors.WithLabelValues(hostname, "pop").Inc()
			return nil, err
		}
		ops, err = atrepo.NormalizeOps(ops)
		if err != nil {
			commitVerifyErrors.WithLabelValues(hostname, "nop").Inc()
			return nil, err
		}

		invTree := repoFragment.MST.Copy()
		for _, op := range ops {
			if err := atrepo.InvertOp(&invTree, &op); err != nil {
				commitVerifyErrors.WithLabelValues(hostname, "inv").Inc()
				return nil, err
			}
		}
		computed, err := invTree.RootCID()
		if err != nil {
			commitVerifyErrors.WithLabelValues(hostname, "it").Inc()
			return nil, err
		}
		if *computed != *c {
			// this is self-inconsistent malformed data
			commitVerifyErrors.WithLabelValues(hostname, "pd").Inc()
			return nil, fmt.Errorf("inverted tree root didn't match prevData")
		}
		//logger.Debug("prevData matched", "prevData", c.String(), "computed", computed.String())

		if prevRoot == nil {
			commitVerifyOkish.WithLabelValues(hostname, "new").Inc()
		} else if hasWarning {
			commitVerifyOkish.WithLabelValues(hostname, "warn").Inc()
		} else {
			// TODO: would it be better to make everything "okish"?
			// commitVerifyOkish.WithLabelValues(hostname, "ok").Inc()
			commitVerifyOk.WithLabelValues(hostname).Inc()
		}
	} else {
		// this source is still on old protocol without new prevData field
		commitVerifyOkish.WithLabelValues(hostname, "old").Inc()
	}

	return repoFragment, nil
}

// HandleSync checks signed commit from a #sync message
func (val *Validator) HandleSync(ctx context.Context, host *models.PDS, msg *atproto.SyncSubscribeRepos_Sync) (newRoot *cid.Cid, err error) {
	hostname := host.Host
	hasWarning := false

	did, err := syntax.ParseDID(msg.Did)
	if err != nil {
		syncVerifyErrors.WithLabelValues(hostname, "did").Inc()
		return nil, err
	}
	rev, err := syntax.ParseTID(msg.Rev)
	if err != nil {
		syncVerifyErrors.WithLabelValues(hostname, "tid").Inc()
		return nil, err
	}
	if rev.Time().After(time.Now().Add(val.maxRevFuture)) {
		syncVerifyErrors.WithLabelValues(hostname, "revf").Inc()
		return nil, val.ErrRevTooFarFuture
	}
	_, err = syntax.ParseDatetime(msg.Time)
	if err != nil {
		syncVerifyErrors.WithLabelValues(hostname, "time").Inc()
		return nil, err
	}

	commit, _, err := atrepo.LoadCommitFromCAR(ctx, bytes.NewReader([]byte(msg.Blocks)))
	if err != nil {
		commitVerifyErrors.WithLabelValues(hostname, "car").Inc()
		return nil, err
	}

	if commit.Rev != rev.String() {
		commitVerifyErrors.WithLabelValues(hostname, "rev").Inc()
		return nil, fmt.Errorf("rev did not match commit")
	}
	if commit.DID != did.String() {
		commitVerifyErrors.WithLabelValues(hostname, "did2").Inc()
		return nil, fmt.Errorf("rev did not match commit")
	}

	err = val.VerifyCommitSignature(ctx, commit, hostname, &hasWarning)
	if err != nil {
		// signature errors are metrics counted inside VerifyCommitSignature()
		return nil, err
	}

	return &commit.Data, nil
}

// TODO: lift back to indigo/atproto/repo util code?
func ParseCommitOps(ops []*atproto.SyncSubscribeRepos_RepoOp) ([]atrepo.Operation, error) {
	out := []atrepo.Operation{}
	for _, rop := range ops {
		switch rop.Action {
		case "create":
			if rop.Cid == nil || rop.Prev != nil {
				return nil, fmt.Errorf("invalid repoOp: create")
			}
			op := atrepo.Operation{
				Path:  rop.Path,
				Prev:  nil,
				Value: (*cid.Cid)(rop.Cid),
			}
			out = append(out, op)
		case "delete":
			if rop.Cid != nil || rop.Prev == nil {
				return nil, fmt.Errorf("invalid repoOp: delete")
			}
			op := atrepo.Operation{
				Path:  rop.Path,
				Prev:  (*cid.Cid)(rop.Prev),
				Value: nil,
			}
			out = append(out, op)
		case "update":
			if rop.Cid == nil || rop.Prev == nil {
				return nil, fmt.Errorf("invalid repoOp: update")
			}
			op := atrepo.Operation{
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

// VerifyCommitSignature get's repo's registered public key from Identity Directory, verifies Commit
// hostname is just for metrics in case of error
func (val *Validator) VerifyCommitSignature(ctx context.Context, commit *atrepo.Commit, hostname string, hasWarning *bool) error {
	if val.directory == nil {
		return nil
	}
	xdid, err := syntax.ParseDID(commit.DID)
	if err != nil {
		commitVerifyErrors.WithLabelValues(hostname, "sig1").Inc()
		return fmt.Errorf("bad car DID, %w", err)
	}
	ident, err := val.directory.LookupDID(ctx, xdid)
	if err != nil {
		if val.AllowSignatureNotFound {
			// allow not-found conditions to pass without signature check
			commitVerifyWarnings.WithLabelValues(hostname, "nok").Inc()
			if hasWarning != nil {
				*hasWarning = true
			}
			return nil
		}
		commitVerifyErrors.WithLabelValues(hostname, "sig2").Inc()
		return fmt.Errorf("DID lookup failed, %w", err)
	}
	pk, err := ident.GetPublicKey("atproto")
	if err != nil {
		commitVerifyErrors.WithLabelValues(hostname, "sig3").Inc()
		return fmt.Errorf("no atproto pubkey, %w", err)
	}
	err = commit.VerifySignature(pk)
	if err != nil {
		// TODO: if the DID document was stale, force re-fetch from source and re-try if pubkey has changed
		commitVerifyErrors.WithLabelValues(hostname, "sig4").Inc()
		return fmt.Errorf("invalid signature, %w", err)
	}
	return nil
}
