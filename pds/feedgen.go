package pds

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/indexer"
	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/types"
	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

type FeedGenerator struct {
	db *gorm.DB
	ix *indexer.Indexer

	readRecord ReadRecordFunc
}

func NewFeedGenerator(db *gorm.DB, ix *indexer.Indexer, readRecord ReadRecordFunc) (*FeedGenerator, error) {
	return &FeedGenerator{
		db:         db,
		ix:         ix,
		readRecord: readRecord,
	}, nil
}

type ReadRecordFunc func(context.Context, uint, cid.Cid) (util.CBOR, error)

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

func (fg *FeedGenerator) hydrateFeed(ctx context.Context, items []*types.FeedPost, reposts []*types.RepostRecord) ([]*bsky.FeedFeedViewPost, error) {
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
			var fp types.FeedPost
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
	var ai types.ActorInfo
	if err := fg.db.First(&ai, "uid = ?", user).Error; err != nil {
		return "", err
	}

	return ai.Did, nil
}

func (fg *FeedGenerator) getActorRefInfo(ctx context.Context, user uint) (*bsky.ActorRef_WithInfo, error) {
	// TODO: cache the shit out of this too
	var ai types.ActorInfo
	if err := fg.db.First(&ai, "uid = ?", user).Error; err != nil {
		return nil, err
	}

	return ai.ActorRef(), nil
}

func (fg *FeedGenerator) hydrateItem(ctx context.Context, item *types.FeedPost) (*bsky.FeedFeedViewPost, error) {
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

	reccid, err := cid.Decode(item.Cid)
	if err != nil {
		return nil, err
	}

	rec, err := fg.readRecord(ctx, item.Author, reccid)
	if err != nil {
		return nil, err
	}

	out.Post.Record = util.LexiconTypeDecoder{rec}

	return out, nil
}

func (fg *FeedGenerator) getPostViewerState(ctx context.Context, item uint, viewer uint, viewerDid string) (*bsky.FeedPost_ViewerState, error) {
	var out bsky.FeedPost_ViewerState

	var vote types.VoteRecord
	if err := fg.db.Find(&vote, "post = ? AND voter = ?", item, viewer).Error; err != nil {
		return nil, err
	}

	if vote.ID != 0 {
		vuri := fmt.Sprintf("at://%s/app.bsky.feed.vote/%s", viewerDid, vote.Rkey)
		switch vote.Dir {
		case types.VoteDirUp:
			out.Upvote = &vuri
		case types.VoteDirDown:
			out.Downvote = &vuri
		}
	}

	var rep types.RepostRecord
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
	var feed []*types.FeedPost
	if err := fg.db.Debug().Find(&feed, "author in (?)",
		fg.db.Model(types.FollowRecord{}).Where("follower = ?", user.ID).Select("target"),
	).Error; err != nil {
		return nil, err
	}

	var rps []*types.RepostRecord
	if err := fg.db.Debug().Find(&rps, "reposter in (?)",
		fg.db.Model(types.FollowRecord{}).Where("follower = ?", user.ID).Select("target"),
	).Error; err != nil {
		return nil, err
	}

	fout, err := fg.hydrateFeed(ctx, feed, rps)
	if err != nil {
		return nil, fmt.Errorf("hydrating feed: %w", err)
	}

	sort.Slice(fout, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339, fout[i].Post.IndexedAt)
		tj, _ := time.Parse(time.RFC3339, fout[j].Post.IndexedAt)

		return tj.Before(ti)
	})

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
		item, err := fg.ix.GetPost(ctx, p.Post.Uri)
		if err != nil {
			return nil, err
		}

		vs, err := fg.getPostViewerState(ctx, item.ID, viewer.ID, viewer.Did)
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

	var feed []*types.FeedPost
	if err := fg.db.Find(&feed, "author = ?", user.ID).Error; err != nil {
		return nil, err
	}

	var reposts []*types.RepostRecord
	if err := fg.db.Find(&reposts, "reposter = ?", user.ID).Error; err != nil {
		return nil, err
	}

	fout, err := fg.hydrateFeed(ctx, feed, reposts)
	if err != nil {
		return nil, fmt.Errorf("hydrating feed: %w", err)
	}

	return fg.personalizeFeed(ctx, fout, user)
}

func (fg *FeedGenerator) GetActorProfileByID(ctx context.Context, actor uint) (*types.ActorInfo, error) {
	var ai types.ActorInfo
	if err := fg.db.First(&ai, "id = ?", actor).Error; err != nil {
		return nil, fmt.Errorf("getActorProfileByID: %w", err)
	}

	return &ai, nil
}
func (fg *FeedGenerator) GetActorProfile(ctx context.Context, actor string) (*types.ActorInfo, error) {
	fmt.Println("get actor profile: ", actor)
	var ai types.ActorInfo
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

type ThreadPost struct {
	Post   *bsky.FeedFeedViewPost
	PostID uint

	ParentUri string
	Parent    *ThreadPost
}

func (fg *FeedGenerator) GetPostThread(ctx context.Context, uri string, depth int) (*ThreadPost, error) {
	post, err := fg.ix.GetPost(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("getting post for thread: %w", err)
	}

	hi, err := fg.hydrateItem(ctx, post)
	if err != nil {
		return nil, err
	}

	p, ok := hi.Post.Record.Val.(*bsky.FeedPost)
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

func (fg *FeedGenerator) hydrateVote(ctx context.Context, v *types.VoteRecord) (*HydratedVote, error) {
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
		log.Warn("not respecting 'before' yet")
	}

	p, err := fg.ix.GetPost(ctx, uri)
	if err != nil {
		return nil, err
	}

	if p.Cid != pcid.String() {
		return nil, fmt.Errorf("listing likes of old post versions not supported")
	}

	var dbdir types.VoteDir
	switch dir {
	case "up":
		dbdir = types.VoteDirUp
	case "down":
		dbdir = types.VoteDirDown
	default:
		return nil, fmt.Errorf("there are only two directions, up or down")
	}

	var voterecs []types.VoteRecord
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
	var follows []types.FollowRecord
	if err := fg.db.Limit(limit).Find(&follows, "follower = (?)", fg.db.Model(types.ActorInfo{}).Where("did = ? or handle = ?", user, user).Select("uid")).Error; err != nil {
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
