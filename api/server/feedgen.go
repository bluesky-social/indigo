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
	Author     uint
	RepostedBy uint
	TrendedBy  uint
	// TODO: only keeping rkey here, assuming collection is app.bsky.feed.post
	Rkey        string
	Cid         string
	UpCount     int64
	ReplyCount  int64
	RepostCount int64
}

type ActorInfo struct {
	gorm.Model
	Uid         uint `gorm:"index"`
	Handle      string
	DisplayName string
	Did         string
	Name        string
	Following   int64
	Followers   int64
	Posts       int64
	DeclRefCid  string
	Type        string
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
}

type FollowRecord struct {
	gorm.Model
	Subject uint
	Follows uint
}

func NewFeedGenerator(db *gorm.DB, readRecord ReadRecordFunc) (*FeedGenerator, error) {
	db.AutoMigrate(&FeedPost{})
	db.AutoMigrate(&ActorInfo{})
	db.AutoMigrate(&FollowRecord{})
	db.AutoMigrate(&VoteRecord{})

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
	if err := fg.db.First(&ai, "uid = ?", user).Error; err != nil {
		return "", err
	}

	return ai.Did, nil
}

func (fg *FeedGenerator) getActorRefInfo(ctx context.Context, user uint) (*bsky.ActorRef_WithInfo, error) {
	// TODO: cache the shit out of this too
	var ai ActorInfo
	if err := fg.db.First(&ai, "uid = ?", user).Error; err != nil {
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
		Uri:           "at://" + authorDid + "/app.bsky.feed.post/" + item.Rkey,
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
		fg.db.Model(FollowRecord{}).Where("subject = ?", user).Select("follows"),
		fg.db.Model(FollowRecord{}).Where("subject = ?", user).Select("follows"),
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

func (fg *FeedGenerator) HandleRepoEvent(ctx context.Context, evt *repomgr.RepoEvent) {
	ctx, span := otel.Tracer("feedgen").Start(ctx, "HandleRepoEvent")
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
		Uid:        evt.User,
		Handle:     ai.Handle,
		Did:        ai.Did,
		Name:       ai.DisplayName,
		DeclRefCid: ai.DeclRefCid,
		Type:       ai.Type,
	}).Error; err != nil {
		return err
	}

	if err := fg.db.Create(&FollowRecord{
		Subject: evt.User,
		Follows: evt.User,
	}).Error; err != nil {
		return err
	}

	return nil
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

func (fg *FeedGenerator) handleRecordCreate(ctx context.Context, evt *repomgr.RepoEvent) error {

	switch rec := evt.Record.(type) {
	case *bsky.FeedPost:
		fp := FeedPost{
			Rkey:   evt.Rkey,
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

		act, err := fg.lookupUserByDid(ctx, puri.Did)
		if err != nil {
			return err
		}

		var post FeedPost
		if err := fg.db.First(&post, "rkey = ? AND author = ?", puri.Rkey, act.Uid).Error; err != nil {
			return err
		}

		if err := fg.db.Create(&VoteRecord{
			Dir:     dbdir,
			Voter:   evt.User,
			Post:    post.ID,
			Created: rec.CreatedAt,
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
	if strings.HasPrefix(actor, "did:") {
		if err := fg.db.First(&ai, "did = ?", actor).Error; err != nil {
			return nil, err
		}
	} else {
		if err := fg.db.First(&ai, "handle = ?", actor).Error; err != nil {
			return nil, err
		}
	}

	return &ai, nil
}

func (fg *FeedGenerator) GetPost(ctx context.Context, uri string) (*FeedPost, error) {
	puri, err := parseAtUri(uri)
	if err != nil {
		return nil, err
	}

	var post FeedPost
	if err := fg.db.First(&post, "rkey = ? AND author = (?)", puri.Rkey, fg.db.Model(ActorInfo{}).Where("did = ?", puri.Did).Select("id")).Error; err != nil {
		return nil, err
	}

	return &post, nil
}

type ThreadPost struct {
	Post *HydratedFeedItem

	ParentUri string
	Parent    *ThreadPost
}

func (fg *FeedGenerator) GetPostThread(ctx context.Context, uri string, depth int) (*ThreadPost, error) {
	post, err := fg.GetPost(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("getting post for thread: %w", err)
	}

	hi, err := fg.hydrateItem(ctx, post)
	if err != nil {
		return nil, err
	}

	p, ok := hi.Record.(*bsky.FeedPost)
	if !ok {
		return nil, fmt.Errorf("getPostThread can only operate on app.bsky.feed.post records")
	}

	out := &ThreadPost{
		Post: hi,
	}

	if p.Reply != nil {
		out.ParentUri = p.Reply.Parent.Uri
		if depth > 0 {

			parent, err := fg.GetPostThread(ctx, p.Reply.Parent.Uri, depth-1)
			if err != nil {
				// TODO: check for and handle 'not found'
				return nil, err
			}
			out.Parent = parent
		}
	}

	return out, nil
}

type HydratedVote struct {
	Actor     *bsky.ActorRef_WithInfo
	Direction string
	IndexedAt time.Time
	CreatedAt string
}

func (fg *FeedGenerator) hydrateVote(ctx context.Context, v *VoteRecord) (*HydratedVote, error) {
	aref, err := fg.getActorRefInfo(ctx, v.Voter)
	if err != nil {
		return nil, err
	}

	return &HydratedVote{
		Actor:     aref,
		Direction: v.Dir.String(),
		IndexedAt: v.UpdatedAt,
		CreatedAt: v.Created,
	}, nil
}

func (fg *FeedGenerator) GetVotes(ctx context.Context, uri string, pcid cid.Cid, dir string, limit int, before string) ([]*HydratedVote, error) {
	if before != "" {
		log.Println("not respecting 'before' yet")
	}

	p, err := fg.GetPost(ctx, uri)
	if err != nil {
		return nil, err
	}

	if p.Cid != pcid.String() {
		return nil, fmt.Errorf("listing likes of old post versions not supported")
	}

	var dbdir VoteDir
	switch dir {
	case "up":
		dbdir = VoteDirUp
	case "down":
		dbdir = VoteDirDown
	default:
		return nil, fmt.Errorf("there are only two directions, up or down")
	}

	var voterecs []VoteRecord
	if err := fg.db.Limit(limit).Find(&voterecs, "dir = ? AND post = ?", dbdir, p.ID).Error; err != nil {
		return nil, err
	}

	var out []*HydratedVote
	for _, vr := range voterecs {
		hv, err := fg.hydrateVote(ctx, &vr)
		if err != nil {
			return nil, err
		}
		out = append(out, hv)
	}

	return out, nil
}

type FollowInfo struct {
	Follower  *bsky.ActorRef_WithInfo
	Subject   *bsky.ActorRef_WithInfo
	CreatedAt string
	IndexedAt string
}

func (fg *FeedGenerator) GetFollows(ctx context.Context, user string, limit int, before string) ([]*FollowInfo, error) {
	var follows []FollowRecord
	if err := fg.db.Limit(limit).Find(&follows, "subject = (?)", fg.db.Model(ActorInfo{}).Where("did = ? or handle = ?", user, user).Select("subject")).Error; err != nil {
		return nil, err
	}

	profile, err := fg.GetActorProfile(ctx, user)
	if err != nil {
		return nil, err
	}

	ai, err := fg.getActorRefInfo(ctx, profile.Uid)
	if err != nil {
		return nil, err
	}

	out := []*FollowInfo{}
	for _, f := range follows {
		fai, err := fg.getActorRefInfo(ctx, f.Follows)
		if err != nil {
			return nil, err
		}

		out = append(out, &FollowInfo{
			Follower:  ai,
			Subject:   fai,
			CreatedAt: f.CreatedAt.Format(time.RFC3339),
			IndexedAt: f.CreatedAt.Format(time.RFC3339),
		})

	}

	return nil, nil
}
