// Bluesky MOderation bot (BMO), a chatops helper for slack
// For now, polls a PDS for new moderation reports and publishes notifications to slack

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/xrpc"

	logging "github.com/ipfs/go-log"
	"github.com/joho/godotenv"
	"github.com/urfave/cli/v2"
)

var log = logging.Logger("beemo")

func main() {

	// only try dotenv if it exists
	if _, err := os.Stat(".env"); err == nil {
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	run(os.Args)
}

func run(args []string) {

	app := cli.App{
		Name:  "beemo",
		Usage: "bluesky moderation reporting bot",
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "pds-host",
			Usage:   "method, hostname, and port of PDS instance",
			Value:   "http://localhost:4849",
			EnvVars: []string{"ATP_PDS_HOST"},
		},
		&cli.StringFlag{
			Name:     "handle",
			Usage:    "for PDS login",
			Required: true,
			EnvVars:  []string{"ATP_AUTH_HANDLE"},
		},
		&cli.StringFlag{
			Name:     "password",
			Usage:    "for PDS login",
			Required: true,
			EnvVars:  []string{"ATP_AUTH_PASSWORD"},
		},
		&cli.StringFlag{
			Name:     "admin-password",
			Usage:    "admin authentication password for PDS",
			Required: true,
			EnvVars:  []string{"ATP_AUTH_ADMIN_PASSWORD"},
		},
		&cli.StringFlag{
			Name: "slack-webhook-url",
			// eg: https://hooks.slack.com/services/X1234
			Usage:    "full URL of slack webhook",
			Required: true,
			EnvVars:  []string{"SLACK_WEBHOOK_URL"},
		},
		&cli.IntFlag{
			Name:    "poll-period",
			Usage:   "API poll period in seconds",
			Value:   30,
			EnvVars: []string{"POLL_PERIOD"},
		},
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "notify-reports",
			Usage:  "watch for new moderation reports, notify in slack",
			Action: pollNewReports,
		},
	}
	app.RunAndExitOnError()
}

func pollNewReports(cctx *cli.Context) error {
	// record last-seen report timestamp
	since := time.Now()
	// NOTE: uncomment this for testing
	//since = time.Now().Add(time.Duration(-12) * time.Hour)
	period := time.Duration(cctx.Int("poll-period")) * time.Second

	// create a new session
	xrpcc := &xrpc.Client{
		Client: cliutil.NewHttpClient(),
		Host:   cctx.String("pds-host"),
		Auth:   &xrpc.AuthInfo{Handle: cctx.String("handle")},
	}

	auth, err := comatproto.SessionCreate(context.TODO(), xrpcc, &comatproto.SessionCreate_Input{
		Identifier: &xrpcc.Auth.Handle,
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
	log.Infof("report polling bot starting up...")
	// can flip this bool to false to prevent spamming slack channel on startup
	if true {
		err := sendSlackMsg(cctx, fmt.Sprintf("restarted bot, monitoring for reports since `%s`...", since.Format(time.RFC3339)))
		if err != nil {
			return err
		}
	}
	for {
		// refresh session
		xrpcc.Auth.AccessJwt = xrpcc.Auth.RefreshJwt
		refresh, err := comatproto.SessionRefresh(context.TODO(), xrpcc)
		if err != nil {
			return err
		}
		xrpcc.Auth.AccessJwt = refresh.AccessJwt
		xrpcc.Auth.RefreshJwt = refresh.RefreshJwt

		// AdminGetModerationReports(ctx context.Context, c *xrpc.Client, subject *string, resolved *bool, before *string, limit *int64)
		resolved := false
		var limit int64 = 50
		mrr, err := comatproto.AdminGetModerationReports(context.TODO(), xrpcc, "", limit, resolved, "")
		if err != nil {
			return err
		}
		// this works out to iterate from newest to oldest, which is the behavior we want (report only newest, then break)
		for _, report := range mrr.Reports {
			if len(report.ResolvedByActionIds) > 0 {
				continue
			}
			createdAt, err := time.Parse(time.RFC3339, report.CreatedAt)
			if err != nil {
				return fmt.Errorf("invalid time format for 'createdAt': %w", err)
			}
			if createdAt.After(since) {
				// ok, we found a "new" report, need to notify
				msg := fmt.Sprintf("===== New moderation report received =====\n")
				msg += fmt.Sprintf("PDS: `%s`\t", cctx.String("pds-host"))
				msg += fmt.Sprintf("report id: `%d`\t", report.Id)
				msg += fmt.Sprintf("recent unresolved: `%d`\n", len(mrr.Reports))
				msg += fmt.Sprintf("createdAt: `%s`\n", report.CreatedAt)
				msg += fmt.Sprintf("reasonType: `%s`\n", *report.ReasonType)
				if report.Subject.RepoRepoRef != nil {
					msg += fmt.Sprintf("subject: `%s`\n", report.Subject.RepoRepoRef.Did)
				} else {
					msg += fmt.Sprintf("subject: `%s`\n", report.Subject.RepoStrongRef.Uri)
				}
				//msg += fmt.Sprintf("reportedByDid: `%s`\n", report.ReportedByDid)
				log.Infof("found new report, notifying slack: %s", report)
				err := sendSlackMsg(cctx, msg)
				if err != nil {
					return fmt.Errorf("failed to send slack message: %w", err)
				}
				since = createdAt
				break
			} else {
				log.Debugf("skipping report: %s", report)
			}
		}
		log.Infof("... sleeping for %s", period)
		time.Sleep(period)
	}
}

type SlackWebhookBody struct {
	Text string `json:"text"`
}

// sends a simple slack message to a channel via "incoming webhook"
// The slack incoming webhook must be already configured in the slack workplace.
func sendSlackMsg(cctx *cli.Context, msg string) error {
	// loosely based on: https://golangcode.com/send-slack-messages-without-a-library/

	webhookUrl := cctx.String("slack-webhook-url")
	body, _ := json.Marshal(SlackWebhookBody{Text: msg})
	req, err := http.NewRequest(http.MethodPost, webhookUrl, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	if resp.StatusCode != 200 || buf.String() != "ok" {
		// TODO: in some cases print body? eg, if short and text
		return fmt.Errorf("failed slack webhook POST request. status=%d", resp.StatusCode)
	}
	return nil
}
