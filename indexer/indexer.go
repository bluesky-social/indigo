package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	comatproto "github.com/gander-social/gander-indigo-sovereign/api/atproto"
	"github.com/gander-social/gander-indigo-sovereign/did"
	"github.com/gander-social/gander-indigo-sovereign/events"
	lexutil "github.com/gander-social/gander-indigo-sovereign/lex/util"
	"github.com/gander-social/gander-indigo-sovereign/models"
	"github.com/gander-social/gander-indigo-sovereign/repomgr"
	"github.com/gander-social/gander-indigo-sovereign/util"
	"github.com/gander-social/gander-indigo-sovereign/xrpc"

	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

const MaxEventSliceLength = 1000000
const MaxOpsSliceLength = 200

type Indexer struct {
	db *gorm.DB

	events *events.EventManager
	didr   did.Resolver

	Crawler *CrawlDispatcher

	SendRemoteFollow       func(context.Context, string, uint) error
	CreateExternalUser     func(context.Context, string) (*models.ActorInfo, error)
	ApplyPDSClientSettings func(*xrpc.Client)

	log *slog.Logger
}

func NewIndexer(db *gorm.DB, evtman *events.EventManager, didr did.Resolver, fetcher *RepoFetcher, crawl bool) (*Indexer, error) {
	db.AutoMigrate(&models.FeedPost{})
	db.AutoMigrate(&models.ActorInfo{})
	db.AutoMigrate(&models.FollowRecord{})
	db.AutoMigrate(&models.VoteRecord{})
	db.AutoMigrate(&models.RepostRecord{})

	ix := &Indexer{
		db:     db,
		events: evtman,
		didr:   didr,
		SendRemoteFollow: func(context.Context, string, uint) error {
			return nil
		},
		ApplyPDSClientSettings: func(*xrpc.Client) {},
		log:                    slog.Default().With("system", "indexer"),
	}

	if crawl {
		c, err := NewCrawlDispatcher(fetcher, fetcher.MaxConcurrency, ix.log)
		if err != nil {
			return nil, err
		}

		ix.Crawler = c
		ix.Crawler.Run()
	}

	return ix, nil
}

func (ix *Indexer) Shutdown() {
	if ix.Crawler != nil {
		ix.Crawler.Shutdown()
	}
}

func (ix *Indexer) HandleRepoEvent(ctx context.Context, evt *repomgr.RepoEvent) error {
	ctx, span := otel.Tracer("indexer").Start(ctx, "HandleRepoEvent")
	defer span.End()

	ix.log.Debug("Handling Repo Event!", "uid", evt.User)

	outops := make([]*comatproto.SyncSubscribeRepos_RepoOp, 0, len(evt.Ops))
	for _, op := range evt.Ops {
		link := (*lexutil.LexLink)(op.RecCid)
		outops = append(outops, &comatproto.SyncSubscribeRepos_RepoOp{
			Path:   op.Collection + "/" + op.Rkey,
			Action: string(op.Kind),
			Cid:    link,
		})
	}

	did, err := ix.DidForUser(ctx, evt.User)
	if err != nil {
		return err
	}

	toobig := false
	slice := evt.RepoSlice
	if len(slice) > MaxEventSliceLength || len(outops) > MaxOpsSliceLength {
		slice = []byte{}
		outops = nil
		toobig = true
	}

	ix.log.Debug("Sending event", "did", did)
	if err := ix.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoCommit: &comatproto.SyncSubscribeRepos_Commit{
			Repo:   did,
			Blocks: slice,
			Rev:    evt.Rev,
			Since:  evt.Since,
			Commit: lexutil.LexLink(evt.NewRoot),
			Time:   time.Now().Format(util.ISO8601),
			Ops:    outops,
			TooBig: toobig,
		},
		PrivUid: evt.User,
	}); err != nil {
		return fmt.Errorf("failed to push event: %s", err)
	}

	return nil
}

func (ix *Indexer) GetUserOrMissing(ctx context.Context, did string) (*models.ActorInfo, error) {
	ctx, span := otel.Tracer("indexer").Start(ctx, "getUserOrMissing")
	defer span.End()

	ai, err := ix.LookupUserByDid(ctx, did)
	if err == nil {
		return ai, nil
	}

	if !isNotFound(err) {
		return nil, err
	}

	// unknown user... create it and send it off to the crawler
	return ix.createMissingUserRecord(ctx, did)
}

func (ix *Indexer) createMissingUserRecord(ctx context.Context, did string) (*models.ActorInfo, error) {
	ctx, span := otel.Tracer("indexer").Start(ctx, "createMissingUserRecord")
	defer span.End()

	externalUserCreationAttempts.Inc()

	ai, err := ix.CreateExternalUser(ctx, did)
	if err != nil {
		return nil, err
	}

	if err := ix.addUserToCrawler(ctx, ai); err != nil {
		return nil, fmt.Errorf("failed to add unknown user to crawler: %w", err)
	}

	return ai, nil
}

func (ix *Indexer) addUserToCrawler(ctx context.Context, ai *models.ActorInfo) error {
	ix.log.Debug("Sending user to crawler: ", "did", ai.Did)
	if ix.Crawler == nil {
		return nil
	}

	return ix.Crawler.Crawl(ctx, ai)
}

func (ix *Indexer) DidForUser(ctx context.Context, uid models.Uid) (string, error) {
	var ai models.ActorInfo
	if err := ix.db.First(&ai, "uid = ?", uid).Error; err != nil {
		return "", err
	}

	return ai.Did, nil
}

func (ix *Indexer) LookupUser(ctx context.Context, id models.Uid) (*models.ActorInfo, error) {
	var ai models.ActorInfo
	if err := ix.db.First(&ai, "uid = ?", id).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (ix *Indexer) LookupUserByDid(ctx context.Context, did string) (*models.ActorInfo, error) {
	var ai models.ActorInfo
	if err := ix.db.Find(&ai, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if ai.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &ai, nil
}

func (ix *Indexer) LookupUserByHandle(ctx context.Context, handle string) (*models.ActorInfo, error) {
	var ai models.ActorInfo
	if err := ix.db.Find(&ai, "handle = ?", handle).Error; err != nil {
		return nil, err
	}

	if ai.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &ai, nil
}

func isNotFound(err error) bool {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}

	return false
}

func (ix *Indexer) GetPost(ctx context.Context, uri string) (*models.FeedPost, error) {
	puri, err := util.ParseAtUri(uri)
	if err != nil {
		return nil, err
	}

	var post models.FeedPost
	if err := ix.db.First(&post, "rkey = ? AND author = (?)", puri.Rkey, ix.db.Model(models.ActorInfo{}).Where("did = ?", puri.Did).Select("id")).Error; err != nil {
		return nil, err
	}

	return &post, nil
}
