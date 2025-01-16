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
	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
		&cli.StringFlag{
			Name:  "admin-endpoint",
			Value: "https://mod.bsky.app",
		},
	},
	Subcommands: []*cli.Command{
		buildInviteTreeCmd,
		checkUserCmd,
		createInviteCmd,
		disableInvitesCmd,
		enableInvitesCmd,
		queryModerationStatusesCmd,
		listInviteTreeCmd,
		reportsCmd,
		takeDownAccountCmd,
	},
}

var checkUserCmd = &cli.Command{
	Name: "check-user",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "raw",
			Usage: "dump simple JSON response to stdout",
		},
		&cli.BoolFlag{
			Name: "list-invited-dids",
		},
	},
	ArgsUsage: `<did-or-handle>`,
	Action: func(cctx *cli.Context) error {
		xrpcc, err := cliutil.GetXrpcClient(cctx, false)
		if err != nil {
			return err
		}

		dir := identity.DefaultDirectory()
		ctx := context.Background()

		ident, err := syntax.ParseAtIdentifier(cctx.Args().First())
		if err != nil {
			return err
		}

		id, err := dir.Lookup(ctx, *ident)
		if err != nil {
			return fmt.Errorf("resolve identifier %q: %w", cctx.Args().First(), err)
		}

		did := id.DID.String()

		adminKey := cctx.String("admin-password")
		xrpcc.AdminToken = &adminKey
		xrpcc.Host = cctx.String("admin-endpoint")

		rep, err := toolsozone.ModerationGetRepo(ctx, xrpcc, did)
		if err != nil {
			return fmt.Errorf("getRepo %s: %w", did, err)
		}

		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}

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
			if rep.InvitedBy != nil {
				if fa := rep.InvitedBy.ForAccount; fa != "" {
					if fa == "admin" {
						invby = fa
					} else {
						id, err := dir.LookupDID(ctx, syntax.DID(fa))
						if err != nil {
							fmt.Println("ERROR: failed to resolve inviter: ", err)
						}

						invby = id.Handle.String()
					}
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

			var invited []*toolsozone.ModerationDefs_RepoViewDetail
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
						repo, err := toolsozone.ModerationGetRepo(ctx, xrpcc, did)
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

			repo, err := toolsozone.ModerationGetRepo(ctx, xrpcc, did)
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
						invrepo, err := toolsozone.ModerationGetRepo(ctx, xrpcc, fa)
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

		// fetch recent moderation reports
		resp, err := toolsozone.ModerationQueryEvents(
			ctx,
			xrpcc,
			nil,   // addedLabels []string
			nil,   // addedTags []string
			nil,   // collections []string
			"",    // comment string
			"",    // createdAfter string
			"",    // createdBefore string
			"",    // createdBy string
			"",    // cursor string
			false, // hasComment bool
			false, // includeAllUserRecords bool
			100,   // limit int64
			nil,   // policies []string
			nil,   // removedLabels []string
			nil,   // removedTags []string
			nil,   // reportTypes []string
			"",    // sortDirection string
			"",    // subject string
			"",    // subjectType string
			[]string{"tools.ozone.moderation.defs#modEventReport"}, // types []string
		)
		if err != nil {
			return err
		}

		for _, rep := range resp.Events {
			b, err := json.MarshalIndent(rep, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
		}
		return nil
	},
}

var disableInvitesCmd = &cli.Command{
	Name:      "disable-invites",
	ArgsUsage: "<did-or-handle>",
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
	Name:      "enable-invites",
	ArgsUsage: "<did-or-handle>",
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
	ArgsUsage: `<did-or-handle>`,
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

			rep, err := toolsozone.ModerationGetRepo(ctx, xrpcc, next)
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
				dir := identity.DefaultDirectory()
				resp, err := dir.LookupHandle(ctx, syntax.Handle(did))
				if err != nil {
					return err
				}

				did = resp.DID.String()
			}

			reason := cctx.String("reason")
			adminUser := cctx.String("admin-user")
			if !strings.HasPrefix(adminUser, "did:") {
				dir := identity.DefaultDirectory()
				resp, err := dir.LookupHandle(ctx, syntax.Handle(adminUser))
				if err != nil {
					return err
				}

				adminUser = resp.DID.String()
			}

			resp, err := toolsozone.ModerationEmitEvent(ctx, xrpcc, &toolsozone.ModerationEmitEvent_Input{
				CreatedBy: adminUser,
				Event: &toolsozone.ModerationEmitEvent_Input_Event{
					ModerationDefs_ModEventTakedown: &toolsozone.ModerationDefs_ModEventTakedown{
						Comment: &reason,
					},
				},
				Subject: &toolsozone.ModerationEmitEvent_Input_Subject{
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

var queryModerationStatusesCmd = &cli.Command{
	Name:      "query-moderation-statuses",
	ArgsUsage: "<did-or-handle>",
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

		resp, err := toolsozone.ModerationQueryEvents(
			ctx,
			xrpcc,
			nil,   // addedLabels []string
			nil,   // addedTags []string
			nil,   // collections []string
			"",    // comment string
			"",    // createdAfter string
			"",    // createdBefore string
			"",    // createdBy string
			"",    // cursor string
			false, // hasComment bool
			false, // includeAllUserRecords bool
			100,   // limit int64
			nil,   // policies []string
			nil,   // removedLabels []string
			nil,   // removedTags []string
			nil,   // reportTypes []string
			"",    // sortDirection string
			"",    // subject string
			"",    // subjectType string
			[]string{"tools.ozone.moderation.defs#modEventReport"}, // types []string
		)
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
