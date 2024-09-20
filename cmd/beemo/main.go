// Bluesky MOderation bot (BMO), a chatops helper for slack
// For now, polls a PDS for new moderation reports and publishes notifications to slack

package main

import (
	"io"
	"log/slog"
	"os"
	"strings"

	_ "github.com/joho/godotenv/autoload"
	_ "go.uber.org/automaxprocs"

	"github.com/carlmjohnson/versioninfo"
	"github.com/urfave/cli/v2"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(-1)
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
			Name:    "log-level",
			Usage:   "log verbosity level (eg: warn, info, debug)",
			EnvVars: []string{"BEEMO_LOG_LEVEL", "GO_LOG_LEVEL", "LOG_LEVEL"},
		},
		&cli.StringFlag{
			Name: "slack-webhook-url",
			// eg: https://hooks.slack.com/services/X1234
			Usage:    "full URL of slack webhook",
			Required: true,
			EnvVars:  []string{"SLACK_WEBHOOK_URL"},
		},
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "notify-reports",
			Usage:  "watch for new moderation reports, notify in slack",
			Action: pollNewReports,
			Flags: []cli.Flag{
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
				&cli.IntFlag{
					Name:    "poll-period",
					Usage:   "API poll period in seconds",
					Value:   30,
					EnvVars: []string{"POLL_PERIOD"},
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
			},
		},
		&cli.Command{
			Name:   "notify-mentions",
			Usage:  "watch firehose for posts mentioning specific accounts",
			Action: notifyMentions,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "relay-host",
					Usage:   "method, hostname, and port of Relay instance (websocket)",
					Value:   "wss://bsky.network",
					EnvVars: []string{"ATP_RELAY_HOST"},
				},
				&cli.StringFlag{
					Name:     "mention-dids",
					Usage:    "DIDs to look for in mentions (comma-separated)",
					Required: true,
					EnvVars:  []string{"BEEMO_MENTION_DIDS"},
				},
				&cli.IntFlag{
					Name:    "minimum-words",
					Usage:   "minimum length of post text (word count; zero for no minimum)",
					Value:   0,
					EnvVars: []string{"BEEMO_MINIMUM_WORDS"},
				},
			},
		},
	}
	return app.Run(args)
}

func configLogger(cctx *cli.Context, writer io.Writer) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cctx.String("log-level")) {
	case "error":
		level = slog.LevelError
	case "warn":
		level = slog.LevelWarn
	case "info":
		level = slog.LevelInfo
	case "debug":
		level = slog.LevelDebug
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
	return logger
}
