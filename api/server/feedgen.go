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

	notifman   *NotificationManager
	readRecord ReadRecordFunc
}

func NewFeedGenerator(db *gorm.DB, notifman *NotificationManager, readRecord ReadRecordFunc) (*FeedGenerator, error) {
	db.AutoMigrate(&FeedPost{})
	db.AutoMigrate(&ActorInfo{})
	db.AutoMigrate(&FollowRecord{})
	db.AutoMigrate(&VoteRecord{})
	db.AutoMigrate(&RepostRecord{})

	return &FeedGenerator{
		db:         db,
		notifman:   notifman,
		readRecord: readRecord,
	}, nil
}

type ReadRecordFunc func(context.Context, uint, cid.Cid) (any, error)

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

func (fg *FeedGenerator) catchup(ctx context.Context, evt *repomgr.RepoEvent) error {
	// TODO: catch up on events that happened since this event (in the event of a crash or downtime)
	return nil
}

/*
type HydratedFeedItem struct {
	Uri           string
	RepostedBy    *bsky.ActorRef_WithInfo
	Record        any
	ReplyCount    int64
	RepostCount   int64
	UpvoteCount   int64
	DownvoteCount int64
	MyState       *bsky.FeedGetAuthorFeed_MyState
	Cid       string
	Author    *bsky.ActorRef_WithInfo
	TrendedBy *bsky.ActorRef_WithInfo
	Embed     *bsky.FeedEmbed
	IndexedAt string
}
*/

func (fg *FeedGenerator) hydrateFeed(ctx context.Context, items []*FeedPost, reposts []*RepostRecord) ([]*bsky.FeedFeedViewPost, error) {
	out := make([]*bsky.FeedFeedViewPost, 0, len(items))
	for _, it := range items {
		hit, err := fg.hydrateItem(ctx, it)
		if err != nil {
			return nil, err
		}

		out = append(out, hit)
	}

	if len(reposts) > 0 {
		for _, rp := range reposts {
			var fp FeedPost
			if err := fg.db.First(&fp, "id = ?", rp.Post).Error; err != nil {
				return nil, err
			}

			fvp, err := fg.hydrateItem(ctx, &fp)
			if err != nil {
				return nil, err
			}

			reposter, err := fg.getActorRefInfo(ctx, rp.Reposter)
			if err != nil {
				return nil, err
			}

			fvp.Reason = &bsky.FeedFeedViewPost_Reason{
				FeedFeedViewPost_ReasonRepost: &bsky.FeedFeedViewPost_ReasonRepost{
					By:        reposter,
					IndexedAt: rp.CreatedAt.Format(time.RFC3339),
				},
			}

			out = append(out, fvp)
		}
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

	return ai.ActorRef(), nil
}

func (ai *ActorInfo) ActorRef() *bsky.ActorRef_WithInfo {
	return &bsky.ActorRef_WithInfo{
		Did: ai.Did,
		Declaration: &bsky.SystemDeclRef{
			Cid:       ai.DeclRefCid,
			ActorType: ai.Type,
		},
		Handle:      ai.Handle,
		DisplayName: &ai.Name,
	}
}

func (fg *FeedGenerator) hydrateItem(ctx context.Context, item *FeedPost) (*bsky.FeedFeedViewPost, error) {
	authorDid, err := fg.didForUser(ctx, item.Author)
	if err != nil {
		return nil, err
	}

	out := &bsky.FeedFeedViewPost{}

	out.Post = &bsky.FeedPost_View{
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

	out.Post.Author = author

	fmt.Println("need to finish reasons")
	/*
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
	*/

	reccid, err := cid.Decode(item.Cid)
	if err != nil {
		return nil, err
	}

	rec, err := fg.readRecord(ctx, item.Author, reccid)
	if err != nil {
		return nil, err
	}

	out.Post.Record = rec

	return out, nil
}

func (fg *FeedGenerator) getPostViewerState(ctx context.Context, item uint, viewer uint, viewerDid string) (*bsky.FeedPost_ViewerState, error) {
	var out bsky.FeedPost_ViewerState

	var vote VoteRecord
	if err := fg.db.Find(&vote, "post = ? AND voter = ?", item, viewer).Error; err != nil {
		return nil, err
	}

	if vote.ID != 0 {
		vuri := fmt.Sprintf("at://%s/app.bsky.feed.vote/%s", viewerDid, vote.Rkey)
		switch vote.Dir {
		case VoteDirUp:
			out.Upvote = &vuri
		case VoteDirDown:
			out.Downvote = &vuri
		}
	}

	var rep RepostRecord
	if err := fg.db.Find(&rep, "post = ? AND reposter = ?", item, viewer).Error; err != nil {
		return nil, err
	}

	if rep.ID != 0 {
		rpuri := fmt.Sprintf("at://%s/app.bsky.feed.repost/%s", viewerDid, rep.Rkey)
		out.Repost = &rpuri
	}

	return &out, nil
}

func (fg *FeedGenerator) GetTimeline(ctx context.Context, user *User, algo string, before string, limit int) ([]*bsky.FeedFeedViewPost, error) {
	ctx, span := otel.Tracer("feedgen").Start(context.Background(), "GetTimeline")
	defer span.End()

	// TODO: this query is just a temporary hack...
	var feed []*FeedPost
	if err := fg.db.Find(&feed, "author = (?)",
		fg.db.Model(FollowRecord{}).Where("follower = ?", user.ID).Select("target"),
	).Error; err != nil {
		return nil, err
	}

	var rps []*RepostRecord
	if err := fg.db.Find(&rps, "reposter = (?)",
		fg.db.Model(FollowRecord{}).Where("follower = ?", user.ID).Select("target"),
	).Error; err != nil {
		return nil, err
	}

	fout, err := fg.hydrateFeed(ctx, feed, rps)
	if err != nil {
		return nil, fmt.Errorf("hydrating feed: %w", err)
	}

	return fg.personalizeFeed(ctx, fout, user)

}

func (fg *FeedGenerator) personalizeFeed(ctx context.Context, feed []*bsky.FeedFeedViewPost, viewer *User) ([]*bsky.FeedFeedViewPost, error) {
	for _, p := range feed {

		// TODO: its inefficient to have to call 'GetPost' again here when we could instead be doing that inside the 'hydrateFeed' call earlier.
		// However, if we introduce per-user information into hydrateFeed, then
		// it cannot be effectively cached, so we separate the 'cacheable'
		// portion of feed generation from the 'per user' portion. An
		// optimization could be to hide the internal post IDs in the Post
		// structs for internal use (stripped out before sending to client)
		item, err := fg.GetPost(ctx, p.Post.Uri)
		if err != nil {
			return nil, err
		}

		vs, err := fg.getPostViewerState(ctx, item.ID, viewer.ID, viewer.DID)
		if err != nil {
			return nil, fmt.Errorf("getting viewer state: %w", err)
		}

		p.Post.Viewer = vs
	}

	return feed, nil
}

func (fg *FeedGenerator) GetAuthorFeed(ctx context.Context, user *User, before string, limit int) ([]*bsky.FeedFeedViewPost, error) {
	ctx, span := otel.Tracer("feedgen").Start(context.Background(), "GetAuthorFeed")
	defer span.End()

	// for memory efficiency, should probably return the actual type that goes out to the user...
	// bsky.FeedGetAuthorFeed_FeedItem

	var feed []*FeedPost
	if err := fg.db.Find(&feed, "author = ?", user.ID).Error; err != nil {
		return nil, err
	}

	var reposts []*RepostRecord
	if err := fg.db.Find(&reposts, "reposter = ?", user.ID).Error; err != nil {
		return nil, err
	}

	fout, err := fg.hydrateFeed(ctx, feed, reposts)
	if err != nil {
		return nil, fmt.Errorf("hydrating feed: %w", err)
	}

	return fg.personalizeFeed(ctx, fout, user)
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
		Follower: evt.User,
		Target:   evt.User,
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
		var replyid uint
		if rec.Reply != nil {
			replyto, err := fg.GetPost(ctx, rec.Reply.Parent.Uri)
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
		if err := fg.db.Create(&fp).Error; err != nil {
			return err
		}

		if err := fg.addNewPostNotification(ctx, rec, &fp); err != nil {
			return err
		}

		return nil
	case *bsky.FeedRepost:
		fp, err := fg.GetPost(ctx, rec.Subject.Uri)
		if err != nil {
			return err
		}

		rr := RepostRecord{
			RecCreated: rec.CreatedAt,
			Post:       fp.ID,
			Reposter:   evt.User,
			Author:     fp.Author,
			RecCid:     evt.RecCid.String(),
			Rkey:       evt.Rkey,
		}
		if err := fg.db.Create(&rr).Error; err != nil {
			return err
		}

		if err := fg.notifman.AddRepost(ctx, fp.Author, rr.ID, evt.User); err != nil {
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

		vr := VoteRecord{
			Dir:     dbdir,
			Voter:   evt.User,
			Post:    post.ID,
			Created: rec.CreatedAt,
			Rkey:    evt.Rkey,
			Cid:     evt.RecCid.String(),
		}
		if err := fg.db.Create(&vr).Error; err != nil {
			return err
		}

		if err := fg.db.Model(FeedPost{}).Where("id = ?", post.ID).Update("up_count", gorm.Expr("up_count + ?", val)).Error; err != nil {
			return err
		}

		if rec.Direction == "up" {
			if err := fg.addNewVoteNotification(ctx, act.ID, &vr); err != nil {
				return err
			}
		}

		return nil
	case *bsky.GraphFollow:
		subj, err := fg.lookupUserByDid(ctx, rec.Subject.Did)
		if err != nil {
			return err
		}
		// 'follower' followed 'target'
		fr := FollowRecord{
			Follower: evt.User,
			Target:   subj.ID,
			Rkey:     evt.Rkey,
			Cid:      evt.RecCid.String(),
		}
		if err := fg.db.Create(&fr).Error; err != nil {
			return err
		}

		if err := fg.notifman.AddFollow(ctx, fr.Follower, fr.Target, fr.ID); err != nil {
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

func (fg *FeedGenerator) lookupUserByHandle(ctx context.Context, handle string) (*ActorInfo, error) {
	var ai ActorInfo
	if err := fg.db.First(&ai, "handle = ?", handle).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (fg *FeedGenerator) addNewPostNotification(ctx context.Context, post *bsky.FeedPost, fp *FeedPost) error {
	if post.Reply != nil {
		replyto, err := fg.GetPost(ctx, post.Reply.Parent.Uri)
		if err != nil {
			fmt.Println("probably shouldnt error when processing a reply to a not-found post")
			return err
		}

		if err := fg.notifman.AddReplyTo(ctx, fp.Author, fp.ID, replyto); err != nil {
			return err
		}
	}

	for _, e := range post.Entities {
		switch e.Type {
		case "mention":
			mentioned, err := fg.lookupUserByDid(ctx, e.Value)
			if err != nil {
				return fmt.Errorf("mentioned user does not exist: %w", err)
			}

			if err := fg.notifman.AddMention(ctx, fp.Author, fp.ID, mentioned.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (fg *FeedGenerator) addNewVoteNotification(ctx context.Context, postauthor uint, vr *VoteRecord) error {
	return fg.notifman.AddUpVote(ctx, vr.Voter, vr.Post, vr.ID, postauthor)
}

func (fg *FeedGenerator) GetActorProfileByID(ctx context.Context, actor uint) (*ActorInfo, error) {
	var ai ActorInfo
	if err := fg.db.First(&ai, "id = ?", actor).Error; err != nil {
		return nil, fmt.Errorf("getActorProfileByID: %w", err)
	}

	return &ai, nil
}
func (fg *FeedGenerator) GetActorProfile(ctx context.Context, actor string) (*ActorInfo, error) {
	fmt.Println("get actor profile: ", actor)
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
	Post   *bsky.FeedFeedViewPost
	PostID uint

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

	p, ok := hi.Post.Record.(*bsky.FeedPost)
	if !ok {
		return nil, fmt.Errorf("getPostThread can only operate on app.bsky.feed.post records")
	}

	out := &ThreadPost{
		Post:   hi,
		PostID: post.ID,
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
	if err := fg.db.Limit(limit).Find(&follows, "follower = (?)", fg.db.Model(ActorInfo{}).Where("did = ? or handle = ?", user, user).Select("uid")).Error; err != nil {
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
		fai, err := fg.getActorRefInfo(ctx, f.Target)
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
