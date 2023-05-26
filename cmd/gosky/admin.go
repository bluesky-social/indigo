package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/api"
	"github.com/bluesky-social/indigo/api/atproto"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	cli "github.com/urfave/cli/v2"
)

var adminCmd = &cli.Command{
	Name: "admin",
	Subcommands: []*cli.Command{
		buildInviteTreeCmd,
		checkUserCmd,
		reportsCmd,
		disableInvitesCmd,
	},
}

var checkUserCmd = &cli.Command{
	Name: "checkUser",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "admin-password",
			EnvVars:  []string{"ATP_AUTH_ADMIN_PASSWORD"},
			Required: true,
		},
		&cli.StringFlag{
			Name:    "plc",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
		&cli.BoolFlag{
			Name: "raw",
		},
		&cli.BoolFlag{
			Name: "list-invited-dids",
		},
	},
	ArgsUsage: `[handle]`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.Background()

		resp, err := atproto.IdentityResolveHandle(ctx, xrpcc, lexutil.NewFormatHandle(cctx.Args().First()))
		if err != nil {
			return fmt.Errorf("resolve handle %q: %w", cctx.Args().First(), err)
		}

		adminKey := cctx.String("admin-password")
		xrpcc.AdminToken = &adminKey

		rep, err := atproto.AdminGetRepo(ctx, xrpcc, resp.Did)
		if err != nil {
			return fmt.Errorf("getRepo %s: %w", resp.Did, err)
		}

		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}

		plcc := cliutil.GetPLCClient(cctx)

		if cctx.Bool("raw") {
			fmt.Println(string(b))
		} else if cctx.Bool("list-invited-dids") {
			for _, inv := range rep.Invites {
				for _, u := range inv.Uses {
					fmt.Println(u.UsedBy)
				}
			}
		} else {
			var invby string
			if fa := rep.InvitedBy.ForAccount; fa != "" {
				if fa == "admin" {
					invby = fa
				} else {
					handle, _, err := api.ResolveDidToHandle(ctx, xrpcc, plcc, fa)
					if err != nil {
						return fmt.Errorf("resolve did %q: %w", fa, err)
					}

					invby = handle
				}
			}

			fmt.Println(rep.Handle)
			fmt.Println(rep.Did)
			fmt.Println(rep.Email)
			fmt.Println("indexed at: ", rep.IndexedAt)
			fmt.Printf("Invited by: %s\n", invby)
			if rep.InvitesDisabled != nil && *rep.InvitesDisabled {
				fmt.Println("INVITES DISABLED")
			}

			var invited []*atproto.AdminDefs_RepoViewDetail
			var lk sync.Mutex
			var wg sync.WaitGroup
			var used int
			var revoked int
			for _, inv := range rep.Invites {
				used += len(inv.Uses)

				if inv.Disabled {
					revoked++
				}
				for _, u := range inv.Uses {
					wg.Add(1)
					go func(did string) {
						defer wg.Done()
						repo, err := atproto.AdminGetRepo(ctx, xrpcc, lexutil.NewFormatDID(did))
						if err != nil {
							fmt.Println("ERROR: ", err)
							return
						}

						lk.Lock()
						invited = append(invited, repo)
						lk.Unlock()
					}(u.UsedBy.String())
				}
			}

			wg.Wait()

			fmt.Printf("Invites, used %d of %d (%d disabled)\n", used, len(rep.Invites), revoked)
			for _, inv := range invited {

				var invited, total int
				for _, code := range inv.Invites {
					total += len(code.Uses) + int(code.Available)
					invited += len(code.Uses)
				}

				fmt.Printf(" - %s (%d / %d)\n", inv.Handle, invited, total)
			}
		}
		return nil
	},
}

var buildInviteTreeCmd = &cli.Command{
	Name: "buildInviteTree",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "admin-password",
			EnvVars:  []string{"ATP_AUTH_ADMIN_PASSWORD"},
			Required: true,
		},
		&cli.StringFlag{
			Name: "invite-list",
		},
		&cli.IntFlag{
			Name:  "top",
			Value: 50,
		},
	},
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.Background()

		adminKey := cctx.String("admin-password")

		xrpcc.AdminToken = &adminKey

		var allcodes []*atproto.ServerDefs_InviteCode

		if invl := cctx.String("invite-list"); invl != "" {
			fi, err := os.Open(invl)
			if err != nil {
				return err
			}

			if err := json.NewDecoder(fi).Decode(&allcodes); err != nil {
				return err
			}
		} else {
			var cursor string
			for {
				invites, err := atproto.AdminGetInviteCodes(ctx, xrpcc, cursor, 100, "")
				if err != nil {
					return err
				}

				allcodes = append(allcodes, invites.Codes...)

				if invites.Cursor != nil {
					cursor = *invites.Cursor
				}
				if len(invites.Codes) == 0 {
					break
				}
			}

			fi, err := os.Create("output.json")
			if err != nil {
				return err
			}
			defer fi.Close()

			if err := json.NewEncoder(fi).Encode(allcodes); err != nil {
				return err
			}
		}

		users := make(map[string]*userInviteInfo)
		users["admin"] = &userInviteInfo{
			Handle: "admin",
		}

		var getUser func(did string) (*userInviteInfo, error)
		getUser = func(did string) (*userInviteInfo, error) {
			u, ok := users[did]
			if ok {
				return u, nil
			}

			repo, err := atproto.AdminGetRepo(ctx, xrpcc, lexutil.NewFormatDID(did))
			if err != nil {
				return nil, err
			}

			var invby string
			if fa := repo.InvitedBy.ForAccount; fa != "" {
				if fa == "admin" {
					invby = "admin"
				} else {
					invu, ok := users[fa]
					if ok {
						invby = invu.Handle
					} else {
						invrepo, err := atproto.AdminGetRepo(ctx, xrpcc, lexutil.NewFormatDID(fa))
						if err != nil {
							return nil, fmt.Errorf("resolving inviter (%q): %w", fa, err)
						}

						invby = invrepo.Handle.String()
					}
				}
			}

			u = &userInviteInfo{
				Did:             did,
				Handle:          repo.Handle.String(),
				InvitedBy:       repo.InvitedBy.ForAccount,
				InvitedByHandle: invby,
				TotalInvites:    len(repo.Invites),
			}
			if repo.Email != nil {
				u.Email = *repo.Email
			}

			users[did] = u

			return u, nil
		}
		_ = getUser

		initmap := make(map[string]*basicInvInfo)
		var initlist []*basicInvInfo
		for _, inv := range allcodes {
			acc, ok := initmap[inv.ForAccount]
			if !ok {
				acc = &basicInvInfo{
					Did: inv.ForAccount,
				}
				initmap[inv.ForAccount] = acc
				initlist = append(initlist, acc)
			}

			acc.TotalInvites += int(inv.Available) + len(inv.Uses)
			for _, u := range inv.Uses {
				acc.Invited = append(acc.Invited, u.UsedBy.String())
			}
		}

		sort.Slice(initlist, func(i, j int) bool {
			return len(initlist[i].Invited) > len(initlist[j].Invited)
		})

		for i := 0; i < cctx.Int("top"); i++ {
			u, err := getUser(initlist[i].Did)
			if err != nil {
				fmt.Printf("getuser %q: %s\n", initlist[i].Did, err)
				continue
			}

			fmt.Printf("%d: %s (%d of %d)\n", i, u.Handle, len(initlist[i].Invited), u.TotalInvites)
		}

		/*
			fmt.Println("writing output...")
			outfi, err := os.Create("userdump.json")
			if err != nil {
				return err
			}
			defer outfi.Close()

			return json.NewEncoder(outfi).Encode(users)
		*/

		return nil
	},
}

type userInviteInfo struct {
	CreatedAt       time.Time
	Did             string
	Handle          string
	InvitedBy       string
	InvitedByHandle string
	TotalInvites    int
	Invited         []string
	Email           string
}

type basicInvInfo struct {
	Did          string
	Invited      []string
	TotalInvites int
}

var reportsCmd = &cli.Command{
	Name: "reports",
	Subcommands: []*cli.Command{
		listReportsCmd,
	},
}

var listReportsCmd = &cli.Command{
	Name: "list",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "admin-password",
			EnvVars:  []string{"ATP_AUTH_ADMIN_PASSWORD"},
			Required: true,
		},
		&cli.StringFlag{
			Name:    "plc",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
		&cli.BoolFlag{
			Name: "raw",
		},
	},
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.Background()

		adminKey := cctx.String("admin-password")
		xrpcc.AdminToken = &adminKey

		resp, err := atproto.AdminGetModerationReports(ctx, xrpcc, "", "", 100, true, "")
		if err != nil {
			return err
		}

		tojson := func(i any) string {
			b, err := json.MarshalIndent(i, "", "  ")
			if err != nil {
				panic(err)
			}

			return string(b)
		}

		for _, rep := range resp.Reports {
			b, err := json.MarshalIndent(rep, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			for _, act := range rep.ResolvedByActionIds {
				action, err := atproto.AdminGetModerationAction(ctx, xrpcc, act)
				if err != nil {
					return err
				}

				fmt.Println(tojson(action))
			}
		}
		return nil
	},
}

var disableInvitesCmd = &cli.Command{
	Name: "disableInvites",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "admin-password",
			EnvVars:  []string{"ATP_AUTH_ADMIN_PASSWORD"},
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.Background()

		adminKey := cctx.String("admin-password")
		xrpcc.AdminToken = &adminKey

		handle := cctx.Args().First()
		if !strings.HasPrefix(handle, "did:") {
			resp, err := atproto.IdentityResolveHandle(ctx, xrpcc, handle)
			if err != nil {
				return err
			}

			handle = resp.Did
		}

		return atproto.AdminDisableAccountInvites(ctx, xrpcc, &atproto.AdminDisableAccountInvites_Input{
			Account: handle,
		})
	},
}
