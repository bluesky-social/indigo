package main

import (
	"os"

	_ "github.com/joho/godotenv/autoload"

	logging "github.com/ipfs/go-log"
	"github.com/urfave/cli/v2"
)

var log = logging.Logger("pubweb")

func init() {
	logging.SetAllLoggers(logging.LevelDebug)
	//logging.SetAllLoggers(logging.LevelWarn)
}

func main() {
	run(os.Args)
}

func run(args []string) {

	app := cli.App{
		Name:  "pubweb",
		Usage: "public web interface to bluesky content",
	}

	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "serve",
			Usage:  "run the server",
			Action: serve,
			Flags: []cli.Flag{
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
					Name:     "http-address",
					Usage:    "Specify the local IP/port to bind to",
					Required: false,
					Value:    ":8200",
					EnvVars:  []string{"PUBWEB_BIND"},
				},
				&cli.BoolFlag{
					Name:     "debug",
					Usage:    "Enable debug mode",
					Value:    false,
					Required: false,
					EnvVars:  []string{"DEBUG"},
				},
			},
		},
	}
	app.RunAndExitOnError()
}
