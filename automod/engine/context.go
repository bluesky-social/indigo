package engine

import (
	"context"
	"fmt"
	"log/slog"

	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// The primary interface exposed to rules. All other contexts derive from this "base" struct.
type BaseContext struct {
	// Actual golang "context.Context", if needed for timeouts etc
	Ctx context.Context
	// Any errors encountered while processing methods on this struct (or sub-types) get rolled up in this nullable field
	Err error
	// slog logger handle, with event-specific structured fields pre-populated. Pointer, but expected to never be nil.
	Logger *slog.Logger

	engine  *Engine // NOTE: pointer, but expected never to be nil
	effects *Effects
}

// Both a useful context on it's own (eg, for identity events), and extended by other context types.
type AccountContext struct {
	BaseContext

	Account AccountMeta
}

// Represents a repository operation on a single record: create, update, delete, etc.
type RecordContext struct {
	AccountContext

	RecordOp RecordOp
	// TODO: could consider adding commit-level metadata here. probably nullable if so, commit-level metadata isn't always available. might be best to do a separate event/context type for that
}

// Represents an ozone event on a subject account.
//
// TODO: for ozone events with a record subject (not account subject), should we extend RecordContext instead?
type OzoneEventContext struct {
	AccountContext

	Event OzoneEvent

	// Moderator team member (for ozone internal events) or account that created a report or appeal
	CreatorAccount AccountMeta

	// If the subject of the event is a record, this is the record metadata
	SubjectRecord *RecordMeta
}

var (
	CreateOp = "create"
	UpdateOp = "update"
	DeleteOp = "delete"
)

// Immutable
type RecordOp struct {
	// Indicates type of record mutation: create, update, or delete.
	// The term "action" is copied from com.atproto.sync.subscribeRepos#repoOp
	Action     string
	DID        syntax.DID
	Collection syntax.NSID
	RecordKey  syntax.RecordKey
	CID        *syntax.CID
	RecordCBOR []byte
}

// Immutable
type RecordMeta struct {
	DID        syntax.DID
	Collection syntax.NSID
	RecordKey  syntax.RecordKey
	CID        *syntax.CID
	// TODO: RecordCBOR []byte? optional?
}

type OzoneEvent struct {
	EventType  string
	EventID    int64
	CreatedAt  syntax.Datetime
	CreatedBy  syntax.DID
	SubjectDID syntax.DID
	SubjectURI *syntax.ATURI
	// TODO: SubjectBlobs []syntax.CID
	Event toolsozone.ModerationDefs_ModEventView_Event
}

// Checks that op has expected fields, based on the action type
func (op *RecordOp) Validate() error {
	switch op.Action {
	case CreateOp, UpdateOp:
		if op.RecordCBOR == nil || op.CID == nil {
			return fmt.Errorf("expected record create/update op to contain both value and CID")
		}
	case DeleteOp:
		if op.RecordCBOR != nil || op.CID != nil {
			return fmt.Errorf("expected record delete op to be empty")
		}
	default:
		return fmt.Errorf("unexpected record op action: %s", op.Action)
	}
	return nil
}

func (op *RecordOp) ATURI() syntax.ATURI {
	return syntax.ATURI(fmt.Sprintf("at://%s/%s/%s", op.DID, op.Collection, op.RecordKey))
}

// TODO: in the future *may* have an IdentityContext with an IdentityOp sub-field

// Access to engine's identity directory (without access to other engine fields)
func (c *BaseContext) Directory() identity.Directory {
	return c.engine.Directory
}

// request external state via engine (indirect)
func (c *BaseContext) GetCount(name, val, period string) int {
	out, err := c.engine.Counters.GetCount(c.Ctx, name, val, period)
	if err != nil {
		if nil == c.Err {
			c.Err = err
		}
		return 0
	}
	return out
}

func (c *BaseContext) GetCountDistinct(name, bucket, period string) int {
	out, err := c.engine.Counters.GetCountDistinct(c.Ctx, name, bucket, period)
	if err != nil {
		if nil == c.Err {
			c.Err = err
		}
		return 0
	}
	return out
}

func (c *BaseContext) InSet(name, val string) bool {
	out, err := c.engine.Sets.InSet(c.Ctx, name, val)
	if err != nil {
		if nil == c.Err {
			c.Err = err
		}
		return false
	}
	return out
}

// Returns a pointer to the underlying automod engine. This usually should NOT be used in rules.
//
// This is an escape hatch for hacking on the system before features get fully integerated in to the content API surface. The Engine API is not stable.
func (c *BaseContext) InternalEngine() *Engine {
	return c.engine
}

func NewAccountContext(ctx context.Context, eng *Engine, meta AccountMeta) AccountContext {
	return AccountContext{
		BaseContext: BaseContext{
			Ctx:     ctx,
			Err:     nil,
			Logger:  eng.Logger.With("did", meta.Identity.DID),
			engine:  eng,
			effects: &Effects{},
		},
		Account: meta,
	}
}

func NewRecordContext(ctx context.Context, eng *Engine, meta AccountMeta, op RecordOp) RecordContext {
	ac := NewAccountContext(ctx, eng, meta)
	ac.BaseContext.Logger = ac.BaseContext.Logger.With("collection", op.Collection, "rkey", op.RecordKey)
	return RecordContext{
		AccountContext: ac,
		RecordOp:       op,
	}
}

// fetch relationship metadata between this account and another account
func (c *AccountContext) GetAccountRelationship(other syntax.DID) AccountRelationship {
	rel, err := c.engine.GetAccountRelationship(c.Ctx, c.Account.Identity.DID, other)
	if err != nil {
		if nil == c.Err {
			c.Err = err
		}
		return AccountRelationship{DID: other}
	}
	return *rel
}

// fetch account metadata for the given DID. if there is any problem with lookup, returns nil.
//
// TODO: should this take an AtIdentifier instead?
func (c *BaseContext) GetAccountMeta(did syntax.DID) *AccountMeta {

	ident, err := c.engine.Directory.LookupDID(c.Ctx, did)
	if err != nil {
		if nil == c.Err {
			c.Err = err
		}
		return nil
	}
	am, err := c.engine.GetAccountMeta(c.Ctx, ident)
	if err != nil {
		if nil == c.Err {
			c.Err = err
		}
		return nil
	}
	return am
}

// update effects (indirect) ======

func (c *BaseContext) Increment(name, val string) {
	c.effects.Increment(name, val)
}

func (c *BaseContext) IncrementDistinct(name, bucket, val string) {
	c.effects.IncrementDistinct(name, bucket, val)
}

func (c *BaseContext) IncrementPeriod(name, val string, period string) {
	c.effects.IncrementPeriod(name, val, period)
}

func (c *BaseContext) Notify(srv string) {
	c.effects.Notify(srv)
}

func (c *AccountContext) AddAccountFlag(val string) {
	c.effects.AddAccountFlag(val)
}

func (c *AccountContext) AddAccountLabel(val string) {
	c.effects.AddAccountLabel(val)
}

func (c *AccountContext) AddAccountTag(val string) {
	c.effects.AddAccountTag(val)
}

func (c *AccountContext) ReportAccount(reason, comment string) {
	c.effects.ReportAccount(reason, comment)
}

func (c *AccountContext) TakedownAccount() {
	c.effects.TakedownAccount()
}

func (c *AccountContext) EscalateAccount() {
	c.effects.EscalateAccount()
}

func (c *AccountContext) AcknowledgeAccount() {
	c.effects.AcknowledgeAccount()
}

func (c *RecordContext) AddRecordFlag(val string) {
	c.effects.AddRecordFlag(val)
}

func (c *RecordContext) AddRecordLabel(val string) {
	c.effects.AddRecordLabel(val)
}

func (c *RecordContext) AddRecordTag(val string) {
	c.effects.AddRecordTag(val)
}

func (c *RecordContext) ReportRecord(reason, comment string) {
	c.effects.ReportRecord(reason, comment)
}

func (c *RecordContext) TakedownRecord() {
	c.effects.TakedownRecord()
}

func (c *RecordContext) EscalateRecord() {
	c.effects.EscalateRecord()
}

func (c *RecordContext) AcknowledgeRecord() {
	c.effects.AcknowledgeRecord()
}

func (c *RecordContext) TakedownBlob(cid string) {
	c.effects.TakedownBlob(cid)
}
