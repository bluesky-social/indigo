// Tool to generate fake accounts, content, and interactions.
// Intended for development and benchmarking. Similar to 'stress' and could
// merge at some point.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/fakedata"
	"github.com/bluesky-social/indigo/version"

	_ "github.com/joho/godotenv/autoload"

	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
)

func main() {
	run(os.Args)
}

func run(args []string) {

	app := cli.App{
		Name:    "fakermaker",
		Usage:   "bluesky fake account/content generator",
		Version: version.Version,
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "pds-host",
			Usage:   "method, hostname, and port of PDS instance",
			Value:   "http://localhost:4849",
			EnvVars: []string{"ATP_PDS_HOST"},
		},
		&cli.StringFlag{
			Name:     "admin-password",
			Usage:    "admin authentication password for PDS",
			Required: true,
			EnvVars:  []string{"ATP_AUTH_ADMIN_PASSWORD"},
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
	app.RunAndExitOnError()
	all(1)
}

// registers fake accounts with PDS, and spits out JSON-lines to stdout with auth info
func genAccounts(cctx *cli.Context) error {

	// establish atproto client, with admin token for auth
	xrpcc, err := cliutil.GetXrpcClient(cctx, false)
	if err != nil {
		return err
	}
	adminToken := cctx.String("admin-password")
	if len(adminToken) > 0 {
		xrpcc.AdminToken = &adminToken
	}

	countTotal := cctx.Int("count")
	countCelebrities := cctx.Int("count-celebrities")
	if countCelebrities > countTotal {
		return fmt.Errorf("more celebrities than total accounts!")
	}
	countRegulars := countTotal - countCelebrities

	// call helper to do actual creation
	var usr *fakedata.AccountContext
	var line []byte
	t1 := fakedata.MeasureIterations("register celebrity accounts")
	for i := 0; i < countCelebrities; i++ {
		if usr, err = fakedata.GenAccount(xrpcc, i, "celebrity"); err != nil {
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
		if usr, err = fakedata.GenAccount(xrpcc, i, "regular"); err != nil {
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

func genProfiles(cctx *cli.Context) error {
	catalog, err := fakedata.ReadAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cctx.String("pds-host")
	genAvatar := !cctx.Bool("no-avatars")
	genBanner := !cctx.Bool("no-banners")
	jobs := cctx.Int("jobs")

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

func genGraph(cctx *cli.Context) error {
	catalog, err := fakedata.ReadAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cctx.String("pds-host")
	maxFollows := cctx.Int("max-follows")
	maxMutes := cctx.Int("max-mutes")
	jobs := cctx.Int("jobs")

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

func genPosts(cctx *cli.Context) error {
	catalog, err := fakedata.ReadAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cctx.String("pds-host")
	maxPosts := cctx.Int("max-posts")
	fracImage := cctx.Float64("frac-image")
	fracMention := cctx.Float64("frac-mention")
	jobs := cctx.Int("jobs")

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

func genInteractions(cctx *cli.Context) error {
	catalog, err := fakedata.ReadAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cctx.String("pds-host")
	fracLike := cctx.Float64("frac-like")
	fracRepost := cctx.Float64("frac-repost")
	fracReply := cctx.Float64("frac-reply")
	jobs := cctx.Int("jobs")

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

func runBrowsing(cctx *cli.Context) error {
	catalog, err := fakedata.ReadAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cctx.String("pds-host")
	jobs := cctx.Int("jobs")

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
