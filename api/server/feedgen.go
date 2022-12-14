package schemagen

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	bsky "github.com/whyrusleeping/gosky/api/bsky"
	"github.com/whyrusleeping/gosky/repomgr"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

type FeedGenerator struct {
	db *gorm.DB

	readRecord ReadRecordFunc
}

type ReadRecordFunc func(context.Context, uint, cid.Cid) (any, error)

type FeedPost struct {
	gorm.Model
	Author      uint
	RepostedBy  uint
	TrendedBy   uint
	Tid         string
	Cid         string
	UpCount     int64
	ReplyCount  int64
	RepostCount int64
}

type ActorInfo struct {
	gorm.Model
	User       uint `gorm:"index"`
	Handle     string
	Did        string
	Name       string
	Following  int
	Followers  int
	Posts      int
	DeclRefCid string
	Type       string
}

type UpVoteRecord struct {
	gorm.Model
	User  uint
	Likes uint
}

type FollowRecord struct {
	gorm.Model
	User    uint
	Follows uint
}

func NewFeedGenerator(db *gorm.DB, readRecord ReadRecordFunc) (*FeedGenerator, error) {
	db.AutoMigrate(&FeedPost{})
	db.AutoMigrate(&ActorInfo{})
	db.AutoMigrate(&FollowRecord{})
	db.AutoMigrate(&UpVoteRecord{})

	return &FeedGenerator{
		db:         db,
		readRecord: readRecord,
	}, nil
}

func (fg *FeedGenerator) catchup(ctx context.Context, evt *repomgr.RepoEvent) error {
	// TODO: catch up on events that happened since this event (in the event of a crash or downtime)
	return nil
}

type HydratedFeedItem struct {
	Uri           string
	RepostedBy    *bsky.ActorRef_WithInfo
	Record        any
	ReplyCount    int64
	RepostCount   int64
	UpvoteCount   int64
	DownvoteCount int64
	MyState       *bsky.FeedGetAuthorFeed_MyState
	Cid           string
	Author        *bsky.ActorRef_WithInfo
	TrendedBy     *bsky.ActorRef_WithInfo
	Embed         *bsky.FeedEmbed
	IndexedAt     string
}

func (fg *FeedGenerator) hydrateFeed(ctx context.Context, items []*FeedPost) ([]*HydratedFeedItem, error) {
	out := make([]*HydratedFeedItem, 0, len(items))
	for _, it := range items {
		hit, err := fg.hydrateItem(ctx, it)
		if err != nil {
			return nil, err
		}

		out = append(out, hit)
	}

	return out, nil
}

func (fg *FeedGenerator) didForUser(ctx context.Context, user uint) (string, error) {
	// TODO: cache the shit out of this
	var ai ActorInfo
	if err := fg.db.First(&ai, "user = ?", user).Error; err != nil {
		return "", err
	}

	return ai.Handle, nil
}

func (fg *FeedGenerator) getActorRefInfo(ctx context.Context, user uint) (*bsky.ActorRef_WithInfo, error) {
	// TODO: cache the shit out of this too
	var ai ActorInfo
	if err := fg.db.First(&ai, "user = ?", user).Error; err != nil {
		return nil, err
	}

	out := bsky.ActorRef_WithInfo{
		Did: ai.Did,
		Declaration: &bsky.SystemDeclRef{
			Cid:       ai.DeclRefCid,
			ActorType: ai.Type,
		},
		Handle:      ai.Handle,
		DisplayName: &ai.Name,
	}

	return &out, nil
}

func (fg *FeedGenerator) hydrateItem(ctx context.Context, item *FeedPost) (*HydratedFeedItem, error) {
	authorDid, err := fg.didForUser(ctx, item.Author)
	if err != nil {
		return nil, err
	}

	out := HydratedFeedItem{
		Uri:           "at://" + authorDid + "/" + item.Tid,
		ReplyCount:    item.ReplyCount,
		RepostCount:   item.RepostCount,
		UpvoteCount:   item.UpCount,
		DownvoteCount: 0,
		Cid:           item.Cid,
		IndexedAt:     item.UpdatedAt.Format(time.RFC3339),
	}

	author, err := fg.getActorRefInfo(ctx, item.Author)
	if err != nil {
		return nil, err
	}

	out.Author = author

	if item.TrendedBy != 0 {
		tb, err := fg.getActorRefInfo(ctx, item.TrendedBy)
		if err != nil {
			return nil, err
		}

		out.TrendedBy = tb
	}

	if item.RepostedBy != 0 {
		rp, err := fg.getActorRefInfo(ctx, item.RepostedBy)
		if err != nil {
			return nil, err
		}

		out.RepostedBy = rp
	}

	reccid, err := cid.Decode(item.Cid)
	if err != nil {
		return nil, err
	}

	rec, err := fg.readRecord(ctx, item.Author, reccid)
	if err != nil {
		return nil, err
	}

	out.Record = rec

	return &out, nil
}

func (fg *FeedGenerator) GetTimeline(ctx context.Context, user uint, algo string, before string, limit int) ([]*HydratedFeedItem, error) {
	ctx, span := otel.Tracer("feedgen").Start(context.Background(), "GetTimeline")
	defer span.End()

	// TODO: this query is just a temporary hack...
	var feed []*FeedPost
	if err := fg.db.Find(&feed, "author = (?) OR reposted_by = (?)",
		fg.db.Model(FollowRecord{}).Where("user = ?", user).Select("follows"),
		fg.db.Model(FollowRecord{}).Where("user = ?", user).Select("follows"),
	).Error; err != nil {
		return nil, err
	}

	return fg.hydrateFeed(ctx, feed)
}

func (fg *FeedGenerator) GetAuthorFeed(ctx context.Context, user uint, before string, limit int) ([]*HydratedFeedItem, error) {
	ctx, span := otel.Tracer("feedgen").Start(context.Background(), "GetAuthorFeed")
	defer span.End()

	// for memory efficiency, should probably return the actual type that goes out to the user...
	// bsky.FeedGetAuthorFeed_FeedItem

	var feed []*FeedPost
	if err := fg.db.Find(&feed, "author = ? OR reposted_by = ?", user, user).Error; err != nil {
		return nil, err
	}

	return fg.hydrateFeed(ctx, feed)
}

func (fg *FeedGenerator) HandleRepoEvent(evt *repomgr.RepoEvent) {
	ctx, span := otel.Tracer("feedgen").Start(context.Background(), "HandleRepoEvent")
	defer span.End()

	if err := fg.catchup(ctx, evt); err != nil {
		log.Println("failed to catch up on user repo changes, processing events off base: ", err)
	}

	fmt.Println("Handling Event!", evt.Kind)

	switch evt.Kind {
	case "createRecord":
		if err := fg.handleRecordCreate(ctx, evt); err != nil {
			log.Println("handle recordCreate: ", err)
		}
	case "initActor":
		if err := fg.handleInitActor(ctx, evt); err != nil {
			log.Println("handle initActor: ", err)
		}
	default:
		log.Println("unrecognized repo event type: ", evt.Kind)
	}

}

func (fg *FeedGenerator) handleInitActor(ctx context.Context, evt *repomgr.RepoEvent) error {
	ai := evt.ActorInfo
	if err := fg.db.Create(&ActorInfo{
		User:       evt.User,
		Handle:     ai.Handle,
		Did:        ai.Did,
		Name:       ai.DisplayName,
		DeclRefCid: ai.DeclRefCid,
		Type:       ai.Type,
	}).Error; err != nil {
		return err
	}

	if err := fg.db.Create(&FollowRecord{
		User:    evt.User,
		Follows: evt.User,
	}).Error; err != nil {
		return err
	}

	return nil
}

type parsedUri struct {
	Did  string
	Rkey string
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

	did := parts[0]
	rkey := parts[1] + "/" + parts[2]

	return &parsedUri{
		Did:  did,
		Rkey: rkey,
	}, nil
}

func (fg *FeedGenerator) handleRecordCreate(ctx context.Context, evt *repomgr.RepoEvent) error {
	switch rec := evt.Record.(type) {
	case *bsky.FeedPost:
		fp := FeedPost{
			Tid:    evt.Rkey,
			Cid:    evt.RecCid.String(),
			Author: evt.User,
		}
		if err := fg.db.Create(&fp).Error; err != nil {
			return err
		}

		if err := fg.addNewPostNotification(ctx, evt.User, fp.ID); err != nil {
			return err
		}
		return nil
	case *bsky.FeedVote:
		var val int
		switch rec.Direction {
		case "up":
			val = 1
		case "down":
			val = -1
		default:
			return fmt.Errorf("invalid vote direction: %q", rec.Direction)
		}

		puri, err := parseAtUri(rec.Subject.Uri)
		if err != nil {
			return err
		}

		act, err := fg.lookupUserByDid(ctx, puri.Did)
		if err != nil {
			return err
		}

		var post FeedPost
		if err := fg.db.First(&post, "tid = ? AND author = ?", puri.Rkey, act.User).Error; err != nil {
			return err
		}

		if err := fg.db.Create(&UpVoteRecord{
			User:  evt.User,
			Likes: post.ID,
		}).Error; err != nil {
			return err
		}

		if err := fg.db.Model(FeedPost{}).Where("id = ?", post.ID).Update("up_count", gorm.Expr("up_count + ?", val)).Error; err != nil {
			return err
		}

		return nil
	default:
		return fmt.Errorf("unrecognized record type: %T", rec)
	}
}

func (fg *FeedGenerator) lookupUserByDid(ctx context.Context, did string) (*ActorInfo, error) {
	var ai ActorInfo
	if err := fg.db.First(&ai, "did = ?", did).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (fg *FeedGenerator) addNewPostNotification(ctx context.Context, user uint, postid uint) error {
	// TODO:
	return nil
}

func (fg *FeedGenerator) GetActorProfile(ctx context.Context, actor string) (*ActorInfo, error) {
	var ai ActorInfo
	if err := fg.db.First(&ai, "handle = ?", actor).Error; err != nil {
		return nil, err
	}

	return nil, nil
}
