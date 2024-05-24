// Bluesky MOderation bot (BMO), a chatops helper for slack
// For now, polls a PDS for new moderation reports and publishes notifications to slack

package main

import (
	"os"

	_ "github.com/joho/godotenv/autoload"
	_ "go.uber.org/automaxprocs"

	"github.com/carlmjohnson/versioninfo"
	logging "github.com/ipfs/go-log"
	"github.com/urfave/cli/v2"
)

var log = logging.Logger("beemo")

func main() {
	if err := run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:    "beemo",
		Usage:   "bluesky moderation reporting bot",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "pds-host",
			Usage:   "method, hostname, and port of PDS instance",
			Value:   "http://localhost:4849",
			EnvVars: []string{"ATP_PDS_HOST"},
		},
		&cli.StringFlag{
			Name:    "admin-host",
			Usage:   "method, hostname, and port of admin interface (eg, Ozone), for direct links",
			Value:   "http://localhost:3000",
			EnvVars: []string{"ATP_ADMIN_HOST"},
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
	return app.Run(args)
}

