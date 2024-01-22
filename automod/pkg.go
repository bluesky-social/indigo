package automod

import (
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/engine"
)

type Engine = engine.Engine
type AccountMeta = engine.AccountMeta
type RuleSet = engine.RuleSet

type AccountContext = engine.AccountContext
type RecordContext = engine.RecordContext
type RecordOp = engine.RecordOp

type IdentityRuleFunc = engine.IdentityRuleFunc
type RecordRuleFunc = engine.RecordRuleFunc
type PostRuleFunc = engine.PostRuleFunc
type ProfileRuleFunc = engine.ProfileRuleFunc
type BlobRuleFunc = engine.BlobRuleFunc
type NotificationRuleFunc = engine.NotificationRuleFunc

var (
	ReportReasonSpam       = engine.ReportReasonSpam
	ReportReasonViolation  = engine.ReportReasonViolation
	ReportReasonMisleading = engine.ReportReasonMisleading
	ReportReasonSexual     = engine.ReportReasonSexual
	ReportReasonRude       = engine.ReportReasonRude
	ReportReasonOther      = engine.ReportReasonOther

	PeriodTotal = countstore.PeriodTotal
	PeriodDay   = countstore.PeriodDay
	PeriodHour  = countstore.PeriodHour

	CreateOp = engine.CreateOp
	UpdateOp = engine.UpdateOp
	DeleteOp = engine.DeleteOp
)
