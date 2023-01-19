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
)

var log = logging.Logger("indexer")

type Indexer struct {
	db *gorm.DB

	notifman *notifs.NotificationManager
	events   *events.EventManager
	didr     plc.PLCClient

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

	log.Infof("Handling Repo Event!")
	var relpds []uint
	var repoOps []*events.RepoOp
	for _, op := range evt.Ops {
		switch op.Kind {
		case repomgr.EvtKindCreateRecord:
			log.Infof("create record: ", evt.User, op.Collection, op.Rkey)
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

	log.Infof("Sending event: ", relpds, len(repoOps))
	if err := ix.events.AddEvent(&events.Event{
		Kind:            events.EvtKindRepoChange,
		CarSlice:        evt.RepoSlice,
		PrivUid:         evt.User,
		RepoOps:         repoOps,
		Repo:            did,
		PrivRelevantPds: relpds,
	}); err != nil {
		return fmt.Errorf("failed to push event: %s", err)
	}

	return nil
}

func (ix *Indexer) handleRecordCreate(ctx context.Context, evt *repomgr.RepoEvent, op *repomgr.RepoOp, local bool) (*events.RepoOp, error) {
	log.Infow("record create event", "collection", op.Collection)
	out := &events.RepoOp{
		Kind:       string(repomgr.EvtKindCreateRecord),
		Collection: op.Collection,
		Rkey:       op.Rkey,
	}
	switch rec := op.Record.(type) {
	case *bsky.FeedPost:
		var replyid uint
		if rec.Reply != nil {
			replyto, err := ix.GetPost(ctx, rec.Reply.Parent.Uri)
			if err != nil {
				return nil, err
			}

			replyid = replyto.ID
		}

		fp := types.FeedPost{
			Rkey:    op.Rkey,
			Cid:     op.RecCid.String(),
			Author:  evt.User,
			ReplyTo: replyid,
		}
		if err := ix.db.Create(&fp).Error; err != nil {
			return nil, err
		}

		if err := ix.addNewPostNotification(ctx, rec, &fp); err != nil {
			return nil, err
		}

	case *bsky.FeedRepost:
		fp, err := ix.GetPost(ctx, rec.Subject.Uri)
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

		puri, err := parseAtUri(rec.Subject.Uri)
		if err != nil {
			return nil, err
		}

		act, err := ix.LookupUserByDid(ctx, puri.Did)
		if err != nil {
			return nil, err
		}

		out.PrivRelevantPds = append(out.PrivRelevantPds, act.PDS)

		var post types.FeedPost
		if err := ix.db.First(&post, "rkey = ? AND author = ?", puri.Rkey, act.Uid).Error; err != nil {
			return nil, err
		}

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

func (ix *Indexer) DidForUser(ctx context.Context, uid uint) (string, error) {
	var ai types.ActorInfo
	if err := ix.db.First(&ai, "id = ?", uid).Error; err != nil {
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
			log.Error("probably shouldnt error when processing a reply to a not-found post")
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
