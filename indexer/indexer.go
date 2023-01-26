package indexer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/notifs"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/types"
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

	crawl bool

	SendRemoteFollow   func(context.Context, string, uint) error
	CreateExternalUser func(context.Context, string) (*types.ActorInfo, error)
}

func NewIndexer(db *gorm.DB, notifman *notifs.NotificationManager, evtman *events.EventManager, didr plc.PLCClient) (*Indexer, error) {
	db.AutoMigrate(&types.FeedPost{})
	db.AutoMigrate(&types.ActorInfo{})
	db.AutoMigrate(&types.FollowRecord{})
	db.AutoMigrate(&types.VoteRecord{})
	db.AutoMigrate(&types.RepostRecord{})

	return &Indexer{
		db:       db,
		notifman: notifman,
		events:   evtman,
		didr:     didr,
		SendRemoteFollow: func(context.Context, string, uint) error {
			return nil
		},
	}, nil
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
			rop, err := ix.handleRecordCreate(ctx, evt, &op, true)
			if err != nil {
				return fmt.Errorf("handle recordCreate: %w", err)
			}
			repoOps = append(repoOps, rop)
			relpds = append(relpds, rop.PrivRelevantPds...)
		case repomgr.EvtKindInitActor:
			rop, err := ix.handleInitActor(ctx, evt, &op)
			if err != nil {
				log.Errorf("handle initActor: %s", err)
			}

			repoOps = append(repoOps, rop)
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

func (ix *Indexer) handleRecordCreate(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp, local bool) (*events.RepoOp, error) {
	log.Infow("record create event", "collection", op.Collection)
	out := &events.RepoOp{
		Kind: string(repomgr.EvtKindCreateRecord),
		Col:  op.Collection,
		Rkey: op.Rkey,
	}
	switch rec := op.Record.(type) {
	case *bsky.FeedPost:
		if err := ix.handleRecordCreateFeedPost(ctx, evt, op, rec); err != nil {
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

		out.PrivRelevantPds = append(out.PrivRelevantPds, author.PDS)

		rr := types.RepostRecord{
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
		var dbdir types.VoteDir
		switch rec.Direction {
		case "up":
			val = 1
			dbdir = types.VoteDirUp
		case "down":
			val = -1
			dbdir = types.VoteDirDown
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

		out.PrivRelevantPds = append(out.PrivRelevantPds, act.PDS)

		vr := types.VoteRecord{
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

		if err := ix.db.Model(types.FeedPost{}).Where("id = ?", post.ID).Update("up_count", gorm.Expr("up_count + ?", val)).Error; err != nil {
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
			nu, err := ix.CreateExternalUser(ctx, rec.Subject.Did)
			if err != nil {
				return nil, fmt.Errorf("create external user: %w", err)
			}

			subj = nu
		}

		if subj.PDS != 0 {
			out.PrivRelevantPds = append(out.PrivRelevantPds, subj.PDS)
		}

		// 'follower' followed 'target'
		fr := types.FollowRecord{
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

func (ix *Indexer) GetPostOrMissing(ctx context.Context, uri string) (*types.FeedPost, error) {
	p, err := ix.GetPost(ctx, uri)
	if err != nil {
		if !isNotFound(err) {
			return nil, err
		}

		// reply to a post we don't know about, create a record for it anyway
		return ix.createMissingPostRecord(ctx, uri)
	}

	return p, nil
}

func (ix *Indexer) handleRecordCreateFeedPost(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp, rec *bsky.FeedPost) error {
	var replyid uint
	if rec.Reply != nil {
		replyto, err := ix.GetPostOrMissing(ctx, rec.Reply.Parent.Uri)
		if err != nil {
			return err
		}

		replyid = replyto.ID

		// TODO: handle root references
	}

	var maybe types.FeedPost
	if err := ix.db.Find(&maybe, "rkey = ? AND author = ?", op.Rkey, evt.User).Error; err != nil {
		return err
	}

	fp := types.FeedPost{
		Rkey:    op.Rkey,
		Cid:     op.RecCid.String(),
		Author:  evt.User,
		ReplyTo: replyid,
	}

	if maybe.ID != 0 {
		// we're likely filling in a missing reference
		if !maybe.Missing {
			// TODO: we've already processed this record creation
			log.Warnw("potentially erroneous event, duplicate create", "rkey", op.Rkey, "user", evt.User)
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

	if err := ix.addNewPostNotification(ctx, rec, &fp); err != nil {
		return err
	}

	return nil
}

func (ix *Indexer) createMissingPostRecord(ctx context.Context, uri string) (*types.FeedPost, error) {
	puri, err := parseAtUri(uri)
	if err != nil {
		return nil, err
	}

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

	var fp types.FeedPost
	if err := ix.db.FirstOrCreate(&fp, types.FeedPost{
		Author:  ai.Uid,
		Rkey:    puri.Rkey,
		Missing: true,
	}).Error; err != nil {
		return nil, err
	}

	return &fp, nil
}

func (ix *Indexer) createMissingUserRecord(ctx context.Context, did string) (*types.ActorInfo, error) {
	ai, err := ix.CreateExternalUser(ctx, did)
	if err != nil {
		return nil, err
	}

	if err := ix.addUserToCrawler(ctx, ai); err != nil {
		return nil, fmt.Errorf("failed to add unknown user to crawler: %w", err)
	}

	return ai, nil
}

func (ix *Indexer) addUserToCrawler(ctx context.Context, ai *types.ActorInfo) error {
	if !ix.crawl {
		return nil
	}

	panic("crawler not implemented")
}

func (ix *Indexer) DidForUser(ctx context.Context, uid uint) (string, error) {
	var ai types.ActorInfo
	if err := ix.db.First(&ai, "uid = ?", uid).Error; err != nil {
		return "", err
	}

	return ai.Did, nil
}

func (ix *Indexer) lookupUser(ctx context.Context, id uint) (*types.ActorInfo, error) {
	var ai types.ActorInfo
	if err := ix.db.First(&ai, "id = ?", id).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (ix *Indexer) LookupUserByDid(ctx context.Context, did string) (*types.ActorInfo, error) {
	var ai types.ActorInfo
	if err := ix.db.First(&ai, "did = ?", did).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (ix *Indexer) lookupUserByHandle(ctx context.Context, handle string) (*types.ActorInfo, error) {
	var ai types.ActorInfo
	if err := ix.db.First(&ai, "handle = ?", handle).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (ix *Indexer) addNewPostNotification(ctx context.Context, post *bsky.FeedPost, fp *types.FeedPost) error {
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

	for _, e := range post.Entities {
		switch e.Type {
		case "mention":
			mentioned, err := ix.LookupUserByDid(ctx, e.Value)
			if err != nil {
				return fmt.Errorf("mentioned user does not exist: %w", err)
			}

			if err := ix.notifman.AddMention(ctx, fp.Author, fp.ID, mentioned.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (ix *Indexer) addNewVoteNotification(ctx context.Context, postauthor uint, vr *types.VoteRecord) error {
	return ix.notifman.AddUpVote(ctx, vr.Voter, vr.Post, vr.ID, postauthor)
}

func (ix *Indexer) handleInitActor(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp) (*events.RepoOp, error) {
	ai := op.ActorInfo

	if err := ix.db.Create(&types.ActorInfo{
		Uid:         evt.User,
		Handle:      ai.Handle,
		Did:         ai.Did,
		DisplayName: ai.DisplayName,
		DeclRefCid:  ai.DeclRefCid,
		Type:        ai.Type,
	}).Error; err != nil {
		return nil, err
	}

	if err := ix.db.Create(&types.FollowRecord{
		Follower: evt.User,
		Target:   evt.User,
	}).Error; err != nil {
		return nil, err
	}

	return &events.RepoOp{
		Kind: string(repomgr.EvtKindInitActor),
	}, nil
}

func isNotFound(err error) bool {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}

	return false
}

func (ix *Indexer) GetPost(ctx context.Context, uri string) (*types.FeedPost, error) {
	puri, err := parseAtUri(uri)
	if err != nil {
		return nil, err
	}

	var post types.FeedPost
	if err := ix.db.First(&post, "rkey = ? AND author = (?)", puri.Rkey, ix.db.Model(types.ActorInfo{}).Where("did = ?", puri.Did).Select("id")).Error; err != nil {
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
