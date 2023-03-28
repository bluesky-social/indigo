// Tool to generate fake accounts, content, and interactions.
// Intended for development and benchmarking. Similar to 'stress' and could
// merge at some point.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/version"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/brianvoe/gofakeit/v6"

	_ "github.com/joho/godotenv/autoload"

	logging "github.com/ipfs/go-log"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
)

var log = logging.Logger("fakermaker")

func main() {
	run(os.Args)
}

func run(args []string) {

	app := cli.App{
		Name:  "fakermaker",
		Usage: "bluesky fake account/content generator",
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
					Value: 100,
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
	all := measureIterations("entire command")
	app.RunAndExitOnError()
	all(1)
}

type AccountContext struct {
	// 0-based index; should match index
	Index       int           `json:"index"`
	AccountType string        `json:"accountType"`
	Email       string        `json:"email"`
	Password    string        `json:"password"`
	Auth        xrpc.AuthInfo `json:"auth"`
}

func accountXrpcClient(cctx *cli.Context, ac *AccountContext) (*xrpc.Client, error) {
	pdsHost := cctx.String("pds-host")
	//httpClient := cliutil.NewHttpClient()
	httpClient := &http.Client{Timeout: 5 * time.Second}
	ua := "IndigoFakerMaker/" + version.Version
	xrpcc := &xrpc.Client{
		Client:    httpClient,
		Host:      pdsHost,
		Auth:      &ac.Auth,
		UserAgent: &ua,
	}
	// use XRPC client to re-auth using user/pass
	auth, err := comatproto.ServerCreateSession(context.TODO(), xrpcc, &comatproto.ServerCreateSession_Input{
		Identifier: &ac.Auth.Handle,
		Password:   ac.Password,
	})
	if err != nil {
		return nil, err
	}
	xrpcc.Auth.AccessJwt = auth.AccessJwt
	xrpcc.Auth.RefreshJwt = auth.RefreshJwt
	return xrpcc, nil
}

type AccountCatalog struct {
	Celebs   []AccountContext
	Regulars []AccountContext
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
	var usr *AccountContext
	var line []byte
	t1 := measureIterations("register celebrity accounts")
	for i := 0; i < countCelebrities; i++ {
		if usr, err = pdsGenAccount(xrpcc, i, "celebrity"); err != nil {
			return err
		}
		// compact single-line JSON by default
		if line, err = json.Marshal(usr); err != nil {
			return nil
		}
		fmt.Println(string(line))
	}
	t1(countCelebrities)

	t2 := measureIterations("register regular accounts")
	for i := 0; i < countRegulars; i++ {
		if usr, err = pdsGenAccount(xrpcc, i, "regular"); err != nil {
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

func measureIterations(name string) func(int) {
	start := time.Now()
	return func(count int) {
		if count == 0 {
			return
		}
		total := time.Since(start)
		log.Infof("%s wall runtime: count=%d total=%s mean=%s", name, count, total, total/time.Duration(count))
	}
}
func pdsGenAccount(xrpcc *xrpc.Client, index int, accountType string) (*AccountContext, error) {
	var suffix string
	if accountType == "celebrity" {
		suffix = "C"
	} else {
		suffix = ""
	}
	prefix := gofakeit.Username()
	if len(prefix) > 10 {
		prefix = prefix[0:10]
	}
	handle := fmt.Sprintf("%s-%s%d.test", prefix, suffix, index)
	email := gofakeit.Email()
	password := gofakeit.Password(true, true, true, true, true, 24)
	ctx := context.TODO()
	resp, err := comatproto.AccountCreate(ctx, xrpcc, &comatproto.AccountCreate_Input{
		Email:    email,
		Handle:   handle,
		Password: password,
	})
	if err != nil {
		return nil, err
	}
	auth := xrpc.AuthInfo{
		AccessJwt:  resp.AccessJwt,
		RefreshJwt: resp.RefreshJwt,
		Handle:     resp.Handle,
		Did:        resp.Did,
	}
	return &AccountContext{
		Index:       index,
		AccountType: accountType,
		Email:       email,
		Password:    password,
		Auth:        auth,
	}, nil
}

func readAccountCatalog(path string) (*AccountCatalog, error) {
	catalog := &AccountCatalog{}
	catFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer catFile.Close()

	decoder := json.NewDecoder(catFile)
	for decoder.More() {
		var usr AccountContext
		if err := decoder.Decode(&usr); err != nil {
			return nil, fmt.Errorf("parse AccountContext: %w", err)
		}
		if usr.AccountType == "celebrity" {
			catalog.Celebs = append(catalog.Celebs, usr)
		} else {
			catalog.Regulars = append(catalog.Regulars, usr)
		}
	}
	// validate index numbers
	for i, u := range catalog.Celebs {
		if i != u.Index {
			return nil, fmt.Errorf("account index didn't match: %d != %d (%s)", i, u.Index, u.AccountType)
		}
	}
	for i, u := range catalog.Regulars {
		if i != u.Index {
			return nil, fmt.Errorf("account index didn't match: %d != %d (%s)", i, u.Index, u.AccountType)
		}
	}
	log.Infof("loaded account catalog: regular=%d celebrity=%d", len(catalog.Regulars), len(catalog.Celebs))
	return catalog, nil
}

func genProfiles(cctx *cli.Context) error {
	catalog, err := readAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	genAvatar := !cctx.Bool("no-avatars")
	genBanner := !cctx.Bool("no-banners")
	jobs := cctx.Int("jobs")

	accChan := make(chan AccountContext, len(catalog.Celebs)+len(catalog.Regulars))
	eg := new(errgroup.Group)
	for i := 0; i < jobs; i++ {
		eg.Go(func() error {
			for acc := range accChan {
				xrpcc, err := accountXrpcClient(cctx, &acc)
				if err != nil {
					return err
				}
				if err = pdsGenProfile(xrpcc, &acc, genAvatar, genBanner); err != nil {
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

func pdsGenProfile(xrpcc *xrpc.Client, acc *AccountContext, genAvatar, genBanner bool) error {

	desc := gofakeit.HipsterSentence(12)
	var name string
	if acc.AccountType == "celebrity" {
		name = gofakeit.CelebrityActor()
	} else {
		name = gofakeit.Name()
	}

	var avatar *lexutil.Blob
	if genAvatar {
		img := gofakeit.ImagePng(200, 200)
		resp, err := comatproto.BlobUpload(context.TODO(), xrpcc, bytes.NewReader(img))
		if err != nil {
			return err
		}
		avatar = &lexutil.Blob{
			Cid:      resp.Cid,
			MimeType: "image/png",
		}
	}
	var banner *lexutil.Blob
	if genBanner {
		img := gofakeit.ImageJpeg(800, 200)
		resp, err := comatproto.BlobUpload(context.TODO(), xrpcc, bytes.NewReader(img))
		if err != nil {
			return err
		}
		avatar = &lexutil.Blob{
			Cid:      resp.Cid,
			MimeType: "image/jpeg",
		}
	}

	_, err := comatproto.RepoPutRecord(context.TODO(), xrpcc, &comatproto.RepoPutRecord_Input{
		Repo:       acc.Auth.Did,
		Collection: "app.bsky.actor.profile",
		Rkey:       "self",
		Record: lexutil.LexiconTypeDecoder{&appbsky.ActorProfile{
			Description: &desc,
			DisplayName: &name,
			Avatar:      avatar,
			Banner:      banner,
		}},
	})
	return err
}

func genGraph(cctx *cli.Context) error {
	catalog, err := readAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	maxFollows := cctx.Int("max-follows")
	maxMutes := cctx.Int("max-mutes")
	jobs := cctx.Int("jobs")

	accChan := make(chan AccountContext, len(catalog.Celebs)+len(catalog.Regulars))
	eg := new(errgroup.Group)
	for i := 0; i < jobs; i++ {
		eg.Go(func() error {
			for acc := range accChan {
				xrpcc, err := accountXrpcClient(cctx, &acc)
				if err != nil {
					return err
				}
				if err = pdsGenFollowsAndMutes(xrpcc, catalog, &acc, maxFollows, maxMutes); err != nil {
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

func pdsGenPosts(xrpcc *xrpc.Client, catalog *AccountCatalog, acc *AccountContext, maxPosts int, fracImage float64, fracMention float64) error {

	var mention *appbsky.FeedPost_Entity
	var tgt *AccountContext
	var text string
	ctx := context.TODO()

	if maxPosts < 1 {
		return nil
	}
	count := rand.Intn(maxPosts)

	// celebrities make 2x the posts
	if acc.AccountType == "celebrity" {
		count = count * 2
	}
	t1 := measureIterations("generate posts")
	for i := 0; i < count; i++ {
		text = gofakeit.Sentence(10)
		if len(text) > 200 {
			text = text[0:200]
		}

		// half the time, mention a celeb
		tgt = nil
		mention = nil
		if fracMention > 0.0 && rand.Float64() < fracMention/2 {
			tgt = &catalog.Regulars[rand.Intn(len(catalog.Regulars))]
		} else if fracMention > 0.0 && rand.Float64() < fracMention/2 {
			tgt = &catalog.Celebs[rand.Intn(len(catalog.Celebs))]
		}
		if tgt != nil {
			text = "@" + tgt.Auth.Handle + " " + text
			mention = &appbsky.FeedPost_Entity{
				Type:  "mention",
				Value: tgt.Auth.Did,
				Index: &appbsky.FeedPost_TextSlice{
					Start: 0,
					End:   int64(len(tgt.Auth.Handle) + 1),
				},
			}
		}

		var images []*appbsky.EmbedImages_Image
		if fracImage > 0.0 && rand.Float64() < fracImage {
			img := gofakeit.ImageJpeg(800, 800)
			resp, err := comatproto.BlobUpload(context.TODO(), xrpcc, bytes.NewReader(img))
			if err != nil {
				return err
			}
			images = append(images, &appbsky.EmbedImages_Image{
				Alt: gofakeit.Lunch(),
				Image: &lexutil.Blob{
					Cid:      resp.Cid,
					MimeType: "image/jpeg",
				},
			})
		}
		post := appbsky.FeedPost{
			Text:      text,
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		if mention != nil {
			post.Entities = []*appbsky.FeedPost_Entity{mention}
		}
		if len(images) > 0 {
			post.Embed = &appbsky.FeedPost_Embed{
				EmbedImages: &appbsky.EmbedImages{
					Images: images,
				},
			}
		}
		if _, err := comatproto.RepoCreateRecord(ctx, xrpcc, &comatproto.RepoCreateRecord_Input{
			Collection: "app.bsky.feed.post",
			Repo:       acc.Auth.Did,
			Record:     lexutil.LexiconTypeDecoder{&post},
		}); err != nil {
			return err
		}
	}
	t1(count)
	return nil
}

func pdsCreateFollow(xrpcc *xrpc.Client, tgt *AccountContext) error {
	follow := &appbsky.GraphFollow{
		CreatedAt: time.Now().Format(time.RFC3339),
		Subject:   tgt.Auth.Did,
	}
	_, err := comatproto.RepoCreateRecord(context.TODO(), xrpcc, &comatproto.RepoCreateRecord_Input{
		Collection: "app.bsky.graph.follow",
		Repo:       xrpcc.Auth.Did,
		Record:     lexutil.LexiconTypeDecoder{follow},
	})
	return err
}

func pdsCreateLike(xrpcc *xrpc.Client, viewPost *appbsky.FeedDefs_FeedViewPost) error {
	ctx := context.TODO()
	like := appbsky.FeedLike{
		Subject: &comatproto.RepoStrongRef{
			Uri: viewPost.Post.Uri,
			Cid: viewPost.Post.Cid,
		},
	}
	// TODO: may have already like? in that case should ignore error
	_, err := comatproto.RepoCreateRecord(ctx, xrpcc, &comatproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.like",
		Repo:       xrpcc.Auth.Did,
		Record:     lexutil.LexiconTypeDecoder{&like},
	})
	return err
}

func pdsCreateRepost(xrpcc *xrpc.Client, viewPost *appbsky.FeedDefs_FeedViewPost) error {
	repost := &appbsky.FeedRepost{
		CreatedAt: time.Now().Format(time.RFC3339),
		Subject: &comatproto.RepoStrongRef{
			Uri: viewPost.Post.Uri,
			Cid: viewPost.Post.Cid,
		},
	}
	_, err := comatproto.RepoCreateRecord(context.TODO(), xrpcc, &comatproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.repost",
		Repo:       xrpcc.Auth.Did,
		Record:     lexutil.LexiconTypeDecoder{repost},
	})
	return err
}

func pdsCreateReply(xrpcc *xrpc.Client, viewPost *appbsky.FeedDefs_FeedViewPost) error {
	text := gofakeit.Sentence(10)
	if len(text) > 200 {
		text = text[0:200]
	}
	parent := &comatproto.RepoStrongRef{
		Uri: viewPost.Post.Uri,
		Cid: viewPost.Post.Cid,
	}
	root := parent
	if viewPost.Reply != nil {
		root = &comatproto.RepoStrongRef{
			Uri: viewPost.Reply.Root.Uri,
			Cid: viewPost.Reply.Root.Cid,
		}
	}
	replyPost := &appbsky.FeedPost{
		CreatedAt: time.Now().Format(time.RFC3339),
		Text:      text,
		Reply: &appbsky.FeedPost_ReplyRef{
			Parent: parent,
			Root:   root,
		},
	}
	_, err := comatproto.RepoCreateRecord(context.TODO(), xrpcc, &comatproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.post",
		Repo:       xrpcc.Auth.Did,
		Record:     lexutil.LexiconTypeDecoder{replyPost},
	})
	return err
}

func pdsGenFollowsAndMutes(xrpcc *xrpc.Client, catalog *AccountCatalog, acc *AccountContext, maxFollows int, maxMutes int) error {

	// TODO: have a "shape" to likelihood of doing a follow
	var tgt *AccountContext

	if maxFollows > len(catalog.Regulars) {
		return fmt.Errorf("not enought regulars to pick maxFollowers from")
	}
	if maxMutes > len(catalog.Regulars) {
		return fmt.Errorf("not enought regulars to pick maxMutes from")
	}

	regCount := 0
	celebCount := 0
	if maxFollows >= 1 {
		regCount = rand.Intn(maxFollows)
		celebCount = rand.Intn(len(catalog.Celebs))
	}
	t1 := measureIterations("generate follows")
	for idx := range rand.Perm(len(catalog.Celebs))[:celebCount] {
		tgt = &catalog.Celebs[idx]
		if tgt.Auth.Did == acc.Auth.Did {
			continue
		}
		if err := pdsCreateFollow(xrpcc, tgt); err != nil {
			return err
		}
	}
	for idx := range rand.Perm(len(catalog.Regulars))[:regCount] {
		tgt = &catalog.Regulars[idx]
		if tgt.Auth.Did == acc.Auth.Did {
			continue
		}
		if err := pdsCreateFollow(xrpcc, tgt); err != nil {
			return err
		}
	}
	t1(regCount + celebCount)

	// only muting other users, not celebs
	muteCount := 0
	if maxFollows >= 1 {
		muteCount = rand.Intn(maxMutes)
	}
	t2 := measureIterations("generate mutes")
	for idx := range rand.Perm(len(catalog.Regulars))[:muteCount] {
		tgt = &catalog.Regulars[idx]
		if tgt.Auth.Did == acc.Auth.Did {
			continue
		}
		if err := appbsky.GraphMuteActor(context.TODO(), xrpcc, &appbsky.GraphMuteActor_Input{Actor: tgt.Auth.Did}); err != nil {
			return err
		}
	}
	t2(muteCount)
	return nil
}

func genPosts(cctx *cli.Context) error {
	catalog, err := readAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	maxPosts := cctx.Int("max-posts")
	fracImage := cctx.Float64("frac-image")
	fracMention := cctx.Float64("frac-mention")
	jobs := cctx.Int("jobs")

	accChan := make(chan AccountContext, len(catalog.Celebs)+len(catalog.Regulars))
	eg := new(errgroup.Group)
	for i := 0; i < jobs; i++ {
		eg.Go(func() error {
			for acc := range accChan {
				xrpcc, err := accountXrpcClient(cctx, &acc)
				if err != nil {
					return err
				}
				if err = pdsGenPosts(xrpcc, catalog, &acc, maxPosts, fracImage, fracMention); err != nil {
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
	catalog, err := readAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	fracLike := cctx.Float64("frac-like")
	fracRepost := cctx.Float64("frac-repost")
	fracReply := cctx.Float64("frac-reply")
	jobs := cctx.Int("jobs")

	accChan := make(chan AccountContext, len(catalog.Celebs)+len(catalog.Regulars))
	eg := new(errgroup.Group)
	for i := 0; i < jobs; i++ {
		eg.Go(func() error {
			for acc := range accChan {
				xrpcc, err := accountXrpcClient(cctx, &acc)
				if err != nil {
					return err
				}
				t1 := measureIterations("all interactions")
				// fetch timeline (up to 100), and iterate over posts
				maxTimeline := 100
				resp, err := appbsky.FeedGetTimeline(context.TODO(), xrpcc, "", "", int64(maxTimeline))
				if err != nil {
					return err
				}
				if len(resp.Feed) > maxTimeline {
					return fmt.Errorf("got too long timeline len=%d", len(resp.Feed))
				}
				for _, post := range resp.Feed {
					// skip account's own posts
					if post.Post.Author.Did == acc.Auth.Did {
						continue
					}

					// generate
					if fracLike > 0.0 && rand.Float64() < fracLike {
						if err := pdsCreateLike(xrpcc, post); err != nil {
							return err
						}
					}
					if fracRepost > 0.0 && rand.Float64() < fracRepost {
						if err := pdsCreateRepost(xrpcc, post); err != nil {
							return err
						}
					}
					if fracReply > 0.0 && rand.Float64() < fracReply {
						if err := pdsCreateReply(xrpcc, post); err != nil {
							return err
						}
					}
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

func browseAccount(xrpcc *xrpc.Client, acc *AccountContext) error {
	// fetch notifications
	maxNotif := 50
	resp, err := appbsky.NotificationListNotifications(context.TODO(), xrpcc, "", int64(maxNotif))
	if err != nil {
		return err
	}
	if len(resp.Notifications) > maxNotif {
		return fmt.Errorf("got too many notifications len=%d", len(resp.Notifications))
	}
	t1 := measureIterations("notification interactions")
	for _, notif := range resp.Notifications {
		switch notif.Reason {
		case "vote":
			fallthrough
		case "repost":
			fallthrough
		case "follow":
			_, err := appbsky.ActorGetProfile(context.TODO(), xrpcc, notif.Author.Did)
			if err != nil {
				return err
			}
			_, err = appbsky.FeedGetAuthorFeed(context.TODO(), xrpcc, notif.Author.Did, "", 50)
			if err != nil {
				return err
			}
		case "mention":
			fallthrough
		case "reply":
			_, err := appbsky.FeedGetPostThread(context.TODO(), xrpcc, 4, notif.Uri)
			if err != nil {
				return err
			}
		default:
		}
	}
	t1(len(resp.Notifications))

	// fetch timeline (up to 100), and iterate over posts
	timelineLen := 100
	timelineResp, err := appbsky.FeedGetTimeline(context.TODO(), xrpcc, "", "", int64(timelineLen))
	if err != nil {
		return err
	}
	if len(timelineResp.Feed) > timelineLen {
		return fmt.Errorf("longer than expected timeline len=%d", len(timelineResp.Feed))
	}
	t2 := measureIterations("timeline interactions")
	for _, post := range timelineResp.Feed {
		// skip account's own posts
		if post.Post.Author.Did == acc.Auth.Did {
			continue
		}
		// TODO: should we do something different here?
		if rand.Float64() < 0.25 {
			_, err = appbsky.FeedGetPostThread(context.TODO(), xrpcc, 4, post.Post.Uri)
			if err != nil {
				return err
			}
		} else if rand.Float64() < 0.25 {
			_, err = appbsky.ActorGetProfile(context.TODO(), xrpcc, post.Post.Author.Did)
			if err != nil {
				return err
			}
			_, err = appbsky.FeedGetAuthorFeed(context.TODO(), xrpcc, post.Post.Author.Did, "", 50)
			if err != nil {
				return err
			}
		}
	}
	t2(len(timelineResp.Feed))

	// notification count for good measure
	_, err = appbsky.NotificationGetUnreadCount(context.TODO(), xrpcc)
	return err
}

func runBrowsing(cctx *cli.Context) error {
	catalog, err := readAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	jobs := cctx.Int("jobs")

	accChan := make(chan AccountContext, len(catalog.Celebs)+len(catalog.Regulars))
	eg := new(errgroup.Group)
	for i := 0; i < jobs; i++ {
		eg.Go(func() error {
			for acc := range accChan {
				xrpcc, err := accountXrpcClient(cctx, &acc)
				if err != nil {
					return err
				}
				if err := browseAccount(xrpcc, &acc); err != nil {
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
