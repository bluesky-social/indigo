package main

import (
	"encoding/json"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/urfave/cli/v2"
)

var cmdRelay = &cli.Command{
	Name:  "relay",
	Usage: "sub-commands for relays",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "relay-host",
			Usage:   "method, hostname, and port of Relay instance",
			Value:   "https://bsky.network",
			EnvVars: []string{"ATP_RELAY_HOST", "RELAY_HOST"},
		},
	},
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:  "account",
			Usage: "sub-commands for accounts/repos on relay",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:    "list",
					Aliases: []string{"ls"},
					Usage:   "enumerate all accounts",
					Flags: []cli.Flag{
						&cli.StringFlag{
							Name:    "collection",
							Aliases: []string{"c"},
							Usage:   "collection (NSID) to match",
						},
						&cli.BoolFlag{
							Name:  "json",
							Usage: "print output as JSON lines",
						},
					},
					Action: runRelayAccountList,
				},
				&cli.Command{
					Name:      "status",
					ArgsUsage: `<did>`,
					Usage:     "describe status of individual account",
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:  "json",
							Usage: "print output as JSON",
						},
					},
					Action: runRelayAccountStatus,
				},
			},
		},
		&cli.Command{
			Name:  "host",
			Usage: "sub-commands for upstream hosts (eg, PDS)",
			Subcommands: []*cli.Command{
				&cli.Command{
					Name:      "request-crawl",
					Aliases:   []string{"add"},
					Usage:     "request crawl of upstream host (eg, PDS)",
					ArgsUsage: `<hostname>`,
					Action:    runRelayHostRequestCrawl,
				},
				&cli.Command{
					Name:    "list",
					Aliases: []string{"ls"},
					Usage:   "enumerate all hosts indexed by relay",
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:  "json",
							Usage: "print output as JSON lines",
						},
					},
					Action: runRelayHostList,
				},
				&cli.Command{
					Name:      "status",
					ArgsUsage: `<hostname>`,
					Usage:     "describe status of individual host",
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:  "json",
							Usage: "print output as JSON",
						},
					},
					Action: runRelayHostStatus,
				},
			},
		},
		cmdRelayAdmin,
	},
}

func runRelayAccountList(cctx *cli.Context) error {
	ctx := cctx.Context

	if cctx.Args().Len() > 0 {
		return fmt.Errorf("unexpected arguments")
	}

	client := xrpc.Client{
		Host: cctx.String("relay-host"),
	}

	collection := cctx.String("collection")
	cursor := ""
	var size int64 = 500
	for {
		if collection != "" {
			resp, err := comatproto.SyncListReposByCollection(ctx, &client, collection, cursor, size)
			if err != nil {
				return err
			}
			for _, r := range resp.Repos {
				fmt.Println(r.Did)
			}

			if resp.Cursor == nil || *resp.Cursor == "" {
				break
			}
			cursor = *resp.Cursor
		} else {
			resp, err := comatproto.SyncListRepos(ctx, &client, cursor, size)
			if err != nil {
				return err
			}

			for _, r := range resp.Repos {
				if cctx.Bool("json") {
					b, err := json.Marshal(r)
					if err != nil {
						return err
					}
					fmt.Println(string(b))
				} else {
					status := "unknown"
					if r.Active != nil && *r.Active {
						status = "active"
					} else if r.Status != nil {
						status = *r.Status
					}
					fmt.Printf("%s\t%s\t%s\n", r.Did, status, r.Rev)
				}
			}

			if resp.Cursor == nil || *resp.Cursor == "" {
				break
			}
			cursor = *resp.Cursor
		}
	}
	return nil
}

func runRelayAccountStatus(cctx *cli.Context) error {
	ctx := cctx.Context

	didStr := cctx.Args().First()
	if didStr == "" {
		return fmt.Errorf("need to provide account DID as argument")
	}
	if cctx.Args().Len() != 1 {
		return fmt.Errorf("unexpected arguments")
	}

	did, err := syntax.ParseDID(didStr)
	if err != nil {
		return err
	}

	client := xrpc.Client{
		Host: cctx.String("relay-host"),
	}

	r, err := comatproto.SyncGetRepoStatus(ctx, &client, did.String())
	if err != nil {
		return err
	}

	if cctx.Bool("json") {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	} else {
		status := "unknown"
		if r.Active {
			status = "active"
		} else if r.Status != nil {
			status = *r.Status
		}
		rev := ""
		if r.Rev != nil {
			rev = *r.Rev
		}
		fmt.Printf("%s\t%s\t%s\n", r.Did, status, rev)
	}

	return nil
}

func runRelayHostRequestCrawl(cctx *cli.Context) error {
	ctx := cctx.Context

	hostname := cctx.Args().First()
	if hostname == "" {
		return fmt.Errorf("need to provide hostname as argument")
	}
	if cctx.Args().Len() != 1 {
		return fmt.Errorf("unexpected arguments")
	}

	client := xrpc.Client{
		Host: cctx.String("relay-host"),
	}

	err := comatproto.SyncRequestCrawl(ctx, &client, &comatproto.SyncRequestCrawl_Input{Hostname: hostname})
	if err != nil {
		return err
	}
	fmt.Println("success")
	return nil
}

func runRelayHostList(cctx *cli.Context) error {
	ctx := cctx.Context

	if cctx.Args().Len() > 0 {
		return fmt.Errorf("unexpected arguments")
	}

	client := xrpc.Client{
		Host: cctx.String("relay-host"),
	}

	cursor := ""
	var size int64 = 500
	for {
		resp, err := comatproto.SyncListHosts(ctx, &client, cursor, size)
		if err != nil {
			return err
		}

		for _, h := range resp.Hosts {
			if cctx.Bool("json") {
				b, err := json.Marshal(h)
				if err != nil {
					return err
				}
				fmt.Println(string(b))
			} else {
				status := ""
				if h.Status != nil {
					status = *h.Status
				}
				count := ""
				if h.AccountCount != nil {
					count = fmt.Sprintf("%d", *h.AccountCount)
				}
				seq := ""
				if h.Seq != nil {
					seq = fmt.Sprintf("%d", *h.Seq)
				}
				fmt.Printf("%s\t%s\t%s\t%s\n", h.Hostname, status, count, seq)
			}
		}

		if resp.Cursor == nil || *resp.Cursor == "" {
			break
		}
		cursor = *resp.Cursor
	}
	return nil
}

func runRelayHostStatus(cctx *cli.Context) error {
	ctx := cctx.Context

	hostname := cctx.Args().First()
	if hostname == "" {
		return fmt.Errorf("need to provide hostname as argument")
	}
	if cctx.Args().Len() != 1 {
		return fmt.Errorf("unexpected arguments")
	}

	client := xrpc.Client{
		Host: cctx.String("relay-host"),
	}

	h, err := comatproto.SyncGetHostStatus(ctx, &client, hostname)
	if err != nil {
		return err
	}

	if cctx.Bool("json") {
		b, err := json.Marshal(h)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	} else {
		status := ""
		if h.Status != nil {
			status = *h.Status
		}
		count := ""
		if h.AccountCount != nil {
			count = fmt.Sprintf("%d", *h.AccountCount)
		}
		seq := ""
		if h.Seq != nil {
			seq = fmt.Sprintf("%d", *h.Seq)
		}
		fmt.Printf("%s\t%s\t%s\t%s\n", h.Hostname, status, count, seq)
	}

	return nil
}
