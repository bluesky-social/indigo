package indexer

import (
	"context"
	"errors"
	"fmt"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/did"
	"github.com/bluesky-social/indigo/events"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/notifs"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"

	logging "github.com/ipfs/go-log"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

var log = logging.Logger("indexer")

const MaxEventSliceLength = 1000000
const MaxOpsSliceLength = 200

type Indexer struct {
	db *gorm.DB

	notifman notifs.NotificationManager
	events   *events.EventManager
	didr     did.Resolver

	Crawler *CrawlDispatcher

	doSpider bool

	SendRemoteFollow       func(context.Context, string, uint) error
	CreateExternalUser     func(context.Context, string) (*models.ActorInfo, error)
	ApplyPDSClientSettings func(*xrpc.Client)
}

func NewIndexer(db *gorm.DB, notifman notifs.NotificationManager, evtman *events.EventManager, didr did.Resolver, fetcher *RepoFetcher, crawl, spider bool) (*Indexer, error) {
	db.AutoMigrate(&models.ActorInfo{})

	ix := &Indexer{
		db:       db,
		notifman: notifman,
		events:   evtman,
		didr:     didr,
		doSpider: spider,
		SendRemoteFollow: func(context.Context, string, uint) error {
			return nil
		},
		ApplyPDSClientSettings: func(*xrpc.Client) {},
	}

	if crawl {
		c, err := NewCrawlDispatcher(fetcher.FetchAndIndexRepo, fetcher.MaxConcurrency)
		if err != nil {
			return nil, err
		}

		ix.Crawler = c
		ix.Crawler.Run()
	}

	return ix, nil
}

func (ix *Indexer) HandleRepoEvent(ctx context.Context, evt *repomgr.RepoEvent) error {
	ctx, span := otel.Tracer("indexer").Start(ctx, "HandleRepoEvent")
	defer span.End()

	log.Debugw("Handling Repo Event!", "uid", evt.User)

	var outops []*comatproto.SyncSubscribeRepos_RepoOp
	for _, op := range evt.Ops {
		link := (*lexutil.LexLink)(op.RecCid)
		outops = append(outops, &comatproto.SyncSubscribeRepos_RepoOp{
			Path:   op.Collection + "/" + op.Rkey,
			Action: string(op.Kind),
			Cid:    link,
		})

		if err := ix.handleRepoOp(ctx, evt, &op); err != nil {
			log.Errorw("failed to handle repo op", "err", err)
		}
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

	log.Debugw("Sending event", "did", did)
	if err := ix.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoCommit: &comatproto.SyncSubscribeRepos_Commit{
			Repo:   did,
			Prev:   (*lexutil.LexLink)(evt.OldRoot),
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

func (ix *Indexer) handleRepoOp(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp) error {
	switch op.Kind {
	case repomgr.EvtKindCreateRecord:
		if ix.doSpider {
			if err := ix.crawlRecordReferences(ctx, op); err != nil {
				return err
			}
		}
	case repomgr.EvtKindDeleteRecord:
		return nil
	case repomgr.EvtKindUpdateRecord:
		return nil
	default:
		log.Error("indexer encountered an unrecognized event kind")
	}

	return nil
}

func (ix *Indexer) crawlAtUriRef(ctx context.Context, uri string) error {
	puri, err := util.ParseAtUri(uri)
	if err != nil {
		return err
	}

	referencesCrawled.Inc()

	_, err = ix.GetUserOrMissing(ctx, puri.Did)
	if err != nil {
		return err
	}
	return nil
}
func (ix *Indexer) crawlRecordReferences(ctx context.Context, op *repomgr.RepoOp) error {
	ctx, span := otel.Tracer("indexer").Start(ctx, "crawlRecordReferences")
	defer span.End()

	switch rec := op.Record.(type) {
	case *bsky.FeedPost:
		for _, e := range rec.Entities {
			if e.Type == "mention" {
				_, err := ix.GetUserOrMissing(ctx, e.Value)
				if err != nil {
					log.Infow("failed to parse user mention", "ref", e.Value, "err", err)
				}
			}
		}

		if rec.Reply != nil {
			if rec.Reply.Parent != nil {
				if err := ix.crawlAtUriRef(ctx, rec.Reply.Parent.Uri); err != nil {
					log.Infow("failed to crawl reply parent", "cid", op.RecCid, "replyuri", rec.Reply.Parent.Uri, "err", err)
				}
			}

			if rec.Reply.Root != nil {
				if err := ix.crawlAtUriRef(ctx, rec.Reply.Root.Uri); err != nil {
					log.Infow("failed to crawl reply root", "cid", op.RecCid, "rooturi", rec.Reply.Root.Uri, "err", err)
				}
			}
		}

		return nil
	case *bsky.FeedRepost:
		if rec.Subject != nil {
			if err := ix.crawlAtUriRef(ctx, rec.Subject.Uri); err != nil {
				log.Infow("failed to crawl repost subject", "cid", op.RecCid, "subjecturi", rec.Subject.Uri, "err", err)
			}
		}
		return nil
	case *bsky.FeedLike:
		if rec.Subject != nil {
			if err := ix.crawlAtUriRef(ctx, rec.Subject.Uri); err != nil {
				log.Infow("failed to crawl like subject", "cid", op.RecCid, "subjecturi", rec.Subject.Uri, "err", err)
			}
		}
		return nil
	case *bsky.GraphFollow:
		_, err := ix.GetUserOrMissing(ctx, rec.Subject)
		if err != nil {
			log.Infow("failed to crawl follow subject", "cid", op.RecCid, "subjectdid", rec.Subject, "err", err)
		}
		return nil
	case *bsky.GraphBlock:
		_, err := ix.GetUserOrMissing(ctx, rec.Subject)
		if err != nil {
			log.Infow("failed to crawl follow subject", "cid", op.RecCid, "subjectdid", rec.Subject, "err", err)
		}
		return nil
	default:
		return nil
	}
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
	log.Debugw("Sending user to crawler: ", "did", ai.Did)
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
