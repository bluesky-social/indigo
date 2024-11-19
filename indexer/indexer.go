package indexer

import (
	"context"
	"database/sql"
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

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

	doAggregations bool
	doSpider       bool

	SendRemoteFollow       func(context.Context, string, uint) error
	CreateExternalUser     func(context.Context, string) (*models.ActorInfo, error)
	ApplyPDSClientSettings func(*xrpc.Client)
}

func NewIndexer(db *gorm.DB, notifman notifs.NotificationManager, evtman *events.EventManager, didr did.Resolver, fetcher *RepoFetcher, crawl, aggregate, spider bool) (*Indexer, error) {
	db.AutoMigrate(&models.FeedPost{})
	db.AutoMigrate(&models.ActorInfo{})
	db.AutoMigrate(&models.FollowRecord{})
	db.AutoMigrate(&models.VoteRecord{})
	db.AutoMigrate(&models.RepostRecord{})

	ix := &Indexer{
		db:             db,
		notifman:       notifman,
		events:         evtman,
		didr:           didr,
		doAggregations: aggregate,
		doSpider:       spider,
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

	outops := make([]*comatproto.SyncSubscribeRepos_RepoOp, 0, len(evt.Ops))
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
		if ix.doAggregations {
			_, err := ix.handleRecordCreate(ctx, evt, op, true)
			if err != nil {
				return fmt.Errorf("handle recordCreate: %w", err)
			}
		}
		if ix.doSpider {
			if err := ix.crawlRecordReferences(ctx, op); err != nil {
				return err
			}
		}
	case repomgr.EvtKindDeleteRecord:
		if ix.doAggregations {
			if err := ix.handleRecordDelete(ctx, evt, op, true); err != nil {
				return fmt.Errorf("handle recordDelete: %w", err)
			}
		}
	case repomgr.EvtKindUpdateRecord:
		if ix.doAggregations {
			if err := ix.handleRecordUpdate(ctx, evt, op, true); err != nil {
				return fmt.Errorf("handle recordCreate: %w", err)
			}
		}
	default:
		return fmt.Errorf("unrecognized repo event type: %q", op.Kind)
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
	case *bsky.ActorProfile:
		return nil
	case *bsky.GraphList:
		return nil
	case *bsky.GraphListitem:
		return nil
	case *bsky.FeedGenerator:
		return nil
	default:
		log.Warnw("unrecognized record type (crawling references)", "record", op.Record, "collection", op.Collection)
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

func (ix *Indexer) handleInitActor(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp) error {
	ai := op.ActorInfo

	if err := ix.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uid"}},
		UpdateAll: true,
	}).Create(&models.ActorInfo{
		Uid:         evt.User,
		Handle:      sql.NullString{String: ai.Handle, Valid: true},
		Did:         ai.Did,
		DisplayName: ai.DisplayName,
		Type:        ai.Type,
		PDS:         evt.PDS,
	}).Error; err != nil {
		return fmt.Errorf("initializing new actor info: %w", err)
	}

	if err := ix.db.Create(&models.FollowRecord{
		Follower: evt.User,
		Target:   evt.User,
	}).Error; err != nil {
		return err
	}

	return nil
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

func (ix *Indexer) handleRecordDelete(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp, local bool) error {
	log.Debugw("record delete event", "collection", op.Collection)

	switch op.Collection {
	case "app.bsky.feed.post":
		u, err := ix.LookupUser(ctx, evt.User)
		if err != nil {
			return err
		}

		uri := "at://" + u.Did + "/app.bsky.feed.post/" + op.Rkey

		// NB: currently not using the 'or missing' variant here. If we delete
		// something that we've never seen before, maybe just dont bother?
		fp, err := ix.GetPost(ctx, uri)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				log.Warnw("deleting post weve never seen before. Weird.", "user", evt.User, "rkey", op.Rkey)
				return nil
			}
			return err
		}

		if err := ix.db.Model(models.FeedPost{}).Where("id = ?", fp.ID).UpdateColumn("deleted", true).Error; err != nil {
			return err
		}
	case "app.bsky.feed.repost":
		if err := ix.db.Where("reposter = ? AND rkey = ?", evt.User, op.Rkey).Delete(&models.RepostRecord{}).Error; err != nil {
			return err
		}

		log.Warn("TODO: remove notifications on delete")
		/*
		   if err := ix.notifman.RemoveRepost(ctx, fp.Author, rr.ID, evt.User); err != nil {
		           return nil, err
		   }
		*/

	case "app.bsky.feed.vote":
		return ix.handleRecordDeleteFeedLike(ctx, evt, op)
	case "app.bsky.graph.follow":
		return ix.handleRecordDeleteGraphFollow(ctx, evt, op)
	case "app.bsky.graph.confirmation":
		return nil
	default:
		return fmt.Errorf("unrecognized record type (delete): %q", op.Collection)
	}

	return nil
}

func (ix *Indexer) handleRecordDeleteFeedLike(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp) error {
	var vr models.VoteRecord
	if err := ix.db.Find(&vr, "voter = ? AND rkey = ?", evt.User, op.Rkey).Error; err != nil {
		return err
	}

	if err := ix.db.Transaction(func(tx *gorm.DB) error {
		tx.Statement.RaiseErrorOnNotFound = true
		if err := tx.Model(models.VoteRecord{}).Where("id = ?", vr.ID).Delete(&vr).Error; err != nil {
			return err
		}

		if err := tx.Model(models.FeedPost{}).Where("id = ?", vr.Post).Update("up_count", gorm.Expr("up_count - 1")).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	log.Warnf("need to delete vote notification")
	return nil
}

func (ix *Indexer) handleRecordDeleteGraphFollow(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp) error {
	q := ix.db.Where("follower = ? AND rkey = ?", evt.User, op.Rkey).Delete(&models.FollowRecord{})
	if err := q.Error; err != nil {
		return err
	}

	if q.RowsAffected == 0 {
		log.Warnw("attempted to delete follow we did not have a record for", "user", evt.User, "rkey", op.Rkey)
		return nil
	}

	return nil
}

func (ix *Indexer) handleRecordCreate(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp, local bool) ([]uint, error) {
	log.Debugw("record create event", "collection", op.Collection)

	var out []uint
	switch rec := op.Record.(type) {
	case *bsky.FeedPost:
		if err := ix.handleRecordCreateFeedPost(ctx, evt.User, op.Rkey, *op.RecCid, rec); err != nil {
			return nil, err
		}
	case *bsky.FeedRepost:
		fp, err := ix.GetPostOrMissing(ctx, rec.Subject.Uri)
		if err != nil {
			return nil, err
		}

		author, err := ix.LookupUser(ctx, fp.Author)
		if err != nil {
			return nil, err
		}

		out = append(out, author.PDS)

		rr := models.RepostRecord{
			RecCreated: rec.CreatedAt,
			Post:       fp.ID,
			Reposter:   evt.User,
			Author:     fp.Author,
			RecCid:     op.RecCid.String(),
			Rkey:       op.Rkey,
		}
		if err := ix.db.Create(&rr).Error; err != nil {
			return nil, err
		}

		if err := ix.notifman.AddRepost(ctx, fp.Author, rr.ID, evt.User); err != nil {
			return nil, err
		}

	case *bsky.FeedLike:
		return nil, ix.handleRecordCreateFeedLike(ctx, rec, evt, op)
	case *bsky.GraphFollow:
		return out, ix.handleRecordCreateGraphFollow(ctx, rec, evt, op)
	case *bsky.GraphBlock:
		return out, nil
	case *bsky.GraphList:
		return out, nil
	case *bsky.GraphListitem:
		return out, nil
	case *bsky.FeedGenerator:
		return out, nil
	case *bsky.ActorProfile:
		log.Debugf("TODO: got actor profile record creation, need to do something with this")
	default:
		log.Warnw("unrecognized record", "record", op.Record, "collection", op.Collection)
		return nil, fmt.Errorf("unrecognized record type (creation): %s", op.Collection)
	}

	return out, nil
}

func (ix *Indexer) handleRecordCreateFeedLike(ctx context.Context, rec *bsky.FeedLike, evt *repomgr.RepoEvent, op *repomgr.RepoOp) error {
	post, err := ix.GetPostOrMissing(ctx, rec.Subject.Uri)
	if err != nil {
		return err
	}

	act, err := ix.LookupUser(ctx, post.Author)
	if err != nil {
		return err
	}

	vr := models.VoteRecord{
		Voter:   evt.User,
		Post:    post.ID,
		Created: rec.CreatedAt,
		Rkey:    op.Rkey,
		Cid:     op.RecCid.String(),
	}
	if err := ix.db.Create(&vr).Error; err != nil {
		return err
	}

	if err := ix.db.Model(models.FeedPost{}).Where("id = ?", post.ID).Update("up_count", gorm.Expr("up_count + 1")).Error; err != nil {
		return err
	}
	if err := ix.addNewVoteNotification(ctx, act.Uid, &vr); err != nil {
		return err
	}

	return nil
}

func (ix *Indexer) handleRecordCreateGraphFollow(ctx context.Context, rec *bsky.GraphFollow, evt *repomgr.RepoEvent, op *repomgr.RepoOp) error {
	subj, err := ix.LookupUserByDid(ctx, rec.Subject)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to lookup user: %w", err)
		}

		nu, err := ix.createMissingUserRecord(ctx, rec.Subject)
		if err != nil {
			return fmt.Errorf("create external user: %w", err)
		}

		subj = nu
	}

	// 'follower' followed 'target'
	fr := models.FollowRecord{
		Follower: evt.User,
		Target:   subj.Uid,
		Rkey:     op.Rkey,
		Cid:      op.RecCid.String(),
	}
	if err := ix.db.Create(&fr).Error; err != nil {
		return err
	}

	if err := ix.notifman.AddFollow(ctx, fr.Follower, fr.Target, fr.ID); err != nil {
		return err
	}

	return nil
}

func (ix *Indexer) handleRecordUpdate(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp, local bool) error {
	log.Debugw("record update event", "collection", op.Collection)

	switch rec := op.Record.(type) {
	case *bsky.FeedPost:
		u, err := ix.LookupUser(ctx, evt.User)
		if err != nil {
			return err
		}

		uri := "at://" + u.Did + "/app.bsky.feed.post/" + op.Rkey
		fp, err := ix.GetPostOrMissing(ctx, uri)
		if err != nil {
			return err
		}

		oldReply := fp.ReplyTo != 0
		newReply := rec.Reply != nil

		if oldReply != newReply {
			// the 'replyness' of the post was changed... that's weird
			log.Errorf("need to properly handle case where reply-ness of posts is changed")
			return nil
		}

		if newReply {
			replyto, err := ix.GetPostOrMissing(ctx, rec.Reply.Parent.Uri)
			if err != nil {
				return err
			}

			if replyto.ID != fp.ReplyTo {
				log.Errorf("post was changed to be a reply to a different post")
				return nil
			}
		}

		if err := ix.db.Model(models.FeedPost{}).Where("id = ?", fp.ID).UpdateColumn("cid", op.RecCid.String()).Error; err != nil {
			return err
		}

		return nil
	case *bsky.FeedRepost:
		var rr models.RepostRecord
		if err := ix.db.First(&rr, "reposter = ? AND rkey = ?", evt.User, op.Rkey).Error; err != nil {
			return err
		}

		// TODO: check if the post changed and do something about that

		rr.RecCreated = rec.CreatedAt
		rr.RecCid = op.RecCid.String()

		if err := ix.db.Save(&rr).Error; err != nil {
			return err
		}

	case *bsky.FeedLike:
		var vr models.VoteRecord
		if err := ix.db.Find(&vr, "voted = ? AND rkey = ?", evt.User, op.Rkey).Error; err != nil {
			return err
		}

		fp, err := ix.GetPostOrMissing(ctx, rec.Subject.Uri)
		if err != nil {
			return err
		}

		if vr.Post != fp.ID {
			// vote is on a completely different post, delete old one, create new one
			if err := ix.handleRecordDeleteFeedLike(ctx, evt, op); err != nil {
				return err
			}

			return ix.handleRecordCreateFeedLike(ctx, rec, evt, op)
		}

		return ix.handleRecordCreateFeedLike(ctx, rec, evt, op)
	case *bsky.GraphFollow:
		if err := ix.handleRecordDeleteGraphFollow(ctx, evt, op); err != nil {
			return err
		}

		return ix.handleRecordCreateGraphFollow(ctx, rec, evt, op)
	case *bsky.ActorProfile:
		log.Debugf("TODO: got actor profile record update, need to do something with this")
	default:
		return fmt.Errorf("unrecognized record type (update): %s", op.Collection)
	}

	return nil
}

func (ix *Indexer) GetPostOrMissing(ctx context.Context, uri string) (*models.FeedPost, error) {
	puri, err := util.ParseAtUri(uri)
	if err != nil {
		return nil, err
	}

	var post models.FeedPost
	if err := ix.db.Find(&post, "rkey = ? AND author = (?)", puri.Rkey, ix.db.Model(models.ActorInfo{}).Where("did = ?", puri.Did).Select("id")).Error; err != nil {
		return nil, err
	}

	if post.ID == 0 {
		// reply to a post we don't know about, create a record for it anyway
		return ix.createMissingPostRecord(ctx, puri)
	}

	return &post, nil
}

func (ix *Indexer) handleRecordCreateFeedPost(ctx context.Context, user models.Uid, rkey string, rcid cid.Cid, rec *bsky.FeedPost) error {
	var replyid uint
	if rec.Reply != nil {
		replyto, err := ix.GetPostOrMissing(ctx, rec.Reply.Parent.Uri)
		if err != nil {
			return err
		}

		replyid = replyto.ID

		rootref, err := ix.GetPostOrMissing(ctx, rec.Reply.Root.Uri)
		if err != nil {
			return err
		}

		// TODO: use this for indexing?
		_ = rootref
	}

	var mentions []*models.ActorInfo
	for _, e := range rec.Entities {
		if e.Type == "mention" {
			ai, err := ix.GetUserOrMissing(ctx, e.Value)
			if err != nil {
				return err
			}

			mentions = append(mentions, ai)
		}
	}

	var maybe models.FeedPost
	if err := ix.db.Find(&maybe, "rkey = ? AND author = ?", rkey, user).Error; err != nil {
		return err
	}

	fp := models.FeedPost{
		Rkey:    rkey,
		Cid:     rcid.String(),
		Author:  user,
		ReplyTo: replyid,
	}

	if maybe.ID != 0 {
		// we're likely filling in a missing reference
		if !maybe.Missing {
			// TODO: we've already processed this record creation
			log.Warnw("potentially erroneous event, duplicate create", "rkey", rkey, "user", user)
		}

		if err := ix.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{clause.Column{Name: "rkey"}, clause.Column{Name: "author"}},
			UpdateAll: true,
		}).Create(&fp).Error; err != nil {
			return err
		}

	} else {
		if err := ix.db.Create(&fp).Error; err != nil {
			return err
		}
	}

	if err := ix.addNewPostNotification(ctx, rec, &fp, mentions); err != nil {
		return err
	}

	return nil
}

func (ix *Indexer) createMissingPostRecord(ctx context.Context, puri *util.ParsedUri) (*models.FeedPost, error) {
	log.Warn("creating missing post record")
	ai, err := ix.GetUserOrMissing(ctx, puri.Did)
	if err != nil {
		return nil, err
	}

	var fp models.FeedPost
	if err := ix.db.FirstOrCreate(&fp, models.FeedPost{
		Author:  ai.Uid,
		Rkey:    puri.Rkey,
		Missing: true,
	}).Error; err != nil {
		return nil, err
	}

	return &fp, nil
}

func (ix *Indexer) addNewPostNotification(ctx context.Context, post *bsky.FeedPost, fp *models.FeedPost, mentions []*models.ActorInfo) error {
	if post.Reply != nil {
		replyto, err := ix.GetPost(ctx, post.Reply.Parent.Uri)
		if err != nil {
			log.Error("probably shouldn't error when processing a reply to a not-found post")
			return err
		}

		if err := ix.notifman.AddReplyTo(ctx, fp.Author, fp.ID, replyto); err != nil {
			return err
		}
	}

	for _, mentioned := range mentions {
		if err := ix.notifman.AddMention(ctx, fp.Author, fp.ID, mentioned.Uid); err != nil {
			return err
		}
	}

	return nil
}

func (ix *Indexer) addNewVoteNotification(ctx context.Context, postauthor models.Uid, vr *models.VoteRecord) error {
	return ix.notifman.AddUpVote(ctx, vr.Voter, vr.Post, vr.ID, postauthor)
}
