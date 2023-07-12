package notifs

import (
	"context"
	"fmt"
	"time"

	appbskytypes "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/models"
)

type NullNotifs struct {
}

var _ NotificationManager = (*NullNotifs)(nil)

func (nn *NullNotifs) GetNotifications(ctx context.Context, user models.Uid) ([]*appbskytypes.NotificationListNotifications_Notification, error) {
	return nil, fmt.Errorf("no notifications engine loaded")
}

func (nn *NullNotifs) GetCount(ctx context.Context, user models.Uid) (int64, error) {
	return 0, fmt.Errorf("no notifications engine loaded")
}

func (nn *NullNotifs) UpdateSeen(ctx context.Context, usr models.Uid, seen time.Time) error {
	return nil
}

func (nn *NullNotifs) AddReplyTo(ctx context.Context, user models.Uid, replyid uint, replyto *models.FeedPost) error {
	return nil
}

func (nn *NullNotifs) AddMention(ctx context.Context, user models.Uid, postid uint, mentioned models.Uid) error {
	return nil
}

func (nn *NullNotifs) AddUpVote(ctx context.Context, voter models.Uid, postid uint, voteid uint, postauthor models.Uid) error {
	return nil
}

func (nn *NullNotifs) AddFollow(ctx context.Context, follower, followed models.Uid, recid uint) error {
	return nil
}

func (nn *NullNotifs) AddRepost(ctx context.Context, op models.Uid, repost uint, reposter models.Uid) error {
	return nil
}
