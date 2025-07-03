package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"
	"github.com/gander-social/gander-indigo-sovereign/xrpc"

	"github.com/urfave/cli/v2"
)

var cmdRelay = &cli.Command{
	Name:  "relay",
	Usage: "sub-commands for relays",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "relay-host",
			Usage:   "method, hostname, and port of Relay instance",
			Value:   "https://gndr.network",
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
				&cli.Command{
					Name:      "diff",
					Usage:     "compare host set (and seq) between two relay instances",
					ArgsUsage: `<relay-A-url> <relay-B-url>`,
					Flags: []cli.Flag{
						&cli.BoolFlag{
							Name:  "verbose",
							Usage: "print all hosts",
						},
						&cli.IntFlag{
							Name:  "seq-slop",
							Value: 100,
							Usage: "sequence delta allowed as close enough",
						},
					},
					Action: runRelayHostDiff,
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
		Host:      cctx.String("relay-host"),
		UserAgent: userAgent(),
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
		Host:      cctx.String("relay-host"),
		UserAgent: userAgent(),
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
		Host:      cctx.String("relay-host"),
		UserAgent: userAgent(),
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
		Host:      cctx.String("relay-host"),
		UserAgent: userAgent(),
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
		Host:      cctx.String("relay-host"),
		UserAgent: userAgent(),
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

type hostInfo struct {
	Hostname string
	Status   string
	Seq      int64
}

func fetchHosts(ctx context.Context, relayHost string) ([]hostInfo, error) {

	client := xrpc.Client{
		Host:      relayHost,
		UserAgent: userAgent(),
	}

	hosts := []hostInfo{}
	cursor := ""
	var size int64 = 500
	for {
		resp, err := comatproto.SyncListHosts(ctx, &client, cursor, size)
		if err != nil {
			return nil, err
		}

		for _, h := range resp.Hosts {
			if h.Status == nil || h.Seq == nil || *h.Seq <= 0 {
				continue
			}

			// TODO: only active or idle hosts?
			info := hostInfo{
				Hostname: h.Hostname,
				Status:   *h.Status,
				Seq:      *h.Seq,
			}
			hosts = append(hosts, info)
		}

		if resp.Cursor == nil || *resp.Cursor == "" {
			break
		}
		cursor = *resp.Cursor
	}
	return hosts, nil
}

func runRelayHostDiff(cctx *cli.Context) error {
	ctx := cctx.Context
	verbose := cctx.Bool("verbose")
	seqSlop := cctx.Int64("seq-slop")

	if cctx.Args().Len() != 2 {
		return fmt.Errorf("expected two relay URLs are args")
	}

	urlOne := cctx.Args().Get(0)
	urlTwo := cctx.Args().Get(1)

	listOne, err := fetchHosts(ctx, urlOne)
	if err != nil {
		return err
	}
	listTwo, err := fetchHosts(ctx, urlTwo)
	if err != nil {
		return err
	}

	allHosts := make(map[string]bool)
	mapOne := make(map[string]hostInfo)
	for _, val := range listOne {
		allHosts[val.Hostname] = true
		mapOne[val.Hostname] = val
	}
	mapTwo := make(map[string]hostInfo)
	for _, val := range listTwo {
		allHosts[val.Hostname] = true
		mapTwo[val.Hostname] = val
	}

	names := []string{}
	for k, _ := range allHosts {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, k := range names {
		one, okOne := mapOne[k]
		two, okTwo := mapTwo[k]
		if !okOne {
			if !verbose && two.Status != "active" {
				continue
			}
			fmt.Printf("%s\t\t%s/%d\tA-missing\n", k, two.Status, two.Seq)
		} else if !okTwo {
			if !verbose && one.Status != "active" {
				continue
			}
			fmt.Printf("%s\t%s/%d\t\tB-missing\n", k, one.Status, one.Seq)
		} else {
			status := ""
			if one.Status != two.Status {
				status = "diff-status"
			} else {
				delta := max(one.Seq, two.Seq) - min(one.Seq, two.Seq)
				if delta == 0 {
					status = "sync"
					if !verbose {
						continue
					}
				} else if delta < seqSlop {
					status = "nearly"
					if !verbose {
						continue
					}
				} else {
					status = fmt.Sprintf("delta=%d", delta)
				}
			}
			fmt.Printf("%s\t%s/%d\t%s/%d\t%s\n", k, one.Status, one.Seq, two.Status, two.Seq, status)
		}
	}

	return nil
}
