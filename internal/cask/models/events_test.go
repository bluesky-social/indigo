package models

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/internal/testutil"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/bluesky-social/indigo/pkg/prototypes"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func testDB(t *testing.T) *foundation.DB {
	t.Helper()
	return testutil.TestFoundationDB(t)
}

func testModels(t *testing.T) *Models {
	t.Helper()
	db := testDB(t)

	// Use a unique prefix for each test to ensure isolation
	m, err := NewWithPrefix(db, uuid.NewString())
	require.NoError(t, err)
	return m
}

func TestWriteEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	event := &prototypes.FirehoseEvent{
		UpstreamSeq: 12345,
		EventType:   "#commit",
		RawEvent:    []byte("test raw event data"),
	}

	err := m.WriteEvent(ctx, event)
	require.NoError(t, err)

	// Verify we can read it back
	events, cursor, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.NotEmpty(t, cursor)

	require.Equal(t, int64(12345), events[0].UpstreamSeq)
	require.Equal(t, "#commit", events[0].EventType)
	require.Equal(t, []byte("test raw event data"), events[0].RawEvent)
}

func TestWriteEvent_Multiple(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write multiple events
	for i := range 5 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Read all events
	events, _, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, events, 5)

	// Verify they're in order (by upstream seq, which we wrote in order)
	for i, event := range events {
		require.Equal(t, int64(100+i), event.UpstreamSeq)
	}
}

func TestGetEventsSince_Pagination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write 10 events
	for i := range 10 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(i),
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Read first 3
	events, cursor, err := m.GetEventsSince(ctx, nil, 3)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.NotEmpty(t, cursor)
	require.Equal(t, int64(0), events[0].UpstreamSeq)
	require.Equal(t, int64(1), events[1].UpstreamSeq)
	require.Equal(t, int64(2), events[2].UpstreamSeq)

	// Read next 3 using cursor
	events, cursor, err = m.GetEventsSince(ctx, cursor, 3)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.NotEmpty(t, cursor)
	require.Equal(t, int64(3), events[0].UpstreamSeq)
	require.Equal(t, int64(4), events[1].UpstreamSeq)
	require.Equal(t, int64(5), events[2].UpstreamSeq)

	// Read remaining 4
	events, cursor, err = m.GetEventsSince(ctx, cursor, 10)
	require.NoError(t, err)
	require.Len(t, events, 4)
	require.NotEmpty(t, cursor)
	require.Equal(t, int64(6), events[0].UpstreamSeq)
	require.Equal(t, int64(9), events[3].UpstreamSeq)

	// Reading past the end should return empty
	events, cursor, err = m.GetEventsSince(ctx, cursor, 10)
	require.NoError(t, err)
	require.Len(t, events, 0)
	require.Empty(t, cursor)
}

func TestGetEventsSince_EmptyDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	events, cursor, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, events, 0)
	require.Empty(t, cursor)
}

func TestGetLatestUpstreamSeq(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Empty database should return 0
	seq, err := m.GetLatestUpstreamSeq(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(0), seq)

	// Write some events
	for i := range 5 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Should return the last event's upstream seq
	seq, err = m.GetLatestUpstreamSeq(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(104), seq)

	// Write one more
	event := &prototypes.FirehoseEvent{
		UpstreamSeq: 999,
		EventType:   "#identity",
		RawEvent:    []byte("identity event"),
	}
	err = m.WriteEvent(ctx, event)
	require.NoError(t, err)

	// Should return the new latest
	seq, err = m.GetLatestUpstreamSeq(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(999), seq)
}

func TestEventsOrdering(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write events with non-sequential upstream seqs to verify
	// our internal ordering (versionstamp) is independent
	upstreamSeqs := []int64{500, 100, 999, 50, 750}

	for _, seq := range upstreamSeqs {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: seq,
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Events should come back in write order, not upstream seq order
	events, _, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, events, 5)

	for i, event := range events {
		require.Equal(t, upstreamSeqs[i], event.UpstreamSeq,
			"event %d should have upstream seq %d", i, upstreamSeqs[i])
	}
}

func TestCursorIsVersionstamp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write an event
	event := &prototypes.FirehoseEvent{
		UpstreamSeq: 123,
		EventType:   "#commit",
		RawEvent:    []byte("test"),
	}
	err := m.WriteEvent(ctx, event)
	require.NoError(t, err)

	// Get the cursor
	_, cursor, err := m.GetEventsSince(ctx, nil, 1)
	require.NoError(t, err)

	// Cursor should be exactly 10 bytes (versionstamp length)
	require.Len(t, cursor, versionstampLength)
}
