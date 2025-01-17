package engine

import (
	"context"
	"fmt"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/xrpc"
)

func dedupeLabelActions(labels, existing, existingNegated []string) []string {
	newLabels := []string{}
	for _, val := range dedupeStrings(labels) {
		exists := false
		for _, e := range existingNegated {
			if val == e {
				exists = true
				break
			}
		}
		for _, e := range existing {
			if val == e {
				exists = true
				break
			}
		}
		if !exists {
			newLabels = append(newLabels, val)
		}
	}
	return newLabels
}

func dedupeTagActions(tags, existing []string) []string {
	newTags := []string{}
	for _, val := range dedupeStrings(tags) {
		exists := false
		for _, e := range existing {
			if val == e {
				exists = true
				break
			}
		}
		if !exists {
			newTags = append(newTags, val)
		}
	}
	return newTags
}

func dedupeFlagActions(flags, existing []string) []string {
	newFlags := []string{}
	for _, val := range dedupeStrings(flags) {
		exists := false
		for _, e := range existing {
			if val == e {
				exists = true
				break
			}
		}
		if !exists {
			newFlags = append(newFlags, val)
		}
	}
	return newFlags
}

func (eng *Engine) dedupeReportActions(ctx context.Context, subject string, reports []ModReport) ([]ModReport, error) {
	newReports := []ModReport{}
	for _, r := range reports {
		counterName := "automod-account-report-" + ReasonShortName(r.ReasonType)
		existing, err := eng.Counters.GetCount(ctx, counterName, subject, countstore.PeriodDay)
		if err != nil {
			return nil, fmt.Errorf("checking report de-dupe counts: %w", err)
		}
		if existing > 0 {
			eng.Logger.Debug("skipping account report due to counter", "existing", existing, "reason", ReasonShortName(r.ReasonType))
		} else {
			err = eng.Counters.Increment(ctx, counterName, subject)
			if err != nil {
				return nil, fmt.Errorf("incrementing report de-dupe count: %w", err)
			}
			newReports = append(newReports, r)
		}
	}
	return newReports, nil
}

func (eng *Engine) circuitBreakReports(ctx context.Context, reports []ModReport) ([]ModReport, error) {
	if len(reports) == 0 {
		return []ModReport{}, nil
	}
	c, err := eng.Counters.GetCount(ctx, "automod-quota", "report", countstore.PeriodDay)
	if err != nil {
		return nil, fmt.Errorf("checking report action quota: %w", err)
	}

	quotaModReportDay := eng.Config.QuotaModReportDay
	if quotaModReportDay == 0 {
		quotaModReportDay = 10000
	}
	if c >= quotaModReportDay {
		eng.Logger.Warn("CIRCUIT BREAKER: automod reports")
		return []ModReport{}, nil
	}
	err = eng.Counters.Increment(ctx, "automod-quota", "report")
	if err != nil {
		return nil, fmt.Errorf("incrementing report action quota: %w", err)
	}
	return reports, nil
}

func (eng *Engine) circuitBreakTakedown(ctx context.Context, takedown bool) (bool, error) {
	if !takedown {
		return false, nil
	}
	c, err := eng.Counters.GetCount(ctx, "automod-quota", "takedown", countstore.PeriodDay)
	if err != nil {
		return false, fmt.Errorf("checking takedown action quota: %w", err)
	}
	quotaModTakedownDay := eng.Config.QuotaModTakedownDay
	if quotaModTakedownDay == 0 {
		quotaModTakedownDay = 200
	}
	if c >= quotaModTakedownDay {
		eng.Logger.Warn("CIRCUIT BREAKER: automod takedowns")
		return false, nil
	}
	err = eng.Counters.Increment(ctx, "automod-quota", "takedown")
	if err != nil {
		return false, fmt.Errorf("incrementing takedown action quota: %w", err)
	}
	return takedown, nil
}

// Combined circuit breaker for miscellaneous mod actions like: escalate, acknowledge
func (eng *Engine) circuitBreakModAction(ctx context.Context, action bool) (bool, error) {
	if !action {
		return false, nil
	}
	c, err := eng.Counters.GetCount(ctx, "automod-quota", "mod-action", countstore.PeriodDay)
	if err != nil {
		return false, fmt.Errorf("checking mod action quota: %w", err)
	}
	quotaModActionDay := eng.Config.QuotaModActionDay
	if quotaModActionDay == 0 {
		quotaModActionDay = 2000
	}
	if c >= quotaModActionDay {
		eng.Logger.Warn("CIRCUIT BREAKER: automod action")
		return false, nil
	}
	err = eng.Counters.Increment(ctx, "automod-quota", "mod-action")
	if err != nil {
		return false, fmt.Errorf("incrementing mod action quota: %w", err)
	}
	return action, nil
}

// Creates a moderation report, but checks first if there was a similar recent one, and skips if so.
//
// Returns a bool indicating if a new report was created.
func (eng *Engine) createReportIfFresh(ctx context.Context, xrpcc *xrpc.Client, did syntax.DID, mr ModReport) (bool, error) {
	// before creating a report, query to see if automod has already reported this account in the past week for the same reason
	// NOTE: this is running in an inner loop (if there are multiple reports), which is a bit inefficient, but seems acceptable

	resp, err := toolsozone.ModerationQueryEvents(
		ctx,
		xrpcc,
		nil,            // addedLabels []string
		nil,            // addedTags []string
		nil,            // collections []string
		"",             // comment string
		"",             // createdAfter string
		"",             // createdBefore string
		xrpcc.Auth.Did, // createdBy string
		"",             // cursor string
		false,          // hasComment bool
		false,          // includeAllUserRecords bool
		5,              // limit int64
		nil,            // policies []string
		nil,            // removedLabels []string
		nil,            // removedTags []string
		nil,            // reportTypes []string
		"",             // sortDirection string
		did.String(),   // subject string
		"",             // subjectType string
		[]string{"tools.ozone.moderation.defs#modEventReport"}, // types []string
	)

	if err != nil {
		return false, err
	}
	for _, modEvt := range resp.Events {
		// defensively ensure that our query params worked correctly
		if modEvt.Event.ModerationDefs_ModEventReport == nil || modEvt.CreatedBy != xrpcc.Auth.Did || modEvt.Subject.AdminDefs_RepoRef == nil || modEvt.Subject.AdminDefs_RepoRef.Did != did.String() || (modEvt.Event.ModerationDefs_ModEventReport.ReportType != nil && *modEvt.Event.ModerationDefs_ModEventReport.ReportType != mr.ReasonType) {
			continue
		}
		// igonre if older
		created, err := syntax.ParseDatetime(modEvt.CreatedAt)
		if err != nil {
			return false, err
		}
		reportDupePeriod := eng.Config.ReportDupePeriod
		if reportDupePeriod == 0 {
			reportDupePeriod = 1 * 24 * time.Hour
		}
		if time.Since(created.Time()) > reportDupePeriod {
			continue
		}

		// there is a recent report which is similar to this one
		eng.Logger.Info("skipping duplicate account report due to API check")
		return false, nil
	}

	eng.Logger.Info("reporting account", "reasonType", mr.ReasonType, "comment", mr.Comment)
	actionNewReportCount.WithLabelValues("account").Inc()
	comment := "[automod] " + mr.Comment
	_, err = toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
		CreatedBy: xrpcc.Auth.Did,
		Event: &toolsozone.ModerationEmitEvent_Input_Event{
			ModerationDefs_ModEventReport: &toolsozone.ModerationDefs_ModEventReport{
				Comment:    &comment,
				ReportType: &mr.ReasonType,
			},
		},
		Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
			AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
				Did: did.String(),
			},
		},
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// Create a moderation report, but checks first if there was a similar recent one, and skips if so.
//
// Returns a bool indicating if a new report was created.
//
// TODO: merge this with createReportIfFresh()
func (eng *Engine) createRecordReportIfFresh(ctx context.Context, xrpcc *xrpc.Client, uri syntax.ATURI, cid *syntax.CID, mr ModReport) (bool, error) {
	// before creating a report, query to see if automod has already reported this account in the past week for the same reason
	// NOTE: this is running in an inner loop (if there are multiple reports), which is a bit inefficient, but seems acceptable

	resp, err := toolsozone.ModerationQueryEvents(
		ctx,
		xrpcc,
		nil,            // addedLabels []string
		nil,            // addedTags []string
		nil,            // collections []string
		"",             // comment string
		"",             // createdAfter string
		"",             // createdBefore string
		xrpcc.Auth.Did, // createdBy string
		"",             // cursor string
		false,          // hasComment bool
		false,          // includeAllUserRecords bool
		5,              // limit int64
		nil,            // policies []string
		nil,            // removedLabels []string
		nil,            // removedTags []string
		nil,            // reportTypes []string
		"",             // sortDirection string
		uri.String(),   // subject string
		"",             // subjectType string
		[]string{"tools.ozone.moderation.defs#modEventReport"}, // types []string
	)
	if err != nil {
		return false, err
	}
	for _, modEvt := range resp.Events {
		// defensively ensure that our query params worked correctly
		if modEvt.Event.ModerationDefs_ModEventReport == nil || modEvt.CreatedBy != xrpcc.Auth.Did || modEvt.Subject.RepoStrongRef == nil || modEvt.Subject.RepoStrongRef.Uri != uri.String() || (modEvt.Event.ModerationDefs_ModEventReport.ReportType != nil && *modEvt.Event.ModerationDefs_ModEventReport.ReportType != mr.ReasonType) {
			continue
		}
		// igonre if older
		created, err := syntax.ParseDatetime(modEvt.CreatedAt)
		if err != nil {
			return false, err
		}
		reportDupePeriod := eng.Config.ReportDupePeriod
		if reportDupePeriod == 0 {
			reportDupePeriod = 1 * 24 * time.Hour
		}
		if time.Since(created.Time()) > reportDupePeriod {
			continue
		}

		// there is a recent report which is similar to this one
		eng.Logger.Info("skipping duplicate account report due to API check")
		return false, nil
	}

	eng.Logger.Info("reporting record", "reasonType", mr.ReasonType, "comment", mr.Comment)
	actionNewReportCount.WithLabelValues("record").Inc()
	comment := "[automod] " + mr.Comment
	_, err = toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
		CreatedBy: xrpcc.Auth.Did,
		Event: &toolsozone.ModerationEmitEvent_Input_Event{
			ModerationDefs_ModEventReport: &toolsozone.ModerationDefs_ModEventReport{
				Comment:    &comment,
				ReportType: &mr.ReasonType,
			},
		},
		Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
			RepoStrongRef: &comatproto.RepoStrongRef{
				Uri: uri.String(),
				Cid: cid.String(),
			},
		},
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
