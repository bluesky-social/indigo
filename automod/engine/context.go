package engine

import (
	"context"
	"fmt"
	"log/slog"

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
	effects Effects
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
	// NOTE: usually a *pointer*, not the value itself
	Value any
}

// Originally intended for push notifications, but can also work for any inter-account notification.
type NotificationContext struct {
	AccountContext

	Recipient    AccountMeta
	Notification NotificationMeta
}

// Additional notification metadata, with fields aligning with the `app.bsky.notification.listNotifications` Lexicon schemas
type NotificationMeta struct {
	// Expected values are 'like', 'repost', 'follow', 'mention', 'reply', and 'quote'; arbitrary values may be added in the future.
	Reason string
	// The content (atproto record) which was the cause of this notification. Could be a post with a mention, or a like, follow, or repost record.
	Subject syntax.ATURI
}

// Checks that op has expected fields, based on the action type
func (op *RecordOp) Validate() error {
	switch op.Action {
	case CreateOp, UpdateOp:
		if op.Value == nil || op.CID == nil {
			return fmt.Errorf("expected record create/update op to contain both value and CID")
		}
	case DeleteOp:
		if op.Value != nil || op.CID != nil {
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

func NewAccountContext(ctx context.Context, eng *Engine, meta AccountMeta) AccountContext {
	return AccountContext{
		BaseContext: BaseContext{
			Ctx:     ctx,
			Err:     nil,
			Logger:  eng.Logger.With("did", meta.Identity.DID),
			engine:  eng,
			effects: Effects{},
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

func NewNotificationContext(ctx context.Context, eng *Engine, sender, recipient AccountMeta, reason string, subject syntax.ATURI) NotificationContext {
	ac := NewAccountContext(ctx, eng, sender)
	ac.BaseContext.Logger = ac.BaseContext.Logger.With("recipient", recipient.Identity.DID, "reason", reason, "subject", subject.String())
	return NotificationContext{
		AccountContext: ac,
		Recipient:      recipient,
		Notification: NotificationMeta{
			Reason:  reason,
			Subject: subject,
		},
	}
}

// update effects (indirect)
func (c *BaseContext) Increment(name, val string) {
	c.effects.Increment(name, val)
}

func (c *BaseContext) IncrementDistinct(name, bucket, val string) {
	c.effects.IncrementDistinct(name, bucket, val)
}

func (c *BaseContext) IncrementPeriod(name, val string, period string) {
	c.effects.IncrementPeriod(name, val, period)
}

func (c *AccountContext) AddAccountFlag(val string) {
	c.effects.AddAccountFlag(val)
}

func (c *AccountContext) AddAccountLabel(val string) {
	c.effects.AddAccountLabel(val)
}

func (c *AccountContext) ReportAccount(reason, comment string) {
	c.effects.ReportAccount(reason, comment)
}

func (c *AccountContext) TakedownAccount() {
	c.effects.TakedownAccount()
}

func (c *RecordContext) AddRecordFlag(val string) {
	c.effects.AddRecordFlag(val)
}

func (c *RecordContext) AddRecordLabel(val string) {
	c.effects.AddRecordLabel(val)
}

func (c *RecordContext) ReportRecord(reason, comment string) {
	c.effects.ReportRecord(reason, comment)
}

func (c *RecordContext) TakedownRecord() {
	c.effects.TakedownRecord()
}

func (c *RecordContext) TakedownBlob(cid string) {
	c.effects.TakedownBlob(cid)
}

func (c *NotificationContext) Reject() {
	c.effects.Reject()
}
