package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/bluesky-social/indigo/xrpc"
	cli "github.com/urfave/cli/v2"
)

var bgsAdminCmd = &cli.Command{
	Name:  "bgs",
	Usage: "sub-commands for administering a BGS",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "key",
			EnvVars: []string{"BGS_ADMIN_KEY"},
		},
		&cli.StringFlag{
			Name:  "bgs",
			Value: "http://localhost:2470",
		},
	},
	Subcommands: []*cli.Command{
		bgsListUpstreamsCmd,
		bgsKickConnectionCmd,
		bgsListDomainBansCmd,
		bgsBanDomainCmd,
		bgsTakedownRepoCmd,
		bgsSetNewSubsEnabledCmd,
		bgsCompactRepo,
		bgsCompactAll,
		bgsResetRepo,
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
	Name:      "kick",
	Usage:     "tell Relay/BGS to drop the subscription connection",
	ArgsUsage: "<host>",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "ban",
			Usage: "make the disconnect sticky",
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

var bgsListDomainBansCmd = &cli.Command{
	Name: "list-domain-bans",
	Action: func(cctx *cli.Context) error {
		url := cctx.String("bgs") + "/admin/subs/listDomainBans"
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

var bgsBanDomainCmd = &cli.Command{
	Name:      "ban-domain",
	ArgsUsage: "<domain>",
	Action: func(cctx *cli.Context) error {
		url := cctx.String("bgs") + "/admin/subs/banDomain"

		b, err := json.Marshal(map[string]string{
			"domain": cctx.Args().First(),
		})
		if err != nil {
			return err
		}

		req, err := http.NewRequest("POST", url, bytes.NewReader(b))
		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json")

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

var bgsTakedownRepoCmd = &cli.Command{
	Name:      "take-down-repo",
	ArgsUsage: "<did>",
	Action: func(cctx *cli.Context) error {
		url := cctx.String("bgs") + "/admin/repo/takeDown"

		b, err := json.Marshal(map[string]string{
			"did": cctx.Args().First(),
		})
		if err != nil {
			return err
		}

		req, err := http.NewRequest("POST", url, bytes.NewReader(b))
		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json")

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

var bgsSetNewSubsEnabledCmd = &cli.Command{
	Name:      "set-accept-subs",
	ArgsUsage: "<boolean>",
	Usage:     "set configuration for whether new subscriptions are allowed",
	Action: func(cctx *cli.Context) error {
		url := cctx.String("bgs") + "/admin/subs/setEnabled"

		bv, err := strconv.ParseBool(cctx.Args().First())
		if err != nil {
			return err
		}

		url += fmt.Sprintf("?enabled=%v", bv)

		req, err := http.NewRequest("POST", url, nil)
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

var bgsCompactRepo = &cli.Command{
	Name:      "compact-repo",
	ArgsUsage: "<did>",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "fast",
		},
	},
	Action: func(cctx *cli.Context) error {
		uu, err := url.Parse(cctx.String("bgs") + "/admin/repo/compact")
		if err != nil {
			return err
		}

		q := uu.Query()
		did := cctx.Args().First()
		q.Add("did", did)

		if cctx.Bool("fast") {
			q.Add("fast", "true")
		}

		uu.RawQuery = q.Encode()

		req, err := http.NewRequest("POST", uu.String(), nil)
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

var bgsCompactAll = &cli.Command{
	Name: "compact-all",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "dry",
		},
		&cli.IntFlag{
			Name: "limit",
		},
		&cli.IntFlag{
			Name: "threshold",
		},
		&cli.BoolFlag{
			Name: "fast",
		},
	},
	Action: func(cctx *cli.Context) error {
		uu, err := url.Parse(cctx.String("bgs") + "/admin/repo/compactAll")
		if err != nil {
			return err
		}

		q := uu.Query()
		if cctx.Bool("dry") {
			q.Add("dry", "true")
		}

		if cctx.Bool("fast") {
			q.Add("fast", "true")
		}

		if cctx.IsSet("limit") {
			q.Add("limit", fmt.Sprint(cctx.Int("limit")))
		}

		if cctx.IsSet("threshold") {
			q.Add("threshold", fmt.Sprint(cctx.Int("threshold")))
		}

		uu.RawQuery = q.Encode()

		req, err := http.NewRequest("POST", uu.String(), nil)
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

var bgsResetRepo = &cli.Command{
	Name:      "reset-repo",
	ArgsUsage: "<did>",
	Action: func(cctx *cli.Context) error {
		url := cctx.String("bgs") + "/admin/repo/reset"

		did := cctx.Args().First()
		url += fmt.Sprintf("?did=%s", did)

		req, err := http.NewRequest("POST", url, nil)
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

var bgsSetTrustedDomains = &cli.Command{
	Name: "set-trusted-domain",
	Action: func(cctx *cli.Context) error {
		url := cctx.String("bgs") + "/admin/pds/addTrustedDomain"

		domain := cctx.Args().First()
		url += fmt.Sprintf("?domain=%s", domain)

		req, err := http.NewRequest("POST", url, nil)
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
