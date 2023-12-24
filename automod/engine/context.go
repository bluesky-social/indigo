package engine

import (
	"context"
	"log/slog"
)

// The primary interface exposed to rules.
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

type AccountContext struct {
	BaseContext

	Account AccountMeta
}

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
	DID        string // NOTE: redundant with account, but eases discoverability?
	Collection string
	RecordKey  string
	CID        *string // TODO: cid.Cid?
	Value      any
}

// TODO: in the future *may* have an IdentityContext with an IdentityOp sub-field

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
