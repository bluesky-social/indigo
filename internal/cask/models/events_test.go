package models

import (
	"context"
	"crypto/rand"
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

func TestGetVersionstampForSeq_Basic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write events with sequential upstream seqs
	for i := range 5 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Get versionstamp for seq 101, then get events after it (should return 102, 103, 104)
	vsCursor, err := m.GetVersionstampForSeq(ctx, 101)
	require.NoError(t, err)
	require.NotEmpty(t, vsCursor)

	events, cursor, err := m.GetEventsSince(ctx, vsCursor, 10)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.NotEmpty(t, cursor)

	require.Equal(t, int64(102), events[0].UpstreamSeq)
	require.Equal(t, int64(103), events[1].UpstreamSeq)
	require.Equal(t, int64(104), events[2].UpstreamSeq)
}

func TestGetVersionstampForSeq_WithGaps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write events with gaps: 1000, 1005, 1010
	seqs := []int64{1000, 1005, 1010}
	for _, seq := range seqs {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: seq,
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Request cursor=1002 (doesn't exist, should floor to 1000)
	// Should return events after 1000: 1005, 1010
	vsCursor, err := m.GetVersionstampForSeq(ctx, 1002)
	require.NoError(t, err)
	require.NotEmpty(t, vsCursor)

	events, cursor, err := m.GetEventsSince(ctx, vsCursor, 10)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.NotEmpty(t, cursor)

	require.Equal(t, int64(1005), events[0].UpstreamSeq)
	require.Equal(t, int64(1010), events[1].UpstreamSeq)
}

func TestGetVersionstampForSeq_CursorMatchesExactly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write events with gaps: 1000, 1005, 1010
	seqs := []int64{1000, 1005, 1010}
	for _, seq := range seqs {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: seq,
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Request cursor=1005 (exact match)
	// Should return events after 1005: just 1010
	vsCursor, err := m.GetVersionstampForSeq(ctx, 1005)
	require.NoError(t, err)
	require.NotEmpty(t, vsCursor)

	events, cursor, err := m.GetEventsSince(ctx, vsCursor, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.NotEmpty(t, cursor)

	require.Equal(t, int64(1010), events[0].UpstreamSeq)
}

func TestGetVersionstampForSeq_CursorBeforeAllEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write events starting at 100
	for i := range 3 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Request cursor=50 (before all events)
	// Floor lookup finds nothing, so vsCursor is nil (start from beginning)
	vsCursor, err := m.GetVersionstampForSeq(ctx, 50)
	require.NoError(t, err)
	require.Empty(t, vsCursor)

	// GetEventsSince with nil cursor starts from beginning
	events, cursor, err := m.GetEventsSince(ctx, vsCursor, 10)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.NotEmpty(t, cursor)

	require.Equal(t, int64(100), events[0].UpstreamSeq)
	require.Equal(t, int64(101), events[1].UpstreamSeq)
	require.Equal(t, int64(102), events[2].UpstreamSeq)
}

func TestGetVersionstampForSeq_CursorAfterAllEvents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write events up to 104
	for i := range 5 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Request cursor=104 (the last event)
	// Should return nothing (no events after 104)
	vsCursor, err := m.GetVersionstampForSeq(ctx, 104)
	require.NoError(t, err)
	require.NotEmpty(t, vsCursor)

	events, cursor, err := m.GetEventsSince(ctx, vsCursor, 10)
	require.NoError(t, err)
	require.Len(t, events, 0)
	require.Empty(t, cursor)

	// Request cursor=200 (way past all events)
	// Floor lookup finds 104, but there's nothing after it
	vsCursor2, err := m.GetVersionstampForSeq(ctx, 200)
	require.NoError(t, err)
	require.NotEmpty(t, vsCursor2)

	events, cursor, err = m.GetEventsSince(ctx, vsCursor2, 10)
	require.NoError(t, err)
	require.Len(t, events, 0)
	require.Empty(t, cursor)
}

func TestGetVersionstampForSeq_EmptyDatabase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Request any cursor on empty database
	vsCursor, err := m.GetVersionstampForSeq(ctx, 100)
	require.NoError(t, err)
	require.Empty(t, vsCursor) // nil since no events exist

	events, cursor, err := m.GetEventsSince(ctx, vsCursor, 10)
	require.NoError(t, err)
	require.Len(t, events, 0)
	require.Empty(t, cursor)
}

func TestGetVersionstampForSeq_Pagination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write 10 events
	for i := range 10 {
		event := &prototypes.FirehoseEvent{
			UpstreamSeq: int64(100 + i),
			EventType:   "#commit",
			RawEvent:    []byte("event data"),
		}
		err := m.WriteEvent(ctx, event)
		require.NoError(t, err)
	}

	// Get versionstamp for seq 99 (before all events, returns nil)
	vsCursor, err := m.GetVersionstampForSeq(ctx, 99)
	require.NoError(t, err)
	require.Empty(t, vsCursor) // nil since cursor is before all events

	// Get first 3 events (starting from beginning since vsCursor is nil)
	events, cursor, err := m.GetEventsSince(ctx, vsCursor, 3)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.NotEmpty(t, cursor)

	require.Equal(t, int64(100), events[0].UpstreamSeq)
	require.Equal(t, int64(102), events[2].UpstreamSeq)

	// Use returned cursor with GetEventsSince for continuation
	events, cursor, err = m.GetEventsSince(ctx, cursor, 3)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.NotEmpty(t, cursor)

	require.Equal(t, int64(103), events[0].UpstreamSeq)
	require.Equal(t, int64(105), events[2].UpstreamSeq)
}

func TestCursorIndex_WrittenCorrectly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Write an event
	event := &prototypes.FirehoseEvent{
		UpstreamSeq: 12345,
		EventType:   "#commit",
		RawEvent:    []byte("test"),
	}
	err := m.WriteEvent(ctx, event)
	require.NoError(t, err)

	// Get the versionstamp via GetEventsSince
	events, vsCursor, err := m.GetEventsSince(ctx, nil, 1)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Len(t, vsCursor, versionstampLength)

	// Now use GetVersionstampForSeq with the exact seq
	// Should return versionstamp for event 12345
	indexCursor, err := m.GetVersionstampForSeq(ctx, 12345)
	require.NoError(t, err)
	require.NotEmpty(t, indexCursor)

	// Events after this cursor should be empty (nothing after 12345)
	events, _, err = m.GetEventsSince(ctx, indexCursor, 10)
	require.NoError(t, err)
	require.Len(t, events, 0)

	// Ask for versionstamp at 12344 (before our event)
	indexCursor2, err := m.GetVersionstampForSeq(ctx, 12344)
	require.NoError(t, err)
	require.Empty(t, indexCursor2) // nil since no events <= 12344

	// Getting events from nil cursor should return our event
	events, returnedCursor, err := m.GetEventsSince(ctx, indexCursor2, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(12345), events[0].UpstreamSeq)

	// The cursor returned should be the same versionstamp as from direct lookup
	require.Equal(t, vsCursor, returnedCursor)
}

func TestWriteEvent_LargeEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	m := testModels(t)

	// Create a large event that requires multiple chunks (>90KB default chunk size)
	// Use random data so it doesn't compress too well
	largeData := make([]byte, 3*1024*1024) // 3MB
	_, err := rand.Read(largeData)
	require.NoError(t, err)

	event := &prototypes.FirehoseEvent{
		UpstreamSeq: 12345,
		EventType:   "#commit",
		RawEvent:    largeData,
	}

	err = m.WriteEvent(ctx, event)
	require.NoError(t, err)

	// Verify we can read it back correctly
	events, cursor, err := m.GetEventsSince(ctx, nil, 10)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.NotEmpty(t, cursor)

	require.Equal(t, int64(12345), events[0].UpstreamSeq)
	require.Equal(t, "#commit", events[0].EventType)
	require.Equal(t, largeData, events[0].RawEvent)

	// Verify GetLatestUpstreamSeq also works with chunked events
	seq, err := m.GetLatestUpstreamSeq(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(12345), seq)
}
