package indexer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/notifs"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var log = logging.Logger("indexer")

type Indexer struct {
	db *gorm.DB

	notifman *notifs.NotificationManager
	events   *events.EventManager
	didr     plc.PLCClient

	// TODO: i feel like the repomgr doesnt belong here
	repomgr *repomgr.RepoManager

	Crawler *CrawlDispatcher

	SendRemoteFollow   func(context.Context, string, uint) error
	CreateExternalUser func(context.Context, string) (*models.ActorInfo, error)
}

func NewIndexer(db *gorm.DB, notifman *notifs.NotificationManager, evtman *events.EventManager, didr plc.PLCClient, repoman *repomgr.RepoManager, crawl bool) (*Indexer, error) {
	db.AutoMigrate(&models.FeedPost{})
	db.AutoMigrate(&models.ActorInfo{})
	db.AutoMigrate(&models.FollowRecord{})
	db.AutoMigrate(&models.VoteRecord{})
	db.AutoMigrate(&models.RepostRecord{})

	ix := &Indexer{
		db:       db,
		notifman: notifman,
		events:   evtman,
		repomgr:  repoman,
		didr:     didr,
		SendRemoteFollow: func(context.Context, string, uint) error {
			return nil
		},
	}

	if crawl {
		ix.Crawler = NewCrawlDispatcher(ix.FetchAndIndexRepo)

		ix.Crawler.Run()
	}

	return ix, nil
}

func (ix *Indexer) catchup(ctx context.Context, evt *repomgr.RepoEvent) error {
	// TODO: catch up on events that happened since this event (in the event of a crash or downtime)
	return nil
}

func (ix *Indexer) HandleRepoEvent(ctx context.Context, evt *repomgr.RepoEvent) error {
	ctx, span := otel.Tracer("indexer").Start(ctx, "HandleRepoEvent")
	defer span.End()

	if err := ix.catchup(ctx, evt); err != nil {
		return fmt.Errorf("failed to catch up on user repo changes, processing events off base: %w", err)
	}

	log.Infow("Handling Repo Event!", "uid", evt.User)
	var relpds []uint
	var repoOps []*events.RepoOp
	for _, op := range evt.Ops {
		switch op.Kind {
		case repomgr.EvtKindCreateRecord:
			log.Infof("create record: %d %s %s", evt.User, op.Collection, op.Rkey)
			rel, err := ix.handleRecordCreate(ctx, evt, &op, true)
			if err != nil {
				return fmt.Errorf("handle recordCreate: %w", err)
			}
			repoOps = append(repoOps, &events.RepoOp{
				Kind: string(repomgr.EvtKindCreateRecord),
				Col:  op.Collection,
				Rkey: op.Rkey,
			})

			relpds = append(relpds, rel...)
		case repomgr.EvtKindInitActor:
			if err := ix.handleInitActor(ctx, evt, &op); err != nil {
				return fmt.Errorf("handle initActor: %w", err)
			}

			repoOps = append(repoOps, &events.RepoOp{
				Kind: string(repomgr.EvtKindInitActor),
			})
		default:
			return fmt.Errorf("unrecognized repo event type: %q", op.Kind)
		}
	}

	did, err := ix.DidForUser(ctx, evt.User)
	if err != nil {
		return err
	}

	log.Infow("Sending event: ", "opcnt", len(repoOps), "did", did)
	if err := ix.events.AddEvent(&events.RepoEvent{
		Repo: did,

		RepoAppend: &events.RepoAppend{
			Car: evt.RepoSlice,
			Ops: repoOps,
		},

		PrivRelevantPds: relpds,
		PrivUid:         evt.User,
	}); err != nil {
		return fmt.Errorf("failed to push event: %s", err)
	}

	return nil
}

func (ix *Indexer) handleRecordCreate(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp, local bool) ([]uint, error) {
	log.Infow("record create event", "collection", op.Collection)

	var out []uint
	switch rec := op.Record.(type) {
	case *bsky.FeedPost:
		if err := ix.handleRecordCreateFeedPost(ctx, evt.User, op.Rkey, op.RecCid, rec); err != nil {
			return nil, err
		}
	case *bsky.FeedRepost:
		fp, err := ix.GetPostOrMissing(ctx, rec.Subject.Uri)
		if err != nil {
			return nil, err
		}

		author, err := ix.lookupUser(ctx, fp.Author)
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

	case *bsky.FeedVote:
		var val int
		var dbdir models.VoteDir
		switch rec.Direction {
		case "up":
			val = 1
			dbdir = models.VoteDirUp
		case "down":
			val = -1
			dbdir = models.VoteDirDown
		default:
			return nil, fmt.Errorf("invalid vote direction: %q", rec.Direction)
		}

		post, err := ix.GetPostOrMissing(ctx, rec.Subject.Uri)
		if err != nil {
			return nil, err
		}

		act, err := ix.lookupUser(ctx, post.Author)
		if err != nil {
			return nil, err
		}

		out = append(out, act.PDS)

		vr := models.VoteRecord{
			Dir:     dbdir,
			Voter:   evt.User,
			Post:    post.ID,
			Created: rec.CreatedAt,
			Rkey:    op.Rkey,
			Cid:     op.RecCid.String(),
		}
		if err := ix.db.Create(&vr).Error; err != nil {
			return nil, err
		}

		if err := ix.db.Model(models.FeedPost{}).Where("id = ?", post.ID).Update("up_count", gorm.Expr("up_count + ?", val)).Error; err != nil {
			return nil, err
		}

		if rec.Direction == "up" {
			if err := ix.addNewVoteNotification(ctx, act.ID, &vr); err != nil {
				return nil, err
			}
		}

	case *bsky.GraphFollow:
		subj, err := ix.LookupUserByDid(ctx, rec.Subject.Did)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("failed to lookup user: %w", err)
			}

			nu, err := ix.createMissingUserRecord(ctx, rec.Subject.Did)
			if err != nil {
				return nil, fmt.Errorf("create external user: %w", err)
			}

			subj = nu
		}

		if subj.PDS != 0 {
			out = append(out, subj.PDS)
		}

		// 'follower' followed 'target'
		fr := models.FollowRecord{
			Follower: evt.User,
			Target:   subj.ID,
			Rkey:     op.Rkey,
			Cid:      op.RecCid.String(),
		}
		if err := ix.db.Create(&fr).Error; err != nil {
			return nil, err
		}

		if err := ix.notifman.AddFollow(ctx, fr.Follower, fr.Target, fr.ID); err != nil {
			return nil, err
		}

		if local && subj.PDS != 0 {
			if err := ix.SendRemoteFollow(ctx, subj.Did, subj.PDS); err != nil {
				log.Error("failed to issue remote follow directive: ", err)
			}
		}

	default:
		return nil, fmt.Errorf("unrecognized record type: %T", rec)
	}

	return out, nil
}

func (ix *Indexer) GetPostOrMissing(ctx context.Context, uri string) (*models.FeedPost, error) {
	puri, err := parseAtUri(uri)
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

func (ix *Indexer) handleRecordCreateFeedPost(ctx context.Context, user uint, rkey string, rcid cid.Cid, rec *bsky.FeedPost) error {
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
			ai, err := ix.LookupUserByDid(ctx, e.Value)
			if err != nil {
				if !isNotFound(err) {
					return err
				}

				// unknown user... create it and send it off to the crawler
				nai, err := ix.createMissingUserRecord(ctx, e.Value)
				if err != nil {
					return fmt.Errorf("creating missing user record: %w", err)
				}

				ai = nai
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

func (ix *Indexer) createMissingPostRecord(ctx context.Context, puri *parsedUri) (*models.FeedPost, error) {
	log.Warn("creating missing post record")
	ai, err := ix.LookupUserByDid(ctx, puri.Did)
	if err != nil {
		if !isNotFound(err) {
			return nil, err
		}

		// unknown user... create it and send it off to the crawler
		nai, err := ix.createMissingUserRecord(ctx, puri.Did)
		if err != nil {
			return nil, fmt.Errorf("creating missing user record: %w", err)
		}

		ai = nai
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

func (ix *Indexer) createMissingUserRecord(ctx context.Context, did string) (*models.ActorInfo, error) {
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
	log.Warnw("Sending user to crawler: ", "did", ai.Did)
	if ix.Crawler == nil {
		return nil
	}

	return ix.Crawler.Crawl(ctx, ai)
}

func (ix *Indexer) DidForUser(ctx context.Context, uid uint) (string, error) {
	var ai models.ActorInfo
	if err := ix.db.First(&ai, "uid = ?", uid).Error; err != nil {
		return "", err
	}

	return ai.Did, nil
}

func (ix *Indexer) lookupUser(ctx context.Context, id uint) (*models.ActorInfo, error) {
	var ai models.ActorInfo
	if err := ix.db.First(&ai, "id = ?", id).Error; err != nil {
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

func (ix *Indexer) lookupUserByHandle(ctx context.Context, handle string) (*models.ActorInfo, error) {
	var ai models.ActorInfo
	if err := ix.db.First(&ai, "handle = ?", handle).Error; err != nil {
		return nil, err
	}

	return &ai, nil
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
		if err := ix.notifman.AddMention(ctx, fp.Author, fp.ID, mentioned.ID); err != nil {
			return err
		}
	}

	return nil
}

func (ix *Indexer) addNewVoteNotification(ctx context.Context, postauthor uint, vr *models.VoteRecord) error {
	return ix.notifman.AddUpVote(ctx, vr.Voter, vr.Post, vr.ID, postauthor)
}

func (ix *Indexer) handleInitActor(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp) error {
	ai := op.ActorInfo

	if err := ix.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "uid"}},
		UpdateAll: true,
	}).Create(&models.ActorInfo{
		Uid:         evt.User,
		Handle:      ai.Handle,
		Did:         ai.Did,
		DisplayName: ai.DisplayName,
		DeclRefCid:  ai.DeclRefCid,
		Type:        ai.Type,
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
	puri, err := parseAtUri(uri)
	if err != nil {
		return nil, err
	}

	var post models.FeedPost
	if err := ix.db.First(&post, "rkey = ? AND author = (?)", puri.Rkey, ix.db.Model(models.ActorInfo{}).Where("did = ?", puri.Did).Select("id")).Error; err != nil {
		return nil, err
	}

	return &post, nil
}

type parsedUri struct {
	Did        string
	Collection string
	Rkey       string
}

func parseAtUri(uri string) (*parsedUri, error) {
	if !strings.HasPrefix(uri, "at://") {
		return nil, fmt.Errorf("AT uris must be prefixed with 'at://'")
	}

	trimmed := strings.TrimPrefix(uri, "at://")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 3 {
		return nil, fmt.Errorf("AT uris must have three parts: did, collection, tid")
	}

	return &parsedUri{
		Did:        parts[0],
		Collection: parts[1],
		Rkey:       parts[2],
	}, nil
}

// TODO: since this function is the only place we depend on the repomanager, i wonder if this should be wired some other way?
func (ix *Indexer) FetchAndIndexRepo(ctx context.Context, ai *models.ActorInfo) error {
	ctx, span := otel.Tracer("indexer").Start(ctx, "FetchAndIndexRepo")
	defer span.End()

	var pds models.PDS
	if err := ix.db.First(&pds, "id = ?", ai.PDS).Error; err != nil {
		return fmt.Errorf("expected to find pds record in db for crawling one of their users: %w", err)
	}

	c := &xrpc.Client{
		Host: pds.Host,
	}

	// TODO: max size on these? A malicious PDS could just send us a petabyte sized repo here and kill us
	repo, err := atproto.SyncGetRepo(ctx, c, ai.Did, "")
	if err != nil {
		return fmt.Errorf("failed to fetch repo: %w", err)
	}

	// this process will send individual indexing events back to the indexer, doing a 'fast forward' of the users entire history
	// we probably want alternative ways of doing this for 'very large' or 'very old' repos, but this works for now
	if err := ix.repomgr.ImportNewRepo(ctx, ai.Uid, bytes.NewReader(repo)); err != nil {
		return err
	}

	return nil
}
