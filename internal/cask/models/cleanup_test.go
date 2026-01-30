package models

import (
	"context"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/pkg/prototypes"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCleanupOldEvents_EmptyDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	deleted, err := m.CleanupOldEvents(ctx, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 0, deleted)
}

func TestCleanupOldEvents_NoOldEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write events with recent timestamps
	now := time.Now()
	for i := range 5 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
			ReceivedAt:  timestamppb.New(now.Add(-time.Duration(i) * time.Minute)),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Cleanup with 1 hour retention - nothing should be deleted
	deleted, err := m.CleanupOldEvents(ctx, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 0, deleted)

	// Verify all events still exist
	events, _, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, events, 5)
}

func TestCleanupOldEvents_DeletesOldEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	now := time.Now()

	// Write 3 old events (2 hours ago)
	for i := range 3 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("old event"),
			ReceivedAt:  timestamppb.New(now.Add(-2 * time.Hour)),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Write 2 recent events (5 minutes ago)
	for i := range 2 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(200 + i),
			EventType:   "#commit",
			RawEvent:    []byte("recent event"),
			ReceivedAt:  timestamppb.New(now.Add(-5 * time.Minute)),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Cleanup with 1 hour retention - should delete 3 old events
	deleted, err := m.CleanupOldEvents(ctx, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 3, deleted)

	// Verify only recent events remain
	events, _, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, int64(200), events[0].UpstreamSeq)
	require.Equal(t, int64(201), events[1].UpstreamSeq)
}

func TestCleanupOldEvents_DeletesAllEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	now := time.Now()

	// Write events that are all old
	for i := range 5 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("old event"),
			ReceivedAt:  timestamppb.New(now.Add(-48 * time.Hour)),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Cleanup with 24 hour retention - should delete all
	deleted, err := m.CleanupOldEvents(ctx, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 5, deleted)

	// Verify no events remain
	events, _, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, events, 0)
}

func TestCleanupOldEvents_DeletesCursorIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	now := time.Now()

	// Write an old event
	event := &prototypes.FirehoseEvent{
		UpstreamSeq: 12345,
		EventType:   "#commit",
		RawEvent:    []byte("old event"),
		ReceivedAt:  timestamppb.New(now.Add(-2 * time.Hour)),
	}
	err := m.WriteEvent(ctx, event)
	require.NoError(t, err)

	// Verify cursor index works before cleanup
	vsCursor, err := m.GetVersionstampForSeq(ctx, 12345)
	require.NoError(t, err)
	require.NotEmpty(t, vsCursor)

	// Cleanup
	deleted, err := m.CleanupOldEvents(ctx, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, deleted)

	// Cursor index should now return empty (event and index deleted)
	vsCursor, err = m.GetVersionstampForSeq(ctx, 12345)
	require.NoError(t, err)
	require.Empty(t, vsCursor)
}

func TestGetOldestEventAge_EmptyDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	age, err := m.GetOldestEventAge(ctx)
	require.NoError(t, err)
	require.Equal(t, time.Duration(0), age)
}

func TestGetOldestEventAge_WithEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	now := time.Now()
	oldestTime := now.Add(-3 * time.Hour)

	// Write oldest event first
	event := &prototypes.FirehoseEvent{
		UpstreamSeq: 100,
		EventType:   "#commit",
		RawEvent:    []byte("oldest event"),
		ReceivedAt:  timestamppb.New(oldestTime),
	}
	err := m.WriteEvent(ctx, event)
	require.NoError(t, err)

	// Write newer events
	for i := range 3 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(200 + i),
			EventType:   "#commit",
			RawEvent:    []byte("newer event"),
			ReceivedAt:  timestamppb.New(now.Add(-time.Duration(i) * time.Minute)),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	age, err := m.GetOldestEventAge(ctx)
	require.NoError(t, err)

	// Age should be approximately 3 hours (with some tolerance for test execution time)
	require.InDelta(t, 3*time.Hour, age, float64(10*time.Second))
}

func TestCleanupOldEvents_BatchProcessing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	now := time.Now()

	// Write more events than cleanupBatchSize (100) to test batching
	numEvents := 150
	for i := range numEvents {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(i),
			EventType:   "#commit",
			RawEvent:    []byte("old event"),
			ReceivedAt:  timestamppb.New(now.Add(-48 * time.Hour)),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Cleanup should process all events across multiple batches
	deleted, err := m.CleanupOldEvents(ctx, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, numEvents, deleted)

	// Verify all events are gone
	events, _, err := m.GetEventsSince(ctx, nil, 200)
	require.NoError(t, err)
	require.Len(t, events, 0)
}

func TestCleanupOldEvents_MixedAges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	now := time.Now()

	// Write events in mixed order: old, recent, old, recent, old
	timestamps := []time.Duration{
		-5 * time.Hour,    // old - delete
		-30 * time.Minute, // recent - keep
		-4 * time.Hour,    // old - delete
		-15 * time.Minute, // recent - keep
		-3 * time.Hour,    // old - delete
	}

	for i, offset := range timestamps {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(i),
			EventType:   "#commit",
			RawEvent:    []byte("event"),
			ReceivedAt:  timestamppb.New(now.Add(offset)),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// With 1 hour retention, only the first 3 events should be deleted
	// (cleanup stops at the first event within retention)
	// Since events are stored in write order, event 0 (oldest timestamp) is first,
	// but cleanup scans by versionstamp order (write order), not ReceivedAt
	deleted, err := m.CleanupOldEvents(ctx, time.Hour)
	require.NoError(t, err)

	// The cleanup scans from oldest versionstamp (write order).
	// Event 0 is old -> delete
	// Event 1 is recent -> stop scanning
	// So only 1 event deleted
	require.Equal(t, 1, deleted)

	// Run cleanup again - now event 1 is at the front (recent), so nothing deleted
	deleted, err = m.CleanupOldEvents(ctx, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 0, deleted)
}
