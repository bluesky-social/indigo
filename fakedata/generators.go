// Helpers for randomly generated app.bsky.* content: accounts, posts, likes,
// follows, mentions, etc

package fakedata

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"

	"github.com/brianvoe/gofakeit/v6"
)

var log = slog.Default().With("system", "fakedata")

func SetLogger(logger *slog.Logger) {
	log = logger
}

func MeasureIterations(name string) func(int) {
	start := time.Now()
	return func(count int) {
		if count == 0 {
			return
		}
		total := time.Since(start)
		log.Info("wall runtime", "name", name, "count", count, "total", total, "rate", total/time.Duration(count))
	}
}

func GenAccount(xrpcc *xrpc.Client, index int, accountType, domainSuffix string, inviteCode *string) (*AccountContext, error) {
	if domainSuffix == "" {
		domainSuffix = "test"
	}
	var handleSuffix string
	if accountType == "celebrity" {
		handleSuffix = "C"
	} else {
		handleSuffix = ""
	}
	prefix := gofakeit.Username()
	if len(prefix) > 10 {
		prefix = prefix[0:10]
	}
	handle := fmt.Sprintf("%s-%s%d.%s", prefix, handleSuffix, index, domainSuffix)
	email := gofakeit.Email()
	password := gofakeit.Password(true, true, true, true, true, 24)
	ctx := context.TODO()
	resp, err := comatproto.ServerCreateAccount(ctx, xrpcc, &comatproto.ServerCreateAccount_Input{
		Email:      &email,
		Handle:     handle,
		InviteCode: inviteCode,
		Password:   &password,
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

func GenProfile(xrpcc *xrpc.Client, acc *AccountContext, genAvatar, genBanner bool) error {

	desc := gofakeit.HipsterSentence(12)
	var name string
	if acc.AccountType == "celebrity" {
		name = gofakeit.CelebrityActor()
	} else {
		name = gofakeit.Name()
	}

	var avatar *lexutil.LexBlob
	if genAvatar {
		img := gofakeit.ImagePng(200, 200)
		resp, err := comatproto.RepoUploadBlob(context.TODO(), xrpcc, bytes.NewReader(img))
		if err != nil {
			return err
		}
		avatar = &lexutil.LexBlob{
			Ref:      resp.Blob.Ref,
			MimeType: "image/png",
			Size:     resp.Blob.Size,
		}
	}
	var banner *lexutil.LexBlob
	if genBanner {
		img := gofakeit.ImageJpeg(800, 200)
		resp, err := comatproto.RepoUploadBlob(context.TODO(), xrpcc, bytes.NewReader(img))
		if err != nil {
			return err
		}
		banner = &lexutil.LexBlob{
			Ref:      resp.Blob.Ref,
			MimeType: "image/jpeg",
			Size:     resp.Blob.Size,
		}
	}

	_, err := comatproto.RepoPutRecord(context.TODO(), xrpcc, &comatproto.RepoPutRecord_Input{
		Repo:       acc.Auth.Did,
		Collection: "app.bsky.actor.profile",
		Rkey:       "self",
		Record: &lexutil.LexiconTypeDecoder{Val: &appbsky.ActorProfile{
			Description: &desc,
			DisplayName: &name,
			Avatar:      avatar,
			Banner:      banner,
		}},
	})
	return err
}

func GenPosts(xrpcc *xrpc.Client, catalog *AccountCatalog, acc *AccountContext, maxPosts int, fracImage float64, fracMention float64) error {

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
	t1 := MeasureIterations("generate posts")
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
			resp, err := comatproto.RepoUploadBlob(context.TODO(), xrpcc, bytes.NewReader(img))
			if err != nil {
				return err
			}
			images = append(images, &appbsky.EmbedImages_Image{
				Alt: gofakeit.Lunch(),
				Image: &lexutil.LexBlob{
					Ref:      resp.Blob.Ref,
					MimeType: "image/jpeg",
					Size:     resp.Blob.Size,
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
			Record:     &lexutil.LexiconTypeDecoder{Val: &post},
		}); err != nil {
			return err
		}
	}
	t1(count)
	return nil
}

func CreateFollow(xrpcc *xrpc.Client, tgt *AccountContext) error {
	follow := &appbsky.GraphFollow{
		CreatedAt: time.Now().Format(time.RFC3339),
		Subject:   tgt.Auth.Did,
	}
	_, err := comatproto.RepoCreateRecord(context.TODO(), xrpcc, &comatproto.RepoCreateRecord_Input{
		Collection: "app.bsky.graph.follow",
		Repo:       xrpcc.Auth.Did,
		Record:     &lexutil.LexiconTypeDecoder{Val: follow},
	})
	return err
}

func CreateLike(xrpcc *xrpc.Client, viewPost *appbsky.FeedDefs_FeedViewPost) error {
	ctx := context.TODO()
	like := appbsky.FeedLike{
		Subject: &comatproto.RepoStrongRef{
			Uri: viewPost.Post.Uri,
			Cid: viewPost.Post.Cid,
		},
		CreatedAt: time.Now().Format(time.RFC3339),
	}
	// TODO: may have already like? in that case should ignore error
	_, err := comatproto.RepoCreateRecord(ctx, xrpcc, &comatproto.RepoCreateRecord_Input{
		Collection: "app.bsky.feed.like",
		Repo:       xrpcc.Auth.Did,
		Record:     &lexutil.LexiconTypeDecoder{Val: &like},
	})
	return err
}

func CreateRepost(xrpcc *xrpc.Client, viewPost *appbsky.FeedDefs_FeedViewPost) error {
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
		Record:     &lexutil.LexiconTypeDecoder{Val: repost},
	})
	return err
}

func CreateReply(xrpcc *xrpc.Client, viewPost *appbsky.FeedDefs_FeedViewPost) error {
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
			Uri: viewPost.Reply.Root.FeedDefs_PostView.Uri,
			Cid: viewPost.Reply.Root.FeedDefs_PostView.Cid,
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
		Record:     &lexutil.LexiconTypeDecoder{Val: replyPost},
	})
	return err
}

func GenFollowsAndMutes(xrpcc *xrpc.Client, catalog *AccountCatalog, acc *AccountContext, maxFollows int, maxMutes int) error {

	// TODO: have a "shape" to likelihood of doing a follow
	var tgt *AccountContext

	if maxFollows > len(catalog.Regulars) {
		return fmt.Errorf("not enough regulars to pick maxFollowers from")
	}
	if maxMutes > len(catalog.Regulars) {
		return fmt.Errorf("not enough regulars to pick maxMutes from")
	}

	regCount := 0
	celebCount := 0
	if maxFollows >= 1 {
		regCount = rand.Intn(maxFollows)
		celebCount = rand.Intn(len(catalog.Celebs))
	}
	t1 := MeasureIterations("generate follows")
	for idx := range rand.Perm(len(catalog.Celebs))[:celebCount] {
		tgt = &catalog.Celebs[idx]
		if tgt.Auth.Did == acc.Auth.Did {
			continue
		}
		if err := CreateFollow(xrpcc, tgt); err != nil {
			return err
		}
	}
	for idx := range rand.Perm(len(catalog.Regulars))[:regCount] {
		tgt = &catalog.Regulars[idx]
		if tgt.Auth.Did == acc.Auth.Did {
			continue
		}
		if err := CreateFollow(xrpcc, tgt); err != nil {
			return err
		}
	}
	t1(regCount + celebCount)

	// only muting other users, not celebs
	muteCount := 0
	if maxFollows >= 1 && maxMutes > 0 {
		muteCount = rand.Intn(maxMutes)
	}
	t2 := MeasureIterations("generate mutes")
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

func GenLikesRepostsReplies(xrpcc *xrpc.Client, acc *AccountContext, fracLike, fracRepost, fracReply float64) error {
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
			if err := CreateLike(xrpcc, post); err != nil {
				return err
			}
		}
		if fracRepost > 0.0 && rand.Float64() < fracRepost {
			if err := CreateRepost(xrpcc, post); err != nil {
				return err
			}
		}
		if fracReply > 0.0 && rand.Float64() < fracReply {
			if err := CreateReply(xrpcc, post); err != nil {
				return err
			}
		}
	}
	return nil
}

func BrowseAccount(xrpcc *xrpc.Client, acc *AccountContext) error {
	// fetch notifications
	maxNotif := 50
	resp, err := appbsky.NotificationListNotifications(context.TODO(), xrpcc, "", int64(maxNotif), false, nil, "")
	if err != nil {
		return err
	}
	if len(resp.Notifications) > maxNotif {
		return fmt.Errorf("got too many notifications len=%d", len(resp.Notifications))
	}
	t1 := MeasureIterations("notification interactions")
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
			_, err = appbsky.FeedGetAuthorFeed(context.TODO(), xrpcc, notif.Author.Did, "", "", false, 50)
			if err != nil {
				return err
			}
		case "mention":
			fallthrough
		case "reply":
			_, err := appbsky.FeedGetPostThread(context.TODO(), xrpcc, 4, 80, notif.Uri)
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
	t2 := MeasureIterations("timeline interactions")
	for _, post := range timelineResp.Feed {
		// skip account's own posts
		if post.Post.Author.Did == acc.Auth.Did {
			continue
		}
		// TODO: should we do something different here?
		if rand.Float64() < 0.25 {
			_, err = appbsky.FeedGetPostThread(context.TODO(), xrpcc, 4, 80, post.Post.Uri)
			if err != nil {
				return err
			}
		} else if rand.Float64() < 0.25 {
			_, err = appbsky.ActorGetProfile(context.TODO(), xrpcc, post.Post.Author.Did)
			if err != nil {
				return err
			}
			_, err = appbsky.FeedGetAuthorFeed(context.TODO(), xrpcc, post.Post.Author.Did, "", "", false, 50)
			if err != nil {
				return err
			}
		}
	}
	t2(len(timelineResp.Feed))

	// notification count for good measure
	_, err = appbsky.NotificationGetUnreadCount(context.TODO(), xrpcc, false, "")
	return err
}
