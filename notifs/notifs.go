package notifs

import (
	"context"
	"fmt"
	"time"

	appbskytypes "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/ipfs/go-cid"
	cbg "github.com/whyrusleeping/cbor-gen"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type NotificationManager struct {
	db *gorm.DB

	getRecord GetRecord
}
type GetRecord func(ctx context.Context, user uint, collection string, rkey string, maybeCid cid.Cid) (cid.Cid, cbg.CBORMarshaler, error)

func NewNotificationManager(db *gorm.DB, getrec GetRecord) *NotificationManager {
	db.AutoMigrate(&NotifRecord{})
	db.AutoMigrate(&NotifSeen{})

	return &NotificationManager{
		db:        db,
		getRecord: getrec,
	}
}

const (
	NotifKindReply   = 1
	NotifKindMention = 2
	NotifKindUpVote  = 3
	NotifKindFollow  = 4
	NotifKindRepost  = 5
)

type NotifRecord struct {
	gorm.Model
	For     uint
	Kind    int64
	Record  uint
	Who     uint
	ReplyTo uint
}

type NotifSeen struct {
	ID       uint `gorm:"primarykey"`
	Usr      uint `gorm:"uniqueIndex"`
	LastSeen time.Time
}

type HydratedNotification struct {
	Record        any
	IsRead        bool
	IndexedAt     time.Time
	Uri           string
	Cid           string
	Author        *appbskytypes.ActorRef_WithInfo
	Reason        string
	ReasonSubject *string
}

func (nm *NotificationManager) GetNotifications(ctx context.Context, user uint) ([]*appbskytypes.NotificationList_Notification, error) {
	var lastSeen time.Time
	if err := nm.db.Model(NotifSeen{}).Where("usr = ?", user).Select("last_seen").Scan(&lastSeen).Error; err != nil {
		return nil, err
	}

	var notifs []NotifRecord
	if err := nm.db.Order("created_at desc").Find(&notifs, "for = ?", user).Error; err != nil {
		return nil, err
	}

	/*
		Record        any                `json:"record" cborgen:"record"`
		IsRead        bool               `json:"isRead" cborgen:"isRead"`
		IndexedAt     string             `json:"indexedAt" cborgen:"indexedAt"`
		Uri           string             `json:"uri" cborgen:"uri"`
		Cid           string             `json:"cid" cborgen:"cid"`
		Author        *ActorRef_WithInfo `json:"author" cborgen:"author"`
		Reason        string             `json:"reason" cborgen:"reason"`
		ReasonSubject *string            `json:"reasonSubject" cborgen:"reasonSubject"`
	*/

	out := []*appbskytypes.NotificationList_Notification{}

	for _, n := range notifs {
		hn, err := nm.hydrateNotification(ctx, &n, lastSeen)
		if err != nil {
			return nil, err
		}

		// TODO: muting
		hn.Author.Viewer = &appbskytypes.ActorRef_ViewerState{}

		out = append(out, hn)
	}
	return out, nil
}

func (nm *NotificationManager) hydrateNotification(ctx context.Context, nrec *NotifRecord, lastSeen time.Time) (*appbskytypes.NotificationList_Notification, error) {

	switch nrec.Kind {
	case NotifKindReply:
		return nm.hydrateNotificationReply(ctx, nrec, lastSeen)
	case NotifKindFollow:
		return nm.hydrateNotificationFollow(ctx, nrec, lastSeen)
	case NotifKindUpVote:
		return nm.hydrateNotificationUpVote(ctx, nrec, lastSeen)
	case NotifKindRepost:
		return nm.hydrateNotificationRepost(ctx, nrec, lastSeen)
		/*
			case NotifKindMention:
				return nm.hydrateNotificationMention(ctx, nrec, lastSeen)
		*/
	default:
		return nil, fmt.Errorf("attempted to hydrate unknown notif kind: %d", nrec.Kind)
	}
}
func (nm *NotificationManager) getActor(ctx context.Context, act uint) (*models.ActorInfo, error) {
	var ai models.ActorInfo
	if err := nm.db.First(&ai, "id = ?", act).Error; err != nil {
		return nil, err
	}

	return &ai, nil
}

func (nm *NotificationManager) hydrateNotificationUpVote(ctx context.Context, nrec *NotifRecord, lastSeen time.Time) (*appbskytypes.NotificationList_Notification, error) {
	var votedOn models.FeedPost
	if err := nm.db.First(&votedOn, "id = ?", nrec.Record).Error; err != nil {
		return nil, err
	}

	voter, err := nm.getActor(ctx, nrec.Who)
	if err != nil {
		return nil, err
	}

	var vote models.VoteRecord
	if err := nm.db.First(&vote, "id = ?", nrec.Record).Error; err != nil {
		return nil, err
	}

	_, rec, err := nm.getRecord(ctx, voter.ID, "app.bsky.feed.vote", vote.Rkey, cid.Undef)
	if err != nil {
		return nil, fmt.Errorf("getting vote: %w", err)
	}

	postAuthor, err := nm.getActor(ctx, votedOn.Author)
	if err != nil {
		return nil, err
	}

	rsub := "at://" + postAuthor.Did + "/app.bsky.feed.post/" + votedOn.Rkey

	return &appbskytypes.NotificationList_Notification{
		Record:        util.LexiconTypeDecoder{Val: rec},
		IsRead:        nrec.CreatedAt.Before(lastSeen),
		IndexedAt:     nrec.CreatedAt.Format(time.RFC3339),
		Uri:           "at://" + voter.Did + "/app.bsky.feed.vote/" + vote.Rkey,
		Cid:           vote.Cid,
		Author:        voter.ActorRef(),
		Reason:        "vote",
		ReasonSubject: &rsub,
	}, nil
}

func (nm *NotificationManager) hydrateNotificationRepost(ctx context.Context, nrec *NotifRecord, lastSeen time.Time) (*appbskytypes.NotificationList_Notification, error) {
	var reposted models.FeedPost
	if err := nm.db.First(&reposted, "id = ?", nrec.Record).Error; err != nil {
		return nil, err
	}

	reposter, err := nm.getActor(ctx, nrec.Who)
	if err != nil {
		return nil, err
	}

	var repost models.RepostRecord
	if err := nm.db.First(&repost, "id = ?", nrec.Record).Error; err != nil {
		return nil, err
	}

	_, rec, err := nm.getRecord(ctx, reposter.ID, "app.bsky.feed.repost", repost.Rkey, cid.Undef)
	if err != nil {
		return nil, fmt.Errorf("getting repost: %w", err)
	}

	postAuthor, err := nm.getActor(ctx, repost.Author)
	if err != nil {
		return nil, err
	}

	rsub := "at://" + postAuthor.Did + "/app.bsky.feed.post/" + reposted.Rkey

	return &appbskytypes.NotificationList_Notification{
		Record:        util.LexiconTypeDecoder{rec},
		IsRead:        nrec.CreatedAt.Before(lastSeen),
		IndexedAt:     nrec.CreatedAt.Format(time.RFC3339),
		Uri:           "at://" + reposter.Did + "/app.bsky.feed.repost/" + repost.Rkey,
		Cid:           repost.RecCid,
		Author:        reposter.ActorRef(),
		Reason:        "repost",
		ReasonSubject: &rsub,
	}, nil
}

func (nm *NotificationManager) hydrateNotificationReply(ctx context.Context, nrec *NotifRecord, lastSeen time.Time) (*appbskytypes.NotificationList_Notification, error) {
	var fp models.FeedPost
	if err := nm.db.First(&fp, "id = ?", nrec.Record).Error; err != nil {
		return nil, err
	}

	var replyTo models.FeedPost
	if err := nm.db.First(&replyTo, "id = ?", nrec.ReplyTo).Error; err != nil {
		return nil, err
	}

	var author models.ActorInfo
	if err := nm.db.First(&author, "id = ?", fp.Author).Error; err != nil {
		return nil, err
	}

	var opAuthor models.ActorInfo
	if err := nm.db.First(&opAuthor, "id = ?", replyTo.Author).Error; err != nil {
		return nil, err
	}

	_, rec, err := nm.getRecord(ctx, author.ID, "app.bsky.feed.post", fp.Rkey, cid.Undef)
	if err != nil {
		return nil, err
	}

	rsub := "at://" + opAuthor.Did + "/app.bsky.feed.post/" + replyTo.Rkey

	return &appbskytypes.NotificationList_Notification{
		Record:        util.LexiconTypeDecoder{rec},
		IsRead:        nrec.CreatedAt.Before(lastSeen),
		IndexedAt:     nrec.CreatedAt.Format(time.RFC3339),
		Uri:           "at://" + author.Did + "/app.bsky.feed.post/" + fp.Rkey,
		Cid:           fp.Cid,
		Author:        author.ActorRef(),
		Reason:        "reply",
		ReasonSubject: &rsub,
	}, nil
}

func (nm *NotificationManager) hydrateNotificationFollow(ctx context.Context, nrec *NotifRecord, lastSeen time.Time) (*appbskytypes.NotificationList_Notification, error) {
	var frec models.FollowRecord
	if err := nm.db.First(&frec, "id = ?", nrec.Record).Error; err != nil {
		return nil, err
	}

	var follower models.ActorInfo
	if err := nm.db.First(&follower, "id = ?", nrec.Who).Error; err != nil {
		return nil, err
	}

	_, rec, err := nm.getRecord(ctx, follower.ID, "app.bsky.graph.follow", frec.Rkey, cid.Undef)
	if err != nil {
		return nil, err
	}

	return &appbskytypes.NotificationList_Notification{
		Record:    util.LexiconTypeDecoder{rec},
		IsRead:    nrec.CreatedAt.Before(lastSeen),
		IndexedAt: nrec.CreatedAt.Format(time.RFC3339),
		Uri:       "at://" + follower.Did + "/app.bsky.graph.follow/" + frec.Rkey,
		Cid:       frec.Cid,
		Author:    follower.ActorRef(),
		Reason:    "follow",
	}, nil

}

func (nm *NotificationManager) GetCount(ctx context.Context, user uint) (int64, error) {
	// TODO: sql count is inefficient
	var lseen time.Time
	if err := nm.db.Model(NotifSeen{}).Where("usr = ?", user).Select("last_seen").Scan(&lseen).Error; err != nil {
		return 0, err
	}

	var c int64
	//seen := nm.db.Model(NotifSeen{}).Where("usr = ?", user).Select("last_seen")
	if err := nm.db.Model(NotifRecord{}).Where("for = ? AND created_at > ?", user, lseen).Count(&c).Error; err != nil {
		return 0, err
	}

	return c, nil
}

func (nm *NotificationManager) UpdateSeen(ctx context.Context, usr uint, seen time.Time) error {
	if err := nm.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "usr"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_seen"}),
	}).Create(NotifSeen{
		Usr:      usr,
		LastSeen: seen,
	}).Error; err != nil {
		return err
	}

	return nil
}

func (nm *NotificationManager) AddReplyTo(ctx context.Context, user uint, replyid uint, replyto *models.FeedPost) error {
	return nm.db.Create(&NotifRecord{
		Kind:    NotifKindReply,
		For:     replyto.Author,
		Who:     user,
		ReplyTo: replyto.ID,
		Record:  replyid,
	}).Error
}

func (nm *NotificationManager) AddMention(ctx context.Context, user uint, postid uint, mentioned uint) error {
	return nm.db.Create(&NotifRecord{
		For:    mentioned,
		Kind:   NotifKindMention,
		Record: postid,
		Who:    user,
	}).Error
}

func (nm *NotificationManager) AddUpVote(ctx context.Context, voter uint, postid uint, voteid uint, postauthor uint) error {
	return nm.db.Create(&NotifRecord{
		For:     postauthor,
		Kind:    NotifKindUpVote,
		ReplyTo: postid,
		Record:  voteid,
		Who:     voter,
	}).Error
}

func (nm *NotificationManager) AddFollow(ctx context.Context, follower, followed, recid uint) error {
	return nm.db.Create(&NotifRecord{
		Kind:   NotifKindFollow,
		For:    followed,
		Who:    follower,
		Record: recid,
	}).Error
}

func (nm *NotificationManager) AddRepost(ctx context.Context, op uint, repost, reposter uint) error {
	return nm.db.Create(&NotifRecord{
		Kind:   NotifKindRepost,
		For:    op,
		Record: repost,
		Who:    reposter,
	}).Error
}
