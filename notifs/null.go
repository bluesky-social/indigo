package notifs

import (
	"context"
	"fmt"
	"time"

	appbskytypes "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/util"
)

type NullNotifs struct {
}

var _ NotificationManager = (*NullNotifs)(nil)

func (nn *NullNotifs) GetNotifications(ctx context.Context, user util.Uid) ([]*appbskytypes.NotificationListNotifications_Notification, error) {
	return nil, fmt.Errorf("no notifications engine loaded")
}

func (nn *NullNotifs) GetCount(ctx context.Context, user util.Uid) (int64, error) {
	return 0, fmt.Errorf("no notifications engine loaded")
}

func (nn *NullNotifs) UpdateSeen(ctx context.Context, usr util.Uid, seen time.Time) error {
	return nil
}

func (nn *NullNotifs) AddReplyTo(ctx context.Context, user util.Uid, replyid uint, replyto *models.FeedPost) error {
	return nil
}

func (nn *NullNotifs) AddMention(ctx context.Context, user util.Uid, postid uint, mentioned util.Uid) error {
	return nil
}

func (nn *NullNotifs) AddUpVote(ctx context.Context, voter util.Uid, postid uint, voteid uint, postauthor util.Uid) error {
	return nil
}

func (nn *NullNotifs) AddFollow(ctx context.Context, follower, followed util.Uid, recid uint) error {
	return nil
}

func (nn *NullNotifs) AddRepost(ctx context.Context, op util.Uid, repost uint, reposter util.Uid) error {
	return nil
}
