package archiver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/xrpc"

	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

const MaxEventSliceLength = 1000000
const MaxOpsSliceLength = 200

type Indexer struct {
	db *gorm.DB

	didr did.Resolver

	Crawler *CrawlDispatcher

	CreateExternalUser     func(context.Context, string) (*User, error)
	ApplyPDSClientSettings func(*xrpc.Client)

	log *slog.Logger
}

type AddEventFunc func(ctx context.Context, ev *events.XRPCStreamEvent) error

func NewIndexer(db *gorm.DB, didr did.Resolver, fetcher *RepoFetcher, crawl bool) (*Indexer, error) {
	ix := &Indexer{
		db:                     db,
		didr:                   didr,
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

func (ix *Indexer) GetUserOrMissing(ctx context.Context, did string) (*User, error) {
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

func (ix *Indexer) createMissingUserRecord(ctx context.Context, did string) (*User, error) {
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

func (ix *Indexer) addUserToCrawler(ctx context.Context, ai *User) error {
	ix.log.Debug("Sending user to crawler: ", "did", ai.Did)
	if ix.Crawler == nil {
		return nil
	}

	return ix.Crawler.Crawl(ctx, ai)
}

func (ix *Indexer) DidForUser(ctx context.Context, uid models.Uid) (string, error) {
	var ai User
	if err := ix.db.First(&ai, "uid = ?", uid).Error; err != nil {
		return "", err
	}

	return ai.Did, nil
}

func (ix *Indexer) LookupUser(ctx context.Context, id models.Uid) (*User, error) {
	var ai User
	if err := ix.db.First(&ai, "uid = ?", id).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (ix *Indexer) LookupUserByDid(ctx context.Context, did string) (*User, error) {
	var ai User
	if err := ix.db.Find(&ai, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if ai.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &ai, nil
}

func (ix *Indexer) LookupUserByHandle(ctx context.Context, handle string) (*User, error) {
	var ai User
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
