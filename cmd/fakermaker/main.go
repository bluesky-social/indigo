// Tool to generate fake accounts, content, and interactions.
// Intended for development and benchmarking. Similar to 'stress' and could
// merge at some point.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	_ "github.com/joho/godotenv/autoload"
	_ "go.uber.org/automaxprocs"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/fakedata"
	"github.com/bluesky-social/indigo/util/cliutil"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"
)

func main() {
	run(os.Args)
}

func run(args []string) {

	app := cli.Command{
		Name:    "fakermaker",
		Usage:   "bluesky fake account/content generator",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "pds-host",
			Usage:   "method, hostname, and port of PDS instance",
			Value:   "http://localhost:4849",
			Sources: cli.EnvVars("ATP_PDS_HOST"),
		},
		&cli.StringFlag{
			Name:     "admin-password",
			Usage:    "admin authentication password for PDS",
			Required: true,
			Sources:  cli.EnvVars("ATP_AUTH_ADMIN_PASSWORD"),
		},
		&cli.IntFlag{
			Name:    "jobs",
			Aliases: []string{"j"},
			Usage:   "number of parallel threads to use",
			Value:   runtime.NumCPU(),
		},
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "gen-accounts",
			Usage:  "create accounts (DID, handle, profile)",
			Action: genAccounts,
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:    "count",
					Aliases: []string{"n"},
					Usage:   "total number of accounts to create",
					Value:   100,
				},
				&cli.IntFlag{
					Name:  "count-celebrities",
					Usage: "number of accounts as 'celebrities' (many followers)",
					Value: 10,
				},
				&cli.StringFlag{
					Name:  "domain-suffix",
					Usage: "domain to register handle under",
					Value: "test",
				},
				&cli.BoolFlag{
					Name:  "use-invite-code",
					Usage: "create and use an invite code",
					Value: false,
				},
			},
		},
		&cli.Command{
			Name:   "gen-profiles",
			Usage:  "creates profile records for accounts",
			Action: genProfiles,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "catalog",
					Usage: "file path of account catalog JSON file",
					Value: "data/fakermaker/accounts.json",
				},
				&cli.BoolFlag{
					Name:  "no-avatars",
					Usage: "disable avatar image generation",
					Value: false,
				},
				&cli.BoolFlag{
					Name:  "no-banners",
					Usage: "disable profile banner image generation",
					Value: false,
				},
			},
		},
		&cli.Command{
			Name:   "gen-graph",
			Usage:  "creates social graph (follows and mutes)",
			Action: genGraph,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "catalog",
					Usage: "file path of account catalog JSON file",
					Value: "data/fakermaker/accounts.json",
				},
				&cli.IntFlag{
					Name:  "max-follows",
					Usage: "create up to this many follows for each account",
					Value: 90,
				},
				&cli.IntFlag{
					Name:  "max-mutes",
					Usage: "create up to this many mutes (blocks) for each account",
					Value: 25,
				},
			},
		},
		&cli.Command{
			Name:   "gen-posts",
			Usage:  "creates posts for accounts",
			Action: genPosts,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "catalog",
					Usage: "file path of account catalog JSON file",
					Value: "data/fakermaker/accounts.json",
				},
				&cli.IntFlag{
					Name:  "max-posts",
					Usage: "create up to this many posts for each account",
					Value: 10,
				},
				&cli.Float64Flag{
					Name:  "frac-image",
					Usage: "portion of posts to include images",
					Value: 0.25,
				},
				&cli.Float64Flag{
					Name:  "frac-mention",
					Usage: "of posts created, fraction to include mentions in",
					Value: 0.50,
				},
			},
		},
		&cli.Command{
			Name:   "gen-interactions",
			Usage:  "create interactions between accounts",
			Action: genInteractions,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "catalog",
					Usage: "file path of account catalog JSON file",
					Value: "data/fakermaker/accounts.json",
				},
				&cli.Float64Flag{
					Name:  "frac-like",
					Usage: "fraction of posts in timeline to like",
					Value: 0.20,
				},
				&cli.Float64Flag{
					Name:  "frac-repost",
					Usage: "fraction of posts in timeline to repost",
					Value: 0.20,
				},
				&cli.Float64Flag{
					Name:  "frac-reply",
					Usage: "fraction of posts in timeline to reply to",
					Value: 0.20,
				},
			},
		},
		&cli.Command{
			Name:   "run-browsing",
			Usage:  "creates read-only load on service (notifications, timeline, etc)",
			Action: runBrowsing,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "catalog",
					Usage: "file path of account catalog JSON file",
					Value: "data/fakermaker/accounts.json",
				},
			},
		},
	}
	all := fakedata.MeasureIterations("entire command")
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		os.Exit(1)
	}
	all(1)
}

// registers fake accounts with PDS, and spits out JSON-lines to stdout with auth info
func genAccounts(ctx context.Context, cmd *cli.Command) error {

	// establish atproto client, with admin token for auth
	xrpcc, err := getXrpcClient(cmd, false)
	if err != nil {
		return err
	}
	adminToken := cmd.String("admin-password")
	if len(adminToken) > 0 {
		xrpcc.AdminToken = &adminToken
	}

	countTotal := cmd.Int("count")
	countCelebrities := cmd.Int("count-celebrities")
	domainSuffix := cmd.String("domain-suffix")
	if countCelebrities > countTotal {
		return fmt.Errorf("more celebrities than total accounts!")
	}
	countRegulars := countTotal - countCelebrities

	var inviteCode *string = nil
	if cmd.Bool("use-invite-code") {
		resp, err := comatproto.ServerCreateInviteCodes(context.TODO(), xrpcc, &comatproto.ServerCreateInviteCodes_Input{
			UseCount:    int64(countTotal),
			ForAccounts: nil,
			CodeCount:   1,
		})
		if err != nil {
			return err
		}
		if len(resp.Codes) != 1 || len(resp.Codes[0].Codes) != 1 {
			return fmt.Errorf("expected a single invite code")
		}
		inviteCode = &resp.Codes[0].Codes[0]
	}

	// call helper to do actual creation
	var usr *fakedata.AccountContext
	var line []byte
	t1 := fakedata.MeasureIterations("register celebrity accounts")
	for i := 0; i < countCelebrities; i++ {
		if usr, err = fakedata.GenAccount(xrpcc, i, "celebrity", domainSuffix, inviteCode); err != nil {
			return err
		}
		// compact single-line JSON by default
		if line, err = json.Marshal(usr); err != nil {
			return nil
		}
		fmt.Println(string(line))
	}
	t1(countCelebrities)

	t2 := fakedata.MeasureIterations("register regular accounts")
	for i := 0; i < countRegulars; i++ {
		if usr, err = fakedata.GenAccount(xrpcc, i, "regular", domainSuffix, inviteCode); err != nil {
			return err
		}
		// compact single-line JSON by default
		if line, err = json.Marshal(usr); err != nil {
			return nil
		}
		fmt.Println(string(line))
	}
	t2(countRegulars)
	return nil
}

func genProfiles(ctx context.Context, cmd *cli.Command) error {
	catalog, err := fakedata.ReadAccountCatalog(cmd.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cmd.String("pds-host")
	genAvatar := !cmd.Bool("no-avatars")
	genBanner := !cmd.Bool("no-banners")
	jobs := cmd.Int("jobs")

	accChan := make(chan fakedata.AccountContext, len(catalog.Celebs)+len(catalog.Regulars))
	eg := new(errgroup.Group)
	for i := 0; i < jobs; i++ {
		eg.Go(func() error {
			for acc := range accChan {
				xrpcc, err := fakedata.AccountXrpcClient(pdsHost, &acc)
				if err != nil {
					return err
				}
				if err = fakedata.GenProfile(xrpcc, &acc, genAvatar, genBanner); err != nil {
					return err
				}
			}
			return nil
		})
	}

	for _, acc := range append(catalog.Celebs, catalog.Regulars...) {
		accChan <- acc
	}
	close(accChan)
	return eg.Wait()
}

func genGraph(ctx context.Context, cmd *cli.Command) error {
	catalog, err := fakedata.ReadAccountCatalog(cmd.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cmd.String("pds-host")
	maxFollows := cmd.Int("max-follows")
	maxMutes := cmd.Int("max-mutes")
	jobs := cmd.Int("jobs")

	accChan := make(chan fakedata.AccountContext, len(catalog.Celebs)+len(catalog.Regulars))
	eg := new(errgroup.Group)
	for i := 0; i < jobs; i++ {
		eg.Go(func() error {
			for acc := range accChan {
				xrpcc, err := fakedata.AccountXrpcClient(pdsHost, &acc)
				if err != nil {
					return err
				}
				if err = fakedata.GenFollowsAndMutes(xrpcc, catalog, &acc, maxFollows, maxMutes); err != nil {
					return err
				}
			}
			return nil
		})
	}

	for _, acc := range append(catalog.Celebs, catalog.Regulars...) {
		accChan <- acc
	}
	close(accChan)
	return eg.Wait()
}

func genPosts(ctx context.Context, cmd *cli.Command) error {
	catalog, err := fakedata.ReadAccountCatalog(cmd.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cmd.String("pds-host")
	maxPosts := cmd.Int("max-posts")
	fracImage := cmd.Float64("frac-image")
	fracMention := cmd.Float64("frac-mention")
	jobs := cmd.Int("jobs")

	accChan := make(chan fakedata.AccountContext, len(catalog.Celebs)+len(catalog.Regulars))
	eg := new(errgroup.Group)
	for i := 0; i < jobs; i++ {
		eg.Go(func() error {
			for acc := range accChan {
				xrpcc, err := fakedata.AccountXrpcClient(pdsHost, &acc)
				if err != nil {
					return err
				}
				if err = fakedata.GenPosts(xrpcc, catalog, &acc, maxPosts, fracImage, fracMention); err != nil {
					return err
				}
			}
			return nil
		})
	}

	for _, acc := range append(catalog.Celebs, catalog.Regulars...) {
		accChan <- acc
	}
	close(accChan)
	return eg.Wait()
}

func genInteractions(ctx context.Context, cmd *cli.Command) error {
	catalog, err := fakedata.ReadAccountCatalog(cmd.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cmd.String("pds-host")
	fracLike := cmd.Float64("frac-like")
	fracRepost := cmd.Float64("frac-repost")
	fracReply := cmd.Float64("frac-reply")
	jobs := cmd.Int("jobs")

	accChan := make(chan fakedata.AccountContext, len(catalog.Celebs)+len(catalog.Regulars))
	eg := new(errgroup.Group)
	for i := 0; i < jobs; i++ {
		eg.Go(func() error {
			for acc := range accChan {
				xrpcc, err := fakedata.AccountXrpcClient(pdsHost, &acc)
				if err != nil {
					return err
				}
				t1 := fakedata.MeasureIterations("all interactions")
				if err := fakedata.GenLikesRepostsReplies(xrpcc, &acc, fracLike, fracRepost, fracReply); err != nil {
					return err
				}
				t1(1)
			}
			return nil
		})
	}

	for _, acc := range append(catalog.Celebs, catalog.Regulars...) {
		accChan <- acc
	}
	close(accChan)
	return eg.Wait()
}

func runBrowsing(ctx context.Context, cmd *cli.Command) error {
	catalog, err := fakedata.ReadAccountCatalog(cmd.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cmd.String("pds-host")
	jobs := cmd.Int("jobs")

	accChan := make(chan fakedata.AccountContext, len(catalog.Celebs)+len(catalog.Regulars))
	eg := new(errgroup.Group)
	for i := 0; i < jobs; i++ {
		eg.Go(func() error {
			for acc := range accChan {
				xrpcc, err := fakedata.AccountXrpcClient(pdsHost, &acc)
				if err != nil {
					return err
				}
				if err := fakedata.BrowseAccount(xrpcc, &acc); err != nil {
					return err
				}
			}
			return nil
		})
	}

	for _, acc := range append(catalog.Celebs, catalog.Regulars...) {
		accChan <- acc
	}
	close(accChan)
	return eg.Wait()
}

func getXrpcClient(cmd *cli.Command, authreq bool) (*xrpc.Client, error) {
	h := "http://localhost:4989"
	if pdsurl := cmd.String("pds-host"); pdsurl != "" {
		h = pdsurl
	}

	auth, err := loadAuthFromEnv(cmd, authreq)
	if err != nil {
		return nil, fmt.Errorf("loading auth: %w", err)
	}

	return &xrpc.Client{
		Client: cliutil.NewHttpClient(),
		Host:   h,
		Auth:   auth,
	}, nil
}

func loadAuthFromEnv(cmd *cli.Command, req bool) (*xrpc.AuthInfo, error) {
	if a := cmd.String("auth"); a != "" {
		if ai, err := cliutil.ReadAuth(a); err != nil && req {
			return nil, err
		} else {
			return ai, nil
		}
	}

	val := os.Getenv("ATP_AUTH_FILE")
	if val == "" {
		if req {
			return nil, fmt.Errorf("no auth env present, ATP_AUTH_FILE not set")
		}

		return nil, nil
	}

	var auth xrpc.AuthInfo
	if err := json.Unmarshal([]byte(val), &auth); err != nil {
		return nil, err
	}

	return &auth, nil
}
