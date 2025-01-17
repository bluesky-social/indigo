package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/urfave/cli/v2"
)

func pollNewReports(cctx *cli.Context) error {
	ctx := context.Background()
	logger := configLogger(cctx, os.Stdout)
	slackWebhookURL := cctx.String("slack-webhook-url")

	// record last-seen report timestamp
	since := time.Now()
	// NOTE: uncomment this for testing
	//since = time.Now().Add(time.Duration(-12) * time.Hour)
	period := time.Duration(cctx.Int("poll-period")) * time.Second

	// create a new session
	xrpcc := &xrpc.Client{
		Client: util.RobustHTTPClient(),
		Host:   cctx.String("pds-host"),
		Auth:   &xrpc.AuthInfo{Handle: cctx.String("handle")},
	}

	auth, err := comatproto.ServerCreateSession(ctx, xrpcc, &comatproto.ServerCreateSession_Input{
		Identifier: xrpcc.Auth.Handle,
		Password:   cctx.String("password"),
	})
	if err != nil {
		return err
	}
	xrpcc.Auth.AccessJwt = auth.AccessJwt
	xrpcc.Auth.RefreshJwt = auth.RefreshJwt
	xrpcc.Auth.Did = auth.Did
	xrpcc.Auth.Handle = auth.Handle

	adminToken := cctx.String("admin-password")
	if len(adminToken) > 0 {
		xrpcc.AdminToken = &adminToken
	}
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
		xrpcc.Auth.AccessJwt = xrpcc.Auth.RefreshJwt
		refresh, err := comatproto.ServerRefreshSession(ctx, xrpcc)
		if err != nil {
			return err
		}
		xrpcc.Auth.AccessJwt = refresh.AccessJwt
		xrpcc.Auth.RefreshJwt = refresh.RefreshJwt

		// query just new reports (regardless of resolution state)
		var limit int64 = 50
		me, err := toolsozone.ModerationQueryEvents(
			cctx.Context,
			xrpcc,
			nil,   // addedLabels []string
			nil,   // addedTags []string
			nil,   // collections []string
			"",    // comment string
			"",    // createdAfter string
			"",    // createdBefore string
			"",    // createdBy string
			"",    // cursor string
			false, // hasComment bool
			true,  // includeAllUserRecords bool
			limit, // limit int64
			nil,   // policies []string
			nil,   // removedLabels []string
			nil,   // removedTags []string
			nil,   // reportTypes []string
			"",    // sortDirection string
			"",    // subject string
			"",    // subjectType string
			[]string{"tools.ozone.moderation.defs#modEventReport"}, // types []string
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
				msg += fmt.Sprintf("instance: `%s`\n", cctx.String("pds-host"))
				msg += fmt.Sprintf("reasonType: `%s`\t", shortType)
				msg += fmt.Sprintf("Admin: %s/reports/%d\n", cctx.String("admin-host"), evt.Id)
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
