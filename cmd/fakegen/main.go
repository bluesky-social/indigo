// Tool to generate fake accounts and fake content.
// Intended for development and benchmarking. Similar to 'stress' and could
// merge at some point.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/bluesky-social/indigo/api"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	cliutil "github.com/bluesky-social/indigo/cmd/gosky/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/brianvoe/gofakeit/v6"
	logging "github.com/ipfs/go-log"
	"github.com/joho/godotenv"
	"github.com/urfave/cli/v2"
)

var log = logging.Logger("fakegen")

func main() {

	// only try dotenv if it exists
	if _, err := os.Stat(".env"); err == nil {
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	app := &cli.App{
		Name:  "fakegen",
		Usage: "bluesky fake account/content generator",
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "pds",
			Usage:   "hostname and port of PDS instance",
			Value:   "http://localhost:4849",
			EnvVars: []string{"ATP_PDS_HOST"},
		},
		&cli.StringFlag{
			Name:     "admin-token",
			Usage:    "admin authentication token for PDS",
			Required: true,
			EnvVars:  []string{"BSKY_ADMIN_AUTH"},
		},
	}
	app.Commands = []*cli.Command{
		&cli.Command{
			Name:   "gen-accounts",
			Usage:  "create accounts (DID, handle, no content)",
			Action: genAccounts,
			Flags: []cli.Flag{
				&cli.IntFlag{
					Name:  "count-regulars",
					Usage: "number of regular accounts to create",
					Value: 100,
				},
				&cli.IntFlag{
					Name:  "count-celebrities",
					Usage: "number of 'celebrity' accountss to create",
					Value: 10,
				},
			},
		},
		&cli.Command{
			Name:   "gen-initial-content",
			Usage:  "creates social graph and initial posts for accounts",
			Action: genInitialContent,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "catalog",
					Usage: "file path of account catalog JSON file",
					Value: "fakegen-accounts.json",
				},
				&cli.IntFlag{
					Name:  "max-posts",
					Usage: "create up to this many posts for each account; celebs do 2x",
					Value: 100,
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
				&cli.Float64Flag{
					Name:  "frac-image",
					Usage: "portion of posts to include images in",
					Value: 0.25,
				},
				&cli.Float64Flag{
					Name:  "frac-mention",
					Usage: "of posts created, fraction to include mentions in",
					Value: 0.25,
				},
			},
		},
		&cli.Command{
			Name:   "gen-content",
			Usage:  "creates posts and interactions for accounts",
			Action: genContent,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "catalog",
					Usage: "file path of account catalog JSON file",
					Value: "fakegen-accounts.json",
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
			Name:   "bench-browsing",
			Usage:  "creates read-only load on service (notifications, timeline, etc)",
			Action: benchBrowsing,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "catalog",
					Usage: "file path of account catalog JSON file",
					Value: "fakegen-accounts.json",
				},
			},
		},
	}
	all := measureIterations("entire command", 1)
	app.RunAndExitOnError()
	all()
}

type AccountContext struct {
	// 0-based index; should match index
	Index       int           `json:"index"`
	AccountType string        `json:"accountType"`
	Email       string        `json:"email"`
	Password    string        `json:"password"`
	Auth        xrpc.AuthInfo `json:"auth"`
}

type AccountCatalog struct {
	Celebs   []AccountContext
	Regulars []AccountContext
}

// registers fake accounts with PDS, and spits out JSON-lines to stdout with auth info
func genAccounts(cctx *cli.Context) error {

	// establish atproto client, with admin token for auth
	atpc, err := cliutil.GetATPClient(cctx, false)
	if err != nil {
		return err
	}
	adminToken := cctx.String("admin-token")
	if len(adminToken) > 0 {
		atpc.C.AdminToken = &adminToken
	}

	// call helper to do actual creation
	var usr *AccountContext
	var line []byte
	countCelebrities := cctx.Int("count-celebrities")
	t1 := measureIterations("register celebrity accounts", countCelebrities)
	for i := 0; i < countCelebrities; i++ {
		if usr, err = pdsGenAccount(atpc, i, "celebrity"); err != nil {
			return err
		}
		// compact single-line JSON by default
		if line, err = json.Marshal(usr); err != nil {
			return nil
		}
		fmt.Println(string(line))
	}
	t1()
	countRegulars := cctx.Int("count-regulars")
	t2 := measureIterations("register regular accounts", countRegulars)
	for i := 0; i < countRegulars; i++ {
		if usr, err = pdsGenAccount(atpc, i, "regular"); err != nil {
			return err
		}
		// compact single-line JSON by default
		if line, err = json.Marshal(usr); err != nil {
			return nil
		}
		fmt.Println(string(line))
	}
	t2()
	return nil
}

func measureIterations(name string, count int) func() {
	if count == 0 {
		return func() {}
	}
	start := time.Now()
	return func() {
		total := time.Since(start)
		log.Infof("%s wall runtime: count=%d total=%s mean=%s", name, count, total, total/time.Duration(count))
	}
}

func pdsGenAccount(atpc *api.ATProto, index int, accountType string) (*AccountContext, error) {
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
	resp, err := atpc.CreateAccount(ctx, email, handle, password, nil)
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

func genInitialContent(cctx *cli.Context) error {
	catalog, err := readAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cctx.String("pds")
	maxPosts := cctx.Int("max-posts")
	maxFollows := cctx.Int("max-follows")
	maxMutes := cctx.Int("max-mutes")
	fracImage := cctx.Float64("frac-image")
	fracMention := cctx.Float64("frac-mention")
	httpClient := cliutil.NewHttpClient()

	if maxFollows > len(catalog.Regulars) {
		return fmt.Errorf("not enought regulars to pick maxFollowers from")
	}
	if maxMutes > len(catalog.Regulars) {
		return fmt.Errorf("not enought regulars to pick maxMutes from")
	}

	// TODO: profile: avatar, display name, description
	for _, acc := range append(catalog.Celebs, catalog.Regulars...) {
		// create an ATP client for this specific user
		atpc := &api.ATProto{
			C: &xrpc.Client{
				Client: httpClient,
				Host:   pdsHost,
				Auth:   &acc.Auth,
			},
		}

		if err = pdsGenPosts(atpc, catalog, &acc, maxPosts, fracImage, fracMention); err != nil {
			return err
		}
		if err = pdsGenFollowsAndMutes(atpc, catalog, &acc, maxFollows, maxMutes); err != nil {
			return err
		}
	}
	return nil
}

func pdsGenPosts(atpc *api.ATProto, catalog *AccountCatalog, acc *AccountContext, maxPosts int, fracImage float64, fracMention float64) error {

	var mention *appbsky.FeedPost_Entity
	var tgt *AccountContext
	var embed *appbsky.FeedPost_Embed
	var text string
	ctx := context.TODO()

	count := rand.Intn(maxPosts)

	// celebrities make 2x the posts
	if acc.AccountType == "celebrity" {
		count = count * 2
	}
	t1 := measureIterations("generate posts", count)
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

		embed = nil
		if fracImage > 0.0 && rand.Float64() < fracImage {
			// XXX: add some images
		}
		post := appbsky.FeedPost{
			Text:      text,
			Embed:     embed,
			CreatedAt: time.Now().Format(time.RFC3339),
		}
		if mention != nil {
			post.Entities = []*appbsky.FeedPost_Entity{mention}
		}
		if _, err := atpc.RepoCreateRecord(ctx, acc.Auth.Did, "app.bsky.feed.post", true, &post); err != nil {
			return err
		}
	}
	t1()
	return nil
}

func pdsCreateFollow(atpc *api.ATProto, tgt *AccountContext) error {
	follow := &appbsky.GraphFollow{
		CreatedAt: time.Now().Format(time.RFC3339),
		Subject: &appbsky.ActorRef{
			Did: tgt.Auth.Did,
			// TODO: this should be a public exported const, not hardcoded here
			DeclarationCid: "bafyreid27zk7lbis4zw5fz4podbvbs4fc5ivwji3dmrwa6zggnj4bnd57u",
		},
	}
	_, err := atpc.RepoCreateRecord(context.TODO(), atpc.C.Auth.Did, "app.bsky.graph.follow", true, &follow)
	return err
}

func pdsCreateLike(atpc *api.ATProto, viewPost *appbsky.FeedFeedViewPost) error {
	vote := &appbsky.FeedSetVote_Input{
		Direction: "up",
		Subject: &comatproto.RepoStrongRef{
			Uri: viewPost.Post.Uri,
			Cid: viewPost.Post.Cid,
		},
	}
	// TODO: may have already voted? in that case should ignore error
	_, err := appbsky.FeedSetVote(context.TODO(), atpc.C, vote)
	return err
}

func pdsCreateRepost(atpc *api.ATProto, viewPost *appbsky.FeedFeedViewPost) error {
	repost := &appbsky.FeedRepost{
		CreatedAt: time.Now().Format(time.RFC3339),
		Subject: &comatproto.RepoStrongRef{
			Uri: viewPost.Post.Uri,
			Cid: viewPost.Post.Cid,
		},
	}
	_, err := atpc.RepoCreateRecord(context.TODO(), atpc.C.Auth.Did, "app.bsky.feed.repost", true, &repost)
	return err
}

func pdsCreateReply(atpc *api.ATProto, viewPost *appbsky.FeedFeedViewPost) error {
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
	_, err := atpc.RepoCreateRecord(context.TODO(), atpc.C.Auth.Did, "app.bsky.feed.post", true, &replyPost)
	return err
}

func pdsGenFollowsAndMutes(atpc *api.ATProto, catalog *AccountCatalog, acc *AccountContext, maxFollows int, maxMutes int) error {

	// TODO: have a "shape" to likelihood of doing a follow
	var tgt *AccountContext

	regCount := rand.Intn(maxFollows)
	celebCount := rand.Intn(len(catalog.Celebs))
	t1 := measureIterations("generate follows", regCount+celebCount)
	for idx := range rand.Perm(len(catalog.Celebs))[:celebCount] {
		tgt = &catalog.Celebs[idx]
		if tgt.Auth.Did == acc.Auth.Did {
			continue
		}
		if err := pdsCreateFollow(atpc, tgt); err != nil {
			return err
		}
	}
	for idx := range rand.Perm(len(catalog.Regulars))[:regCount] {
		tgt = &catalog.Regulars[idx]
		if tgt.Auth.Did == acc.Auth.Did {
			continue
		}
		if err := pdsCreateFollow(atpc, tgt); err != nil {
			return err
		}
	}
	t1()

	// only muting other users, not celebs
	muteCount := rand.Intn(maxMutes)
	t2 := measureIterations("generate mutes", muteCount)
	for idx := range rand.Perm(len(catalog.Regulars))[:muteCount] {
		tgt = &catalog.Regulars[idx]
		if tgt.Auth.Did == acc.Auth.Did {
			continue
		}
		if err := appbsky.GraphMute(context.TODO(), atpc.C, &appbsky.GraphMute_Input{User: tgt.Auth.Did}); err != nil {
			return err
		}
	}
	t2()
	return nil
}

func genContent(cctx *cli.Context) error {
	catalog, err := readAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cctx.String("pds")
	maxPosts := cctx.Int("max-posts")
	fracImage := cctx.Float64("frac-image")
	fracMention := cctx.Float64("frac-mention")
	fracLike := cctx.Float64("frac-like")
	fracRepost := cctx.Float64("frac-repost")
	fracReply := cctx.Float64("frac-reply")
	httpClient := cliutil.NewHttpClient()

	// TODO: profile: avatar, display name, description
	for _, acc := range append(catalog.Celebs, catalog.Regulars...) {
		// create an ATP client for this specific user
		atpc := &api.ATProto{
			C: &xrpc.Client{
				Client: httpClient,
				Host:   pdsHost,
				Auth:   &acc.Auth,
			},
		}

		// generate some more posts, similar to before (but fewer)
		if err = pdsGenPosts(atpc, catalog, &acc, maxPosts, fracImage, fracMention); err != nil {
			return err
		}

		t1 := measureIterations("all interactions", 1)
		// fetch timeline (up to 100), and iterate over posts
		resp, err := appbsky.FeedGetTimeline(context.TODO(), atpc.C, "", "", 100)
		if err != nil {
			return err
		}
		for _, post := range resp.Feed {
			// skip account's own posts
			if post.Post.Author.Did == acc.Auth.Did {
				continue
			}

			// generate
			if fracLike > 0.0 && rand.Float64() < fracLike {
				if err := pdsCreateLike(atpc, post); err != nil {
					return err
				}
			}
			if fracRepost > 0.0 && rand.Float64() < fracRepost {
				if err := pdsCreateRepost(atpc, post); err != nil {
					return err
				}
			}
			if fracReply > 0.0 && rand.Float64() < fracReply {
				if err := pdsCreateReply(atpc, post); err != nil {
					return err
				}
			}
		}
		t1()
	}
	return nil
}

func benchBrowsing(cctx *cli.Context) error {
	catalog, err := readAccountCatalog(cctx.String("catalog"))
	if err != nil {
		return err
	}

	pdsHost := cctx.String("pds")
	httpClient := cliutil.NewHttpClient()

	for _, acc := range append(catalog.Celebs, catalog.Regulars...) {
		// create an ATP client for this specific user
		atpc := &api.ATProto{
			C: &xrpc.Client{
				Client: httpClient,
				Host:   pdsHost,
				Auth:   &acc.Auth,
			},
		}

		// fetch notifications
		resp, err := appbsky.NotificationList(context.TODO(), atpc.C, "", 50)
		if err != nil {
			return err
		}
		t1 := measureIterations("notification interactions", len(resp.Notifications))
		for _, notif := range resp.Notifications {
			switch notif.Reason {
			case "vote":
				fallthrough
			case "repost":
				fallthrough
			case "follow":
				_, err := appbsky.ActorGetProfile(context.TODO(), atpc.C, notif.Author.Did)
				if err != nil {
					return err
				}
				_, err = appbsky.FeedGetAuthorFeed(context.TODO(), atpc.C, notif.Author.Did, "", 50)
				if err != nil {
					return err
				}
			case "mention":
				fallthrough
			case "reply":
				_, err := appbsky.FeedGetPostThread(context.TODO(), atpc.C, 4, notif.Uri)
				if err != nil {
					return err
				}
			default:
			}
		}
		t1()

		// fetch timeline (up to 100), and iterate over posts
		timelineResp, err := appbsky.FeedGetTimeline(context.TODO(), atpc.C, "", "", 100)
		if err != nil {
			return err
		}
		t2 := measureIterations("timeline interactions", len(timelineResp.Feed))
		for _, post := range timelineResp.Feed {
			// skip account's own posts
			if post.Post.Author.Did == acc.Auth.Did {
				continue
			}
			// TODO: should we do something different here?
			if rand.Float64() < 0.25 {
				_, err = appbsky.FeedGetPostThread(context.TODO(), atpc.C, 4, post.Post.Uri)
				if err != nil {
					return err
				}
			} else if rand.Float64() < 0.25 {
				_, err = appbsky.ActorGetProfile(context.TODO(), atpc.C, post.Post.Author.Did)
				if err != nil {
					return err
				}
				_, err = appbsky.FeedGetAuthorFeed(context.TODO(), atpc.C, post.Post.Author.Did, "", 50)
				if err != nil {
					return err
				}
			}
		}
		t2()

		// notification count for good measure
		_, err = appbsky.NotificationGetCount(context.TODO(), atpc.C)
	}
	return nil
}
