package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/util"

	"github.com/urfave/cli/v3"
)

func pollNewReports(ctx context.Context, cmd *cli.Command) error {
	logger := configLogger(cmd, os.Stdout)
	slackWebhookURL := cmd.String("slack-webhook-url")

	// record last-seen report timestamp
	since := time.Now()
	// NOTE: uncomment this for testing
	//since = time.Now().Add(time.Duration(-12) * time.Hour)
	period := time.Duration(cmd.Int("poll-period")) * time.Second

	// create a new session
	xrpcc := atclient.NewAPIClient(cmd.String("pds-host"))
	xrpcc.Client = util.RobustHTTPClient()

	auth, err := comatproto.ServerCreateSession(ctx, xrpcc, &comatproto.ServerCreateSession_Input{
		Identifier: cmd.String("handle"),
		Password:   cmd.String("password"),
	})
	if err != nil {
		return err
	}
	did, err := syntax.ParseDID(auth.Did)
	if err != nil {
		return err
	}
	xrpcc.Auth = &atclient.PasswordAuth{
		Session: atclient.PasswordSessionData{
			AccessToken:  auth.AccessJwt,
			RefreshToken: auth.RefreshJwt,
			AccountDID:   did,
			Host:         cmd.String("pds-host"),
		},
	}
	xrpcc.AccountDID = &did
	logger.Info("report polling bot starting up...")
	// can flip this bool to false to prevent spamming slack channel on startup
	if true {
		err := sendSlackMsg(ctx, fmt.Sprintf("restarted bot, monitoring for reports since `%s`...", since.Format(time.RFC3339)), slackWebhookURL)
		if err != nil {
			return err
		}
	}
	for {
		// refresh session
		pauth := xrpcc.Auth.(*atclient.PasswordAuth)
		pauth.Session.AccessToken = pauth.Session.RefreshToken
		refresh, err := comatproto.ServerRefreshSession(ctx, xrpcc)
		if err != nil {
			return err
		}
		pauth.Session.AccessToken = refresh.AccessJwt
		pauth.Session.RefreshToken = refresh.RefreshJwt

		// query just new reports (regardless of resolution state)
		var limit int64 = 50
		me, err := toolsozone.ModerationQueryEvents(
			ctx,
			xrpcc,
			nil,   // addedLabels []string
			nil,   // addedTags []string
			"",    // ageAssuranceState
			"",    // batchId string
			nil,   // collections []string
			"",    // comment string
			"",    // createdAfter string
			"",    // createdBefore string
			"",    // createdBy string
			"",    // cursor string
			false, // hasComment bool
			true,  // includeAllUserRecords bool
			limit, // limit int64
			nil,   // modTool
			nil,   // policies []string
			nil,   // removedLabels []string
			nil,   // removedTags []string
			nil,   // reportTypes []string
			"",    // sortDirection string
			"",    // subject string
			"",    // subjectType string
			[]string{"tools.ozone.moderation.defs#modEventReport"}, // types []string
			false, // withStrike bool
		)
		if err != nil {
			return err
		}
		// this works out to iterate from newest to oldest, which is the behavior we want (report only newest, then break)
		for _, evt := range me.Events {
			report := evt.Event.ModerationDefs_ModEventReport
			// TODO: filter out based on subject state? similar to old "report.ResolvedByActionIds"
			createdAt, err := time.Parse(time.RFC3339, evt.CreatedAt)
			if err != nil {
				return fmt.Errorf("invalid time format for 'createdAt': %w", err)
			}
			if createdAt.After(since) {
				shortType := ""
				if report.ReportType != nil && strings.Contains(*report.ReportType, "#") {
					shortType = strings.SplitN(*report.ReportType, "#", 2)[1]
				}
				// ok, we found a "new" report, need to notify
				msg := fmt.Sprintf("⚠️ New report at `%s` ⚠️\n", evt.CreatedAt)
				msg += fmt.Sprintf("report id: `%d`\t", evt.Id)
				msg += fmt.Sprintf("instance: `%s`\n", cmd.String("pds-host"))
				msg += fmt.Sprintf("reasonType: `%s`\t", shortType)
				msg += fmt.Sprintf("Admin: %s/reports/%d\n", cmd.String("admin-host"), evt.Id)
				//msg += fmt.Sprintf("reportedByDid: `%s`\n", report.ReportedByDid)
				logger.Info("found new report, notifying slack", "report", report)
				err := sendSlackMsg(ctx, msg, slackWebhookURL)
				if err != nil {
					return fmt.Errorf("failed to send slack message: %w", err)
				}
				since = createdAt
				break
			} else {
				logger.Debug("skipping report", "report", report)
			}
		}
		logger.Info("... sleeping", "period", period)
		time.Sleep(period)
	}
}
