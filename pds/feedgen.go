package pds

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/indexer"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"

	"github.com/ipfs/go-cid"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

type FeedGenerator struct {
	db *gorm.DB
	ix *indexer.Indexer

	readRecord ReadRecordFunc

	log *slog.Logger
}

func NewFeedGenerator(db *gorm.DB, ix *indexer.Indexer, readRecord ReadRecordFunc, log *slog.Logger) (*FeedGenerator, error) {
	return &FeedGenerator{
		db:         db,
		ix:         ix,
		readRecord: readRecord,
		log:        log,
	}, nil
}

type ReadRecordFunc func(context.Context, models.Uid, cid.Cid) (lexutil.CBOR, error)

/*
type HydratedFeedItem struct {
	Uri           string
	RepostedBy    *bsky.ActorDefs_ProfileViewBasic
	Record        any
	ReplyCount    int64
	RepostCount   int64
	UpvoteCount   int64
	DownvoteCount int64
	MyState       *bsky.FeedGetAuthorFeed_MyState
	Cid       string
	Author    *bsky.ActorDefs_ProfileViewBasic
	TrendedBy *bsky.ActorDefs_ProfileViewBasic
	Embed     *bsky.FeedEmbed
	IndexedAt string
}
*/

func (fg *FeedGenerator) hydrateFeed(ctx context.Context, items []*models.FeedPost, reposts []*models.RepostRecord) ([]*bsky.FeedDefs_FeedViewPost, error) {
	out := make([]*bsky.FeedDefs_FeedViewPost, 0, len(items))
	for _, it := range items {
		hit, err := fg.hydrateItem(ctx, it)
		if err != nil {
			return nil, err
		}

		out = append(out, hit)
	}

	if len(reposts) > 0 {
		for _, rp := range reposts {
			var fp models.FeedPost
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

			fvp.Reason = &bsky.FeedDefs_FeedViewPost_Reason{
				FeedDefs_ReasonRepost: &bsky.FeedDefs_ReasonRepost{
					By:        reposter,
					IndexedAt: rp.CreatedAt.Format(time.RFC3339),
				},
			}

			out = append(out, fvp)
		}
	}

	return out, nil
}

func (fg *FeedGenerator) didForUser(ctx context.Context, user models.Uid) (string, error) {
	// TODO: cache the shit out of this
	var ai models.ActorInfo
	if err := fg.db.First(&ai, "uid = ?", user).Error; err != nil {
		return "", err
	}

	return ai.Did, nil
}

func (fg *FeedGenerator) getActorRefInfo(ctx context.Context, user models.Uid) (*bsky.ActorDefs_ProfileViewBasic, error) {
	// TODO: cache the shit out of this too
	var ai models.ActorInfo
	if err := fg.db.First(&ai, "uid = ?", user).Error; err != nil {
		return nil, err
	}

	return ai.ActorRef(), nil
}

func (fg *FeedGenerator) hydrateItem(ctx context.Context, item *models.FeedPost) (*bsky.FeedDefs_FeedViewPost, error) {
	authorDid, err := fg.didForUser(ctx, item.Author)
	if err != nil {
		return nil, err
	}

	out := &bsky.FeedDefs_FeedViewPost{}

	out.Post = &bsky.FeedDefs_PostView{
		Uri:         "at://" + authorDid + "/app.bsky.feed.post/" + item.Rkey,
		ReplyCount:  &item.ReplyCount,
		RepostCount: &item.RepostCount,
		LikeCount:   &item.UpCount,
		Cid:         item.Cid,
		IndexedAt:   item.UpdatedAt.Format(time.RFC3339),
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

	out.Post.Record = &lexutil.LexiconTypeDecoder{Val: rec}

	return out, nil
}

func (fg *FeedGenerator) getPostViewerState(ctx context.Context, item uint, viewer models.Uid, viewerDid string) (*bsky.FeedDefs_ViewerState, error) {
	var out bsky.FeedDefs_ViewerState

	var vote models.VoteRecord
	if err := fg.db.Find(&vote, "post = ? AND voter = ?", item, viewer).Error; err != nil {
		return nil, err
	}

	if vote.ID != 0 {
		vuri := fmt.Sprintf("at://%s/app.bsky.feed.vote/%s", viewerDid, vote.Rkey)
		out.Like = &vuri
	}

	var rep models.RepostRecord
	if err := fg.db.Find(&rep, "post = ? AND reposter = ?", item, viewer).Error; err != nil {
		return nil, err
	}

	if rep.ID != 0 {
		rpuri := fmt.Sprintf("at://%s/app.bsky.feed.repost/%s", viewerDid, rep.Rkey)
		out.Repost = &rpuri
	}

	return &out, nil
}

func (fg *FeedGenerator) GetTimeline(ctx context.Context, user *User, algo string, before string, limit int) ([]*bsky.FeedDefs_FeedViewPost, error) {
	ctx, span := otel.Tracer("feedgen").Start(context.Background(), "GetTimeline")
	defer span.End()

	// TODO: this query is just a temporary hack...
	var feed []*models.FeedPost
	if err := fg.db.Debug().Find(&feed, "author in (?)",
		fg.db.Model(models.FollowRecord{}).Where("follower = ?", user.ID).Select("target"),
	).Error; err != nil {
		return nil, err
	}

	var rps []*models.RepostRecord
	if err := fg.db.Debug().Find(&rps, "reposter in (?)",
		fg.db.Model(models.FollowRecord{}).Where("follower = ?", user.ID).Select("target"),
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

func (fg *FeedGenerator) personalizeFeed(ctx context.Context, feed []*bsky.FeedDefs_FeedViewPost, viewer *User) ([]*bsky.FeedDefs_FeedViewPost, error) {
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

func (fg *FeedGenerator) GetAuthorFeed(ctx context.Context, user *User, before string, limit int) ([]*bsky.FeedDefs_FeedViewPost, error) {
	ctx, span := otel.Tracer("feedgen").Start(context.Background(), "GetAuthorFeed")
	defer span.End()

	// for memory efficiency, should probably return the actual type that goes out to the user...
	// bsky.FeedGetAuthorFeed_FeedItem

	var feed []*models.FeedPost
	if err := fg.db.Find(&feed, "author = ?", user.ID).Error; err != nil {
		return nil, err
	}

	var reposts []*models.RepostRecord
	if err := fg.db.Find(&reposts, "reposter = ?", user.ID).Error; err != nil {
		return nil, err
	}

	fout, err := fg.hydrateFeed(ctx, feed, reposts)
	if err != nil {
		return nil, fmt.Errorf("hydrating feed: %w", err)
	}

	return fg.personalizeFeed(ctx, fout, user)
}

func (fg *FeedGenerator) GetActorProfileByID(ctx context.Context, actor uint) (*models.ActorInfo, error) {
	var ai models.ActorInfo
	if err := fg.db.First(&ai, "id = ?", actor).Error; err != nil {
		return nil, fmt.Errorf("getActorProfileByID: %w", err)
	}

	return &ai, nil
}
func (fg *FeedGenerator) GetActorProfile(ctx context.Context, actor string) (*models.ActorInfo, error) {
	fmt.Println("get actor profile: ", actor)
	var ai models.ActorInfo
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
	Post   *bsky.FeedDefs_FeedViewPost
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
	Actor     *bsky.ActorDefs_ProfileViewBasic
	Direction string
	IndexedAt time.Time
	CreatedAt string
}

func (fg *FeedGenerator) hydrateVote(ctx context.Context, v *models.VoteRecord) (*HydratedVote, error) {
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

func (fg *FeedGenerator) GetVotes(ctx context.Context, uri string, pcid cid.Cid, limit int, before string) ([]*HydratedVote, error) {
	if before != "" {
		fg.log.Warn("not respecting 'before' yet")
	}

	p, err := fg.ix.GetPost(ctx, uri)
	if err != nil {
		return nil, err
	}

	if p.Cid != pcid.String() {
		return nil, fmt.Errorf("listing likes of old post versions not supported")
	}

	var voterecs []models.VoteRecord
	if err := fg.db.Limit(limit).Find(&voterecs, "post = ?", p.ID).Error; err != nil {
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
	Follower  *bsky.ActorDefs_ProfileViewBasic
	Subject   *bsky.ActorDefs_ProfileViewBasic
	CreatedAt string
	IndexedAt string
}

func (fg *FeedGenerator) GetFollows(ctx context.Context, user string, limit int, before string) ([]*FollowInfo, error) {
	var follows []models.FollowRecord
	if err := fg.db.Limit(limit).Find(&follows, "follower = (?)", fg.db.Model(models.ActorInfo{}).Where("did = ? or handle = ?", user, user).Select("uid")).Error; err != nil {
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
