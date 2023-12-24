package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/countstore"
	"github.com/bluesky-social/indigo/automod/util"
	"github.com/bluesky-social/indigo/xrpc"
)

func dedupeLabelActions(labels, existing, existingNegated []string) []string {
	newLabels := []string{}
	for _, val := range util.DedupeStrings(labels) {
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

func dedupeFlagActions(flags, existing []string) []string {
	newFlags := []string{}
	for _, val := range util.DedupeStrings(flags) {
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

// REVIEW: this does does both reads and then mutations of the planned effect, rather than just returning things, which neither the name nor signiture clearly suggests.
func (eng *Engine) dedupeReportActions(evt *RepoEvent, eff *Effects, reports []ModReport) []ModReport {
	newReports := []ModReport{}
	for _, r := range reports {
		counterName := "automod-account-report-" + ReasonShortName(r.ReasonType)
		existing, err := eng.GetCount(counterName, evt.Account.Identity.DID.String(), countstore.PeriodDay)
		if err != nil {
			panic(err) // XXX
		}
		if existing > 0 {
			eng.Logger.Debug("skipping account report due to counter", "existing", existing, "reason", ReasonShortName(r.ReasonType))
		} else {
			eff.Increment(counterName, evt.Account.Identity.DID.String())
			newReports = append(newReports, r)
		}
	}
	return newReports
}

func (eng *Engine) circuitBreakReports(eff *Effects, reports []ModReport) []ModReport {
	if len(reports) == 0 {
		return []ModReport{}
	}
	c, err := eng.GetCount("automod-quota", "report", countstore.PeriodDay)
	if err != nil {
		panic(err) // XXX
	}
	if c >= QuotaModReportDay {
		eng.Logger.Warn("CIRCUIT BREAKER: automod reports")
		return []ModReport{}
	}
	eff.Increment("automod-quota", "report") // REVIEW: should this increment just happen directly on the engine?  it's not part of the relatively pure rule application logic, and we just had to read the engine again for it, so, maybe?
	return reports
}

func (eng *Engine) circuitBreakTakedown(eff *Effects, takedown bool) bool {
	if !takedown {
		return takedown
	}
	c, err := eng.GetCount("automod-quota", "takedown", countstore.PeriodDay)
	if err != nil {
		panic(err) // XXX
	}
	if c >= QuotaModTakedownDay {
		eng.Logger.Warn("CIRCUIT BREAKER: automod takedowns")
		return false
	}
	eff.Increment("automod-quota", "takedown") // REVIEW: same question as above about if this could just as apply directly to the engine state.
	return takedown
}

// Creates a moderation report, but checks first if there was a similar recent one, and skips if so.
//
// Returns a bool indicating if a new report was created.
func (eng *Engine) createReportIfFresh(ctx context.Context, xrpcc *xrpc.Client, evt *RepoEvent, mr ModReport) (bool, error) {
	// before creating a report, query to see if automod has already reported this account in the past week for the same reason
	// NOTE: this is running in an inner loop (if there are multiple reports), which is a bit inefficient, but seems acceptable

	// AdminQueryModerationEvents(ctx context.Context, c *xrpc.Client, createdBy string, cursor string, inc ludeAllUserRecords bool, limit int64, sortDirection string, subject string, types []string)
	resp, err := comatproto.AdminQueryModerationEvents(ctx, xrpcc, xrpcc.Auth.Did, "", false, 5, "", evt.Account.Identity.DID.String(), []string{"com.atproto.admin.defs#modEventReport"})
	if err != nil {
		return false, err
	}
	for _, modEvt := range resp.Events {
		// defensively ensure that our query params worked correctly
		if modEvt.Event.AdminDefs_ModEventReport == nil || modEvt.CreatedBy != xrpcc.Auth.Did || modEvt.Subject.AdminDefs_RepoRef == nil || modEvt.Subject.AdminDefs_RepoRef.Did != evt.Account.Identity.DID.String() || (modEvt.Event.AdminDefs_ModEventReport.ReportType != nil && *modEvt.Event.AdminDefs_ModEventReport.ReportType != mr.ReasonType) {
			continue
		}
		// igonre if older
		created, err := syntax.ParseDatetime(modEvt.CreatedAt)
		if err != nil {
			return false, err
		}
		if time.Since(created.Time()) > ReportDupePeriod {
			continue
		}

		// there is a recent report which is similar to this one
		eng.Logger.Info("skipping duplicate account report due to API check")
		return false, nil
	}

	eng.Logger.Info("reporting account", "reasonType", mr.ReasonType, "comment", mr.Comment)
	_, err = comatproto.ModerationCreateReport(ctx, xrpcc, &comatproto.ModerationCreateReport_Input{
		ReasonType: &mr.ReasonType,
		Reason:     &mr.Comment,
		Subject: &comatproto.ModerationCreateReport_Input_Subject{
			AdminDefs_RepoRef: &comatproto.AdminDefs_RepoRef{
				Did: evt.Account.Identity.DID.String(),
			},
		},
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

func slackBody(header string, acct AccountMeta, newLabels, newFlags []string, newReports []ModReport, newTakedown bool) string {
	msg := header
	msg += fmt.Sprintf("`%s` / `%s` / <https://bsky.app/profile/%s|bsky> / <https://admin.prod.bsky.dev/repositories/%s|ozone>\n",
		acct.Identity.DID,
		acct.Identity.Handle,
		acct.Identity.DID,
		acct.Identity.DID,
	)
	if len(newLabels) > 0 {
		msg += fmt.Sprintf("New Labels: `%s`\n", strings.Join(newLabels, ", "))
	}
	if len(newFlags) > 0 {
		msg += fmt.Sprintf("New Flags: `%s`\n", strings.Join(newFlags, ", "))
	}
	for _, rep := range newReports {
		msg += fmt.Sprintf("Report `%s`: %s\n", rep.ReasonType, rep.Comment)
	}
	if newTakedown {
		msg += fmt.Sprintf("Takedown!\n")
	}
	return msg
}
