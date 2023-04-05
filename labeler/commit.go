package labeler

import (
	"context"
	"fmt"
	"strings"
	"time"

	label "github.com/bluesky-social/indigo/api/label"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/models"
	util "github.com/bluesky-social/indigo/util"

	"gorm.io/gorm/clause"
)

// Persist to database (and repo), and emit events.
func (s *Server) CommitLabels(ctx context.Context, labels []*label.Label, negate bool) error {

	now := time.Now()
	nowStr := now.Format(util.ISO8601)
	var labelRows []models.Label

	for _, l := range labels {
		l.Cts = nowStr

		path, _, err := s.repoman.CreateRecord(ctx, s.user.UserId, "com.atproto.label.label", l)
		if err != nil {
			return fmt.Errorf("failed to persist label in local repo: %w", err)
		}
		labelUri := "at://" + s.user.Did + "/" + path
		log.Infof("persisted label in repo: %s", labelUri)

		rkey := strings.SplitN(path, "/", 2)[1]
		lr := models.Label{
			Uri:       l.Uri,
			SourceDid: l.Src,
			Cid:       l.Cid,
			Val:       l.Val,
			RepoRKey:  &rkey,
			CreatedAt: now,
		}
		if negate {
			lr.NegatedAt = now
		}
		labelRows = append(labelRows, lr)
	}

	// ... and database ...
	if len(labelRows) > 0 {
		// TODO(bnewbold): don't clobber action labels (aka, human interventions)
		res := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&labelRows)
		if res.Error != nil {
			return res.Error
		}
	}

	// ... then re-publish as XRPCStreamEvent
	if len(labels) > 0 {
		log.Infof("broadcasting labels: %s", labels)
		lev := events.XRPCStreamEvent{
			LabelLabels: &label.SubscribeLabels_Labels{
				// NOTE(bnewbold): generic event handler code handles Seq field for us
				Labels: labels,
			},
		}
		err := s.evtmgr.AddEvent(ctx, &lev)
		if err != nil {
			return fmt.Errorf("failed to publish XRPCStreamEvent: %w", err)
		}
	}

	return nil
}
