package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/xrpc"
	cli "github.com/urfave/cli/v2"
)

var bgsAdminCmd = &cli.Command{
	Name: "bgs",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "key",
			EnvVars: []string{"BGS_ADMIN_KEY"},
		},
		&cli.StringFlag{
			Name:  "bgs",
			Value: "https://bgs.bsky-sandbox.dev",
		},
	},
	Subcommands: []*cli.Command{
		bgsListUpstreamsCmd,
		bgsKickConnectionCmd,
	},
}

var bgsListUpstreamsCmd = &cli.Command{
	Name: "list",
	Action: func(cctx *cli.Context) error {
		url := cctx.String("bgs") + "/admin/subs/getUpstreamConns"
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return err
		}

		auth := cctx.String("key")
		req.Header.Set("Authorization", "Bearer "+auth)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode != 200 {
			var e xrpc.XRPCError
			if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
				return err
			}

			return &e
		}

		var out []string
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return err
		}

		for _, h := range out {
			fmt.Println(h)
		}

		return nil
	},
}

var bgsKickConnectionCmd = &cli.Command{
	Name: "kick",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "ban",
		},
	},
	Action: func(cctx *cli.Context) error {
		uu := cctx.String("bgs") + "/admin/subs/killUpstream?host="

		uu += url.QueryEscape(cctx.Args().First())

		if cctx.Bool("ban") {
			uu += "&block=true"
		}

		req, err := http.NewRequest("POST", uu, nil)
		if err != nil {
			return err
		}

		auth := cctx.String("key")
		req.Header.Set("Authorization", "Bearer "+auth)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}

		if resp.StatusCode != 200 {
			var e xrpc.XRPCError
			if err := json.NewDecoder(resp.Body).Decode(&e); err != nil {
				return err
			}

			return &e
		}

		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return err
		}

		fmt.Println(out)

		return nil
	},
}
