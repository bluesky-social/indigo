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

func (nn *NullNotifs) GetNotifications(ctx context.Context, user uint) ([]*appbskytypes.NotificationList_Notification, error) {
	return nil, fmt.Errorf("no notifications engine loaded")
}

func (nn *NullNotifs) GetCount(ctx context.Context, user uint) (int64, error) {
	return 0, fmt.Errorf("no notifications engine loaded")
}

func (nn *NullNotifs) UpdateSeen(ctx context.Context, usr uint, seen time.Time) error {
	return nil
}

func (nn *NullNotifs) AddReplyTo(ctx context.Context, user uint, replyid uint, replyto *models.FeedPost) error {
	return nil
}

func (nn *NullNotifs) AddMention(ctx context.Context, user uint, postid uint, mentioned uint) error {
	return nil
}

func (nn *NullNotifs) AddUpVote(ctx context.Context, voter uint, postid uint, voteid uint, postauthor uint) error {
	return nil
}

func (nn *NullNotifs) AddFollow(ctx context.Context, follower, followed, recid uint) error {
	return nil
}

func (nn *NullNotifs) AddRepost(ctx context.Context, op uint, repost, reposter uint) error {
	return nil
}
