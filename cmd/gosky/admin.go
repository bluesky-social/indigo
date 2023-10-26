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
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/util/cliutil"
	cli "github.com/urfave/cli/v2"
)

var adminCmd = &cli.Command{
	Name:  "admin",
	Usage: "sub-commands for PDS administration",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "admin-password",
			EnvVars:  []string{"ATP_AUTH_ADMIN_PASSWORD"},
			Required: true,
		},
	},
	Subcommands: []*cli.Command{
		buildInviteTreeCmd,
		checkUserCmd,
		createInviteCmd,
		disableInvitesCmd,
		enableInvitesCmd,
		getModerationActionsCmd,
		listInviteTreeCmd,
		reportsCmd,
		takeDownAccountCmd,
	},
}

var checkUserCmd = &cli.Command{
	Name: "check-user",
	Flags: []cli.Flag{
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

		phr := &api.ProdHandleResolver{}

		did := cctx.Args().First()
		if !strings.HasPrefix(did, "did:") {
			rdid, err := phr.ResolveHandleToDid(ctx, cctx.Args().First())
			if err != nil {
				return fmt.Errorf("resolve handle %q: %w", cctx.Args().First(), err)
			}

			did = rdid
		}

		adminKey := cctx.String("admin-password")
		xrpcc.AdminToken = &adminKey

		rep, err := atproto.AdminGetRepo(ctx, xrpcc, did)
		if err != nil {
			return fmt.Errorf("getRepo %s: %w", did, err)
		}

		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}

		plcc := cliutil.GetDidResolver(cctx)

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
					handle, _, err := api.ResolveDidToHandle(ctx, xrpcc, plcc, phr, fa)
					if err != nil {
						fmt.Println("ERROR: failed to resolve inviter: ", err)
						handle = fa
					}

					invby = handle
				}
			}

			fmt.Println(rep.Handle)
			fmt.Println(rep.Did)
			if rep.Email != nil {
				fmt.Println(*rep.Email)
			}
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
						repo, err := atproto.AdminGetRepo(ctx, xrpcc, did)
						if err != nil {
							fmt.Println("ERROR: ", err)
							return
						}

						lk.Lock()
						invited = append(invited, repo)
						lk.Unlock()
					}(u.UsedBy)
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
	Name: "build-invite-tree",
	Flags: []cli.Flag{
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

			repo, err := atproto.AdminGetRepo(ctx, xrpcc, did)
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
						invrepo, err := atproto.AdminGetRepo(ctx, xrpcc, fa)
						if err != nil {
							return nil, fmt.Errorf("resolving inviter (%q): %w", fa, err)
						}

						invby = invrepo.Handle
					}
				}
			}

			u = &userInviteInfo{
				Did:             did,
				Handle:          repo.Handle,
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
				acc.Invited = append(acc.Invited, u.UsedBy)
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
		&cli.BoolFlag{
			Name: "raw",
		},
		&cli.BoolFlag{
			Name:  "resolved",
			Value: true,
		},
		&cli.BoolFlag{
			Name: "template-output",
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

		// AdminGetModerationReports(ctx context.Context, c *xrpc.Client, actionType string, actionedBy string, cursor string, ignoreSubjects []string, limit int64, reporters []string, resolved bool, reverse bool, subject string) (*AdminGetModerationReports_Output, error)
		resp, err := atproto.AdminGetModerationReports(ctx, xrpcc, "", "", "", nil, 100, nil, cctx.Bool("resolved"), false, "")
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
	Name: "disable-invites",
	Action: func(cctx *cli.Context) error {

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.Background()

		adminKey := cctx.String("admin-password")
		xrpcc.AdminToken = &adminKey

		phr := &api.ProdHandleResolver{}
		handle := cctx.Args().First()
		if !strings.HasPrefix(handle, "did:") {
			resp, err := phr.ResolveHandleToDid(ctx, handle)
			if err != nil {
				return err
			}

			handle = resp
		}

		if err := atproto.AdminDisableAccountInvites(ctx, xrpcc, &atproto.AdminDisableAccountInvites_Input{
			Account: handle,
		}); err != nil {
			return err
		}

		if err := atproto.AdminDisableInviteCodes(ctx, xrpcc, &atproto.AdminDisableInviteCodes_Input{
			Accounts: []string{handle},
		}); err != nil {
			return err
		}

		return nil
	},
}

var enableInvitesCmd = &cli.Command{
	Name: "enable-invites",
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
			phr := &api.ProdHandleResolver{}
			resp, err := phr.ResolveHandleToDid(ctx, handle)
			if err != nil {
				return err
			}

			handle = resp
		}

		return atproto.AdminEnableAccountInvites(ctx, xrpcc, &atproto.AdminEnableAccountInvites_Input{
			Account: handle,
		})
	},
}

var listInviteTreeCmd = &cli.Command{
	Name: "list-invite-tree",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "disable-invites",
			Usage: "additionally disable invites for all printed DIDs",
		},
		&cli.BoolFlag{
			Name:  "revoke-existing-invites",
			Usage: "additionally revoke any existing invites for all printed DIDs",
		},
		&cli.BoolFlag{
			Name:  "print-handles",
			Usage: "print handle for each DID",
		},
		&cli.BoolFlag{
			Name:  "print-emails",
			Usage: "print account email for each DID",
		},
	},
	ArgsUsage: `[handle]`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.Background()

		phr := &api.ProdHandleResolver{}

		did := cctx.Args().First()
		if !strings.HasPrefix(did, "did:") {
			rdid, err := phr.ResolveHandleToDid(ctx, cctx.Args().First())
			if err != nil {
				return fmt.Errorf("resolve handle %q: %w", cctx.Args().First(), err)
			}

			did = rdid
		}

		adminKey := cctx.String("admin-password")
		xrpcc.AdminToken = &adminKey

		queue := []string{did}

		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]

			if cctx.Bool("disable-invites") {
				if err := atproto.AdminDisableAccountInvites(ctx, xrpcc, &atproto.AdminDisableAccountInvites_Input{
					Account: next,
				}); err != nil {
					return fmt.Errorf("failed to disable invites on %q: %w", next, err)
				}
			}

			if cctx.Bool("revoke-existing-invites") {
				if err := atproto.AdminDisableInviteCodes(ctx, xrpcc, &atproto.AdminDisableInviteCodes_Input{
					Accounts: []string{next},
				}); err != nil {
					return fmt.Errorf("failed to revoke existing invites on %q: %w", next, err)
				}
			}

			rep, err := atproto.AdminGetRepo(ctx, xrpcc, next)
			if err != nil {
				fmt.Printf("Failed to getRepo for DID %s: %s\n", next, err.Error())
				continue
			}
			fmt.Print(next)

			if cctx.Bool("print-handles") {
				if rep.Handle != "" {
					fmt.Print(" ", rep.Handle)
				} else {
					fmt.Print(" NO HANDLE")
				}
			}

			if cctx.Bool("print-emails") {
				if rep.Email != nil {
					fmt.Print(" ", *rep.Email)
				} else {
					fmt.Print(" NO EMAIL")
				}
			}
			fmt.Println()

			for _, inv := range rep.Invites {
				for _, u := range inv.Uses {
					queue = append(queue, u.UsedBy)
				}
			}
		}
		return nil
	},
}

var takeDownAccountCmd = &cli.Command{
	Name: "account-takedown",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "reason",
			Usage:    "why the account is being taken down",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "admin-user",
			Usage:    "account of person running this command, for recordkeeping",
			Required: true,
		},
		&cli.BoolFlag{
			Name:  "revert-actions",
			Usage: "revert existing moderation actions on this user before taking down",
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

		for _, did := range cctx.Args().Slice() {
			if !strings.HasPrefix(did, "did:") {
				phr := &api.ProdHandleResolver{}
				resp, err := phr.ResolveHandleToDid(ctx, did)
				if err != nil {
					return err
				}

				did = resp
			}

			reason := cctx.String("reason")
			adminUser := cctx.String("admin-user")
			if !strings.HasPrefix(adminUser, "did:") {
				phr := &api.ProdHandleResolver{}
				resp, err := phr.ResolveHandleToDid(ctx, adminUser)
				if err != nil {
					return err
				}

				adminUser = resp
			}

			if cctx.Bool("revert-actions") {
				resp, err := atproto.AdminGetModerationActions(ctx, xrpcc, "", 100, did)
				if err != nil {
					return err
				}

				for _, act := range resp.Actions {
					if act.Action == nil || *act.Action != "com.atproto.admin.defs#acknowledge" {
						return fmt.Errorf("will only revert acknowledge actions")
					}

					_, err := atproto.AdminReverseModerationAction(ctx, xrpcc, &atproto.AdminReverseModerationAction_Input{
						CreatedBy: adminUser,
						Id:        act.Id,
						Reason:    "reverting for takedown",
					})
					if err != nil {
						return fmt.Errorf("failed to revert existing action: %w", err)
					}
				}

			}

			resp, err := atproto.AdminTakeModerationAction(ctx, xrpcc, &atproto.AdminTakeModerationAction_Input{
				Action:    "com.atproto.admin.defs#takedown",
				Reason:    reason,
				CreatedBy: adminUser,
				Subject: &atproto.AdminTakeModerationAction_Input_Subject{
					AdminDefs_RepoRef: &atproto.AdminDefs_RepoRef{
						Did: did,
					},
				},
			})
			if err != nil {
				return err
			}

			b, err := json.MarshalIndent(resp, "", "  ")
			if err != nil {
				return err
			}

			fmt.Println(string(b))
		}
		return nil
	},
}

var getModerationActionsCmd = &cli.Command{
	Name: "get-moderation-actions",
	Action: func(cctx *cli.Context) error {

		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		ctx := context.Background()

		adminKey := cctx.String("admin-password")
		xrpcc.AdminToken = &adminKey

		did := cctx.Args().First()
		if !strings.HasPrefix(did, "did:") {
			phr := &api.ProdHandleResolver{}
			resp, err := phr.ResolveHandleToDid(ctx, did)
			if err != nil {
				return err
			}

			did = resp
		}

		resp, err := atproto.AdminGetModerationActions(ctx, xrpcc, "", 100, did)
		if err != nil {
			return err
		}

		b, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(b))
		return nil
	},
}

var createInviteCmd = &cli.Command{
	Name: "create-invites",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "useCount",
			Value: 1,
		},
		&cli.IntFlag{
			Name:  "num",
			Value: 1,
		},
		&cli.StringFlag{
			Name: "bulk",
		},
	},
	ArgsUsage: "[handle]",
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		adminKey := cctx.String("admin-password")

		count := cctx.Int("useCount")
		num := cctx.Int("num")

		phr := &api.ProdHandleResolver{}
		if bulkfi := cctx.String("bulk"); bulkfi != "" {
			xrpcc.AdminToken = &adminKey
			dids, err := readDids(bulkfi)
			if err != nil {
				return err
			}

			for i, d := range dids {
				if !strings.HasPrefix(d, "did:plc:") {
					out, err := phr.ResolveHandleToDid(context.TODO(), d)
					if err != nil {
						return fmt.Errorf("failed to resolve %q: %w", d, err)
					}

					dids[i] = out
				}
			}

			for n := 0; n < len(dids); n += 500 {
				slice := dids
				if len(slice) > 500 {
					slice = slice[:500]
				}

				_, err = comatproto.ServerCreateInviteCodes(context.TODO(), xrpcc, &comatproto.ServerCreateInviteCodes_Input{
					UseCount:    int64(count),
					ForAccounts: slice,
					CodeCount:   int64(num),
				})
				if err != nil {
					return err
				}
			}

			return nil
		}

		var usrdid []string
		if forUser := cctx.Args().Get(0); forUser != "" {
			if !strings.HasPrefix(forUser, "did:") {
				resp, err := phr.ResolveHandleToDid(context.TODO(), forUser)
				if err != nil {
					return fmt.Errorf("resolving handle: %w", err)
				}

				usrdid = []string{resp}
			} else {
				usrdid = []string{forUser}
			}
		}

		xrpcc.AdminToken = &adminKey
		resp, err := comatproto.ServerCreateInviteCodes(context.TODO(), xrpcc, &comatproto.ServerCreateInviteCodes_Input{
			UseCount:    int64(count),
			ForAccounts: usrdid,
			CodeCount:   int64(num),
		})
		if err != nil {
			return fmt.Errorf("creating codes: %w", err)
		}

		for _, c := range resp.Codes {
			for _, cc := range c.Codes {
				fmt.Println(cc)
			}
		}

		return nil
	},
}
