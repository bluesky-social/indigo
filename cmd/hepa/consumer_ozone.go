package main

import (
	"context"
	"fmt"
	"time"

	toolsozone "github.com/bluesky-social/indigo/api/ozone"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func (s *Server) RunOzoneConsumer(ctx context.Context) error {

	cur, err := s.ReadLastOzoneCursor(ctx)
	if err != nil {
		return err
	}

	if cur == "" {
		cur = syntax.DatetimeNow().String()
	}
	since, err := syntax.ParseDatetime(cur)
	if err != nil {
		return err
	}

	s.logger.Info("subscribing to ozone event log", "upstream", s.engine.OzoneClient.Host, "cursor", cur, "since", since)
	var limit int64 = 50
	period := time.Second * 5

	for {
		//func ModerationQueryEvents(ctx context.Context, c *xrpc.Client, addedLabels []string, addedTags []string, comment string, createdAfter string, createdBefore string, createdBy string, cursor string, hasComment bool, includeAllUserRecords bool, limit int64, removedLabels []string, removedTags []string, reportTypes []string, sortDirection string, subject string, types []string) (*ModerationQueryEvents_Output, error) {
		me, err := toolsozone.ModerationQueryEvents(
			ctx,
			s.engine.OzoneClient,
			nil,            // addedLabels: If specified, only events where all of these labels were added are returned
			nil,            // addedTags: If specified, only events where all of these tags were added are returned
			"",             // comment: If specified, only events with comments containing the keyword are returned
			since.String(), // createdAfter: Retrieve events created after a given timestamp
			"",             // createdBefore: Retrieve events created before a given timestamp
			"",             // createdBy
			"",             // cursor
			false,          // hasComment: If true, only events with comments are returned
			true,           // includeAllUserRecords: If true, events on all record types (posts, lists, profile etc.) owned by the did are returned
			limit,
			nil,   // removedLabels: If specified, only events where all of these labels were removed are returned
			nil,   // removedTags
			nil,   // reportTypes
			"asc", // sortDirection: Sort direction for the events. Defaults to descending order of created at timestamp.
			"",    // subject
			nil,   // types: The types of events (fully qualified string in the format of tools.ozone.moderation.defs#modEvent<name>) to filter by. If not specified, all events are returned.
		)
		if err != nil {
			s.logger.Warn("ozone query events failed; sleeping then will retrying", "err", err, "period", period.String())
			time.Sleep(period)
			continue
		}

		// track if the response contained anything new
		anyNewEvents := false
		for _, evt := range me.Events {
			createdAt, err := syntax.ParseDatetime(evt.CreatedAt)
			if err != nil {
				return fmt.Errorf("invalid time format for ozone 'createdAt': %w", err)
			}
			// skip if the timestamp is the exact same
			if createdAt == since {
				continue
			}
			anyNewEvents = true
			// TODO: is there a race condition here?
			if !createdAt.Time().After(since.Time()) {
				s.logger.Error("out of order ozone event", "createdAt", createdAt, "since", since)
				return fmt.Errorf("out of order ozone event")
			}
			if err = s.HandleOzoneEvent(ctx, evt); err != nil {
				s.logger.Error("failed to process ozone event", "event", evt)
			}
			since = createdAt
			s.lastOzoneCursor.Store(since.String())
		}
		if !anyNewEvents {
			s.logger.Debug("... ozone poller sleeping", "period", period.String())
			time.Sleep(period)
		}
	}
}

func (s *Server) HandleOzoneEvent(ctx context.Context, eventView *toolsozone.ModerationDefs_ModEventView) error {

	s.logger.Debug("received ozone event", "eventID", eventView.Id, "createdAt", eventView.CreatedAt)

	if err := s.engine.ProcessOzoneEvent(ctx, eventView); err != nil {
		s.logger.Error("engine failed to process ozone event", "err", err)
	}
	return nil
}
