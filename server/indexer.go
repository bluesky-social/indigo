package schemagen

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"time"

	bsky "github.com/whyrusleeping/gosky/api/bsky"
	"github.com/whyrusleeping/gosky/repomgr"
	"github.com/whyrusleeping/gosky/xrpc"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

type Indexer struct {
	db *gorm.DB

	notifman *NotificationManager
	events   *EventManager
	didr     PLCClient

	sendRemoteFollow func(context.Context, string, uint) error
}

func NewIndexer(db *gorm.DB, notifman *NotificationManager, evtman *EventManager, didr PLCClient) (*Indexer, error) {
	db.AutoMigrate(&FeedPost{})
	db.AutoMigrate(&ActorInfo{})
	db.AutoMigrate(&FollowRecord{})
	db.AutoMigrate(&VoteRecord{})
	db.AutoMigrate(&RepostRecord{})

	return &Indexer{
		db:       db,
		notifman: notifman,
		events:   evtman,
		didr:     didr,
	}, nil
}

type FeedPost struct {
	gorm.Model
	Author      uint
	Rkey        string
	Cid         string
	UpCount     int64
	ReplyCount  int64
	RepostCount int64
	ReplyTo     uint
}

type RepostRecord struct {
	ID         uint `gorm:"primarykey"`
	CreatedAt  time.Time
	RecCreated string
	Post       uint
	Reposter   uint
	Author     uint
	RecCid     string
	Rkey       string
}

type ActorInfo struct {
	gorm.Model
	Uid         uint `gorm:"index"`
	Handle      string
	DisplayName string
	Did         string
	Following   int64
	Followers   int64
	Posts       int64
	DeclRefCid  string
	Type        string
	PDS         uint
}

type VoteDir int

func (vd VoteDir) String() string {
	switch vd {
	case VoteDirUp:
		return "up"
	case VoteDirDown:
		return "down"
	default:
		return "<unknown>"
	}
}

const (
	VoteDirUp   = VoteDir(1)
	VoteDirDown = VoteDir(2)
)

type VoteRecord struct {
	gorm.Model
	Dir     VoteDir
	Voter   uint
	Post    uint
	Created string
	Rkey    string
	Cid     string
}

type FollowRecord struct {
	gorm.Model
	Follower uint
	Target   uint
	Rkey     string
	Cid      string
}

func (ix *Indexer) catchup(ctx context.Context, evt *repomgr.RepoEvent) error {
	// TODO: catch up on events that happened since this event (in the event of a crash or downtime)
	return nil
}

func (ix *Indexer) HandleRepoEvent(ctx context.Context, evt *repomgr.RepoEvent) {
	ctx, span := otel.Tracer("indexer").Start(ctx, "HandleRepoEvent")
	defer span.End()

	if err := ix.catchup(ctx, evt); err != nil {
		log.Println("failed to catch up on user repo changes, processing events off base: ", err)
	}

	fmt.Println("Handling Event!", evt.Kind)

	switch evt.Kind {
	case repomgr.EvtKindCreateRecord:
		if err := ix.handleRecordCreate(ctx, evt, true); err != nil {
			log.Println("handle recordCreate: ", err)
		}
	case repomgr.EvtKindInitActor:
		if err := ix.handleInitActor(ctx, evt); err != nil {
			log.Println("handle initActor: ", err)
		}
	default:
		log.Println("unrecognized repo event type: ", evt.Kind)
	}
}

func (ix *Indexer) handleRecordCreate(ctx context.Context, evt *repomgr.RepoEvent, local bool) error {
	fmt.Println("record create event", evt.Collection)
	var relevantPds []uint
	switch rec := evt.Record.(type) {
	case *bsky.FeedPost:
		var replyid uint
		if rec.Reply != nil {
			replyto, err := ix.GetPost(ctx, rec.Reply.Parent.Uri)
			if err != nil {
				return err
			}

			replyid = replyto.ID
		}

		fp := FeedPost{
			Rkey:    evt.Rkey,
			Cid:     evt.RecCid.String(),
			Author:  evt.User,
			ReplyTo: replyid,
		}
		if err := ix.db.Create(&fp).Error; err != nil {
			return err
		}

		if err := ix.addNewPostNotification(ctx, rec, &fp); err != nil {
			return err
		}

	case *bsky.FeedRepost:
		fp, err := ix.GetPost(ctx, rec.Subject.Uri)
		if err != nil {
			return err
		}

		author, err := ix.lookupUser(ctx, fp.Author)
		if err != nil {
			return err
		}

		relevantPds = append(relevantPds, author.PDS)

		rr := RepostRecord{
			RecCreated: rec.CreatedAt,
			Post:       fp.ID,
			Reposter:   evt.User,
			Author:     fp.Author,
			RecCid:     evt.RecCid.String(),
			Rkey:       evt.Rkey,
		}
		if err := ix.db.Create(&rr).Error; err != nil {
			return err
		}

		if err := ix.notifman.AddRepost(ctx, fp.Author, rr.ID, evt.User); err != nil {
			return err
		}

	case *bsky.FeedVote:
		var val int
		var dbdir VoteDir
		switch rec.Direction {
		case "up":
			val = 1
			dbdir = VoteDirUp
		case "down":
			val = -1
			dbdir = VoteDirDown
		default:
			return fmt.Errorf("invalid vote direction: %q", rec.Direction)
		}

		puri, err := parseAtUri(rec.Subject.Uri)
		if err != nil {
			return err
		}

		act, err := ix.lookupUserByDid(ctx, puri.Did)
		if err != nil {
			return err
		}

		relevantPds = append(relevantPds, act.PDS)

		var post FeedPost
		if err := ix.db.First(&post, "rkey = ? AND author = ?", puri.Rkey, act.Uid).Error; err != nil {
			return err
		}

		vr := VoteRecord{
			Dir:     dbdir,
			Voter:   evt.User,
			Post:    post.ID,
			Created: rec.CreatedAt,
			Rkey:    evt.Rkey,
			Cid:     evt.RecCid.String(),
		}
		if err := ix.db.Create(&vr).Error; err != nil {
			return err
		}

		if err := ix.db.Model(FeedPost{}).Where("id = ?", post.ID).Update("up_count", gorm.Expr("up_count + ?", val)).Error; err != nil {
			return err
		}

		if rec.Direction == "up" {
			if err := ix.addNewVoteNotification(ctx, act.ID, &vr); err != nil {
				return err
			}
		}

	case *bsky.GraphFollow:
		subj, err := ix.lookupUserByDid(ctx, rec.Subject.Did)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			nu, err := ix.createExternalUser(ctx, rec.Subject.Did)
			if err != nil {
				return err
			}

			subj = nu
		}

		if subj.PDS != 0 {
			relevantPds = append(relevantPds, subj.PDS)
		}

		// 'follower' followed 'target'
		fr := FollowRecord{
			Follower: evt.User,
			Target:   subj.ID,
			Rkey:     evt.Rkey,
			Cid:      evt.RecCid.String(),
		}
		if err := ix.db.Create(&fr).Error; err != nil {
			return err
		}

		if err := ix.notifman.AddFollow(ctx, fr.Follower, fr.Target, fr.ID); err != nil {
			return err
		}

		if local && subj.PDS != 0 {
			if err := ix.sendRemoteFollow(ctx, subj.Did, subj.PDS); err != nil {
				log.Println("failed to issue remote follow directive: ", err)
			}
		}

	default:
		return fmt.Errorf("unrecognized record type: %T", rec)
	}

	did, err := ix.didForUser(ctx, evt.User)
	if err != nil {
		return err
	}

	fmt.Println("Sending event: ", evt.Collection, relevantPds)
	if err := ix.events.AddEvent(&Event{
		CarSlice:    evt.RepoSlice,
		Kind:        EvtKindCreateRecord,
		uid:         evt.User,
		User:        did,
		Collection:  evt.Collection,
		Rkey:        evt.Rkey,
		relevantPds: relevantPds,
	}); err != nil {
		log.Println("failed to push event: ", err)
	}

	return nil
}
func (ix *Indexer) createExternalUser(ctx context.Context, did string) (*ActorInfo, error) {
	doc, err := ix.didr.GetDocument(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("could not locate DID document for followed user: %s", err)
	}

	if len(doc.Service) == 0 {
		return nil, fmt.Errorf("external followed user %s had no services in did document", did)
	}

	svc := doc.Service[0]
	durl, err := url.Parse(svc.ServiceEndpoint)
	if err != nil {
		return nil, err
	}

	// TODO: the PDS's DID should also be in the service, we could use that to look up?
	var peering Peering
	if err := ix.db.First(&peering, "host = ?", durl.Host).Error; err != nil {
		return nil, err
	}

	var handle string
	if len(doc.AlsoKnownAs) > 0 {
		hurl, err := url.Parse(doc.AlsoKnownAs[0])
		if err != nil {
			return nil, err
		}

		handle = hurl.Host
	}

	c := &xrpc.Client{Host: svc.ServiceEndpoint}
	profile, err := bsky.ActorGetProfile(ctx, c, did)
	if err != nil {
		return nil, err
	}

	if handle != profile.Handle {
		return nil, fmt.Errorf("mismatch in handle between did document and pds profile (%s != %s)", handle, profile.Handle)
	}

	// TODO: request this users info from their server to fill out our data...
	u := User{
		Handle: handle,
		Did:    did,
		PDS:    peering.ID,
	}

	if err := ix.db.Create(&u).Error; err != nil {
		return nil, fmt.Errorf("failed to create other pds user: %w", err)
	}

	// okay cool, its a user on a server we are peered with
	// lets make a local record of that user for the future
	subj := &ActorInfo{
		Uid:         u.ID,
		Handle:      handle,
		DisplayName: *profile.DisplayName,
		Did:         did,
		DeclRefCid:  profile.Declaration.Cid,
		Type:        "",
		PDS:         peering.ID,
	}
	if err := ix.db.Create(subj).Error; err != nil {
		return nil, err
	}

	return subj, nil
}

func (ix *Indexer) didForUser(ctx context.Context, uid uint) (string, error) {
	var ai ActorInfo
	if err := ix.db.First(&ai, "id = ?", uid).Error; err != nil {
		return "", err
	}

	return ai.Did, nil
}

func (ix *Indexer) lookupUser(ctx context.Context, id uint) (*ActorInfo, error) {
	var ai ActorInfo
	if err := ix.db.First(&ai, "id = ?", id).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (ix *Indexer) lookupUserByDid(ctx context.Context, did string) (*ActorInfo, error) {
	var ai ActorInfo
	if err := ix.db.First(&ai, "did = ?", did).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (ix *Indexer) lookupUserByHandle(ctx context.Context, handle string) (*ActorInfo, error) {
	var ai ActorInfo
	if err := ix.db.First(&ai, "handle = ?", handle).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (ix *Indexer) addNewPostNotification(ctx context.Context, post *bsky.FeedPost, fp *FeedPost) error {
	if post.Reply != nil {
		replyto, err := ix.GetPost(ctx, post.Reply.Parent.Uri)
		if err != nil {
			fmt.Println("probably shouldnt error when processing a reply to a not-found post")
			return err
		}

		if err := ix.notifman.AddReplyTo(ctx, fp.Author, fp.ID, replyto); err != nil {
			return err
		}
	}

	for _, e := range post.Entities {
		switch e.Type {
		case "mention":
			mentioned, err := ix.lookupUserByDid(ctx, e.Value)
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

func (ix *Indexer) addNewVoteNotification(ctx context.Context, postauthor uint, vr *VoteRecord) error {
	return ix.notifman.AddUpVote(ctx, vr.Voter, vr.Post, vr.ID, postauthor)
}

func (ix *Indexer) handleInitActor(ctx context.Context, evt *repomgr.RepoEvent) error {
	ai := evt.ActorInfo
	if err := ix.db.Create(&ActorInfo{
		Uid:         evt.User,
		Handle:      ai.Handle,
		Did:         ai.Did,
		DisplayName: ai.DisplayName,
		DeclRefCid:  ai.DeclRefCid,
		Type:        ai.Type,
	}).Error; err != nil {
		return err
	}

	if err := ix.db.Create(&FollowRecord{
		Follower: evt.User,
		Target:   evt.User,
	}).Error; err != nil {
		return err
	}

	return nil
}

func (ix *Indexer) GetPost(ctx context.Context, uri string) (*FeedPost, error) {
	puri, err := parseAtUri(uri)
	if err != nil {
		return nil, err
	}

	var post FeedPost
	if err := ix.db.First(&post, "rkey = ? AND author = (?)", puri.Rkey, ix.db.Model(ActorInfo{}).Where("did = ?", puri.Did).Select("id")).Error; err != nil {
		return nil, err
	}

	return &post, nil
}
