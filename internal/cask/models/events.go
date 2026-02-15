package models

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/bluesky-social/indigo/pkg/types"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
)

const (
	// versionstampLength is the length of an FDB versionstamp (10 bytes)
	// 8 bytes for commit version + 2 bytes for batch order
	versionstampLength = 10

	// cursorLength is the length of a cursor: versionstamp + event index byte.
	// Within a single FDB transaction, all versionstamped operations receive the
	// same 10-byte versionstamp. The event index byte differentiates multiple
	// events written in the same batch transaction.
	cursorLength = versionstampLength + 1
)

// WriteEvent writes a firehose event to FoundationDB with a versionstamped key.
// The event will be assigned a sequence number by FDB's versionstamp at commit time.
// Large events are automatically chunked and optionally compressed.
// It also writes a secondary index mapping upstream_seq -> versionstamp for O(1) cursor lookup.
func (m *Models) WriteEvent(ctx context.Context, event *types.FirehoseEvent) (err error) {
	_, span, done := foundation.Observe(ctx, m.db, "WriteEvent")
	defer func() { done(err) }()

	span.SetAttributes(
		attribute.Int64("upstream_seq", event.UpstreamSeq),
		attribute.String("event_type", event.EventType),
	)

	eventBuf, err := proto.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to proto marshal firehose event: %w", err)
	}

	chunks, err := m.db.Chunker.ChunkData(eventBuf)
	if err != nil {
		return fmt.Errorf("failed to chunk event data: %w", err)
	}

	cursorIndexKey := m.cursorIndex.Pack(tuple.Tuple{event.UpstreamSeq})

	_, err = foundation.Transaction(m.db, func(tx fdb.Transaction) (any, error) {
		m.writeEventToTx(tx, chunks, cursorIndexKey, 0)
		return nil, nil
	})

	return
}

// WriteEventBatch writes multiple firehose events in a single FDB transaction.
// Each event is assigned a unique event index byte appended after the versionstamp,
// so all events in the batch get distinct keys despite sharing the same versionstamp.
// One OTEL span covers the entire batch.
// maxEventsPerBatch is the maximum number of events that can be written in a single
// FDB transaction. Each event is distinguished by a 1-byte event index, so the
// maximum is 255 (byte range 0-254). This must be kept in sync with the consumer's
// maxBatchSize constant.
const maxEventsPerBatch = 255

func (m *Models) WriteEventBatch(ctx context.Context, events []*types.FirehoseEvent) (err error) {
	if len(events) == 0 {
		return nil
	}
	if len(events) > maxEventsPerBatch {
		return fmt.Errorf("batch size %d exceeds maximum of %d events per transaction", len(events), maxEventsPerBatch)
	}

	_, span, done := foundation.Observe(ctx, m.db, "WriteEventBatch")
	defer func() { done(err) }()

	span.SetAttributes(attribute.Int("batch_size", len(events)))

	// Pre-marshal and chunk all events outside the transaction
	type preparedEvent struct {
		chunks    [][]byte
		cursorKey []byte
	}

	prepared := make([]preparedEvent, len(events))
	for i, event := range events {
		eventBuf, err := proto.Marshal(event)
		if err != nil {
			return fmt.Errorf("failed to proto marshal firehose event %d: %w", i, err)
		}

		chunks, err := m.db.Chunker.ChunkData(eventBuf)
		if err != nil {
			return fmt.Errorf("failed to chunk event data %d: %w", i, err)
		}

		prepared[i] = preparedEvent{
			chunks:    chunks,
			cursorKey: m.cursorIndex.Pack(tuple.Tuple{event.UpstreamSeq}),
		}
	}

	_, err = foundation.Transaction(m.db, func(tx fdb.Transaction) (any, error) {
		for i, pe := range prepared {
			m.writeEventToTx(tx, pe.chunks, pe.cursorKey, byte(i))
		}
		return nil, nil
	})

	return
}

// writeEventToTx writes a single event's chunks and cursor index to the given transaction.
// The eventIndex byte differentiates events that share the same versionstamp within a batch.
//
// Event key structure: [prefix][versionstamp (10)][event_index (1)][chunk_index (1)][offset (4)]
// Cursor index value:  [versionstamp (10)][event_index (1)][offset (4)]
func (m *Models) writeEventToTx(tx fdb.Transaction, chunks [][]byte, cursorIndexKey []byte, eventIndex byte) {
	prefix := m.events.Bytes()

	for i, chunk := range chunks {
		// Key: [prefix][VS placeholder(10)][event_index(1)][chunk_index(1)][offset(4)]
		eventKey := make([]byte, 0, len(prefix)+16)
		eventKey = append(eventKey, prefix...)
		eventKey = append(eventKey, make([]byte, 10)...) // versionstamp placeholder
		eventKey = append(eventKey, eventIndex)          // event index within batch
		eventKey = append(eventKey, byte(i))             // chunk index
		eventKey = binary.LittleEndian.AppendUint32(eventKey, uint32(len(prefix)))

		tx.SetVersionstampedKey(fdb.Key(eventKey), chunk)
	}

	// Cursor index value: [VS placeholder(10)][event_index(1)][offset(4) = 0]
	// After commit, FDB strips the 4-byte offset and stores 11 bytes.
	cursorValue := make([]byte, 15)
	cursorValue[10] = eventIndex
	tx.SetVersionstampedValue(fdb.Key(cursorIndexKey), cursorValue)
}

// GetLatestUpstreamSeq returns the upstream sequence number from the most recent
// event stored in FDB. This is used to resume consuming from the upstream firehose
// after a restart. Returns 0 if no events exist. We use the term "upstream" to refer
// to the upstream firehose server to which this cask instance points.
func (m *Models) GetLatestUpstreamSeq(ctx context.Context) (seq int64, err error) {
	_, span, done := foundation.Observe(ctx, m.db, "GetLatestUpstreamSeq")
	defer func() { done(err) }()

	var eventData []byte
	eventData, err = foundation.ReadTransaction(m.db, func(tx fdb.ReadTransaction) ([]byte, error) {
		prefixLen := len(m.events.Bytes())

		// Read the last keys by doing a reverse range scan
		// We need to read all chunks for the latest event
		start := m.events.FDBKey()
		end := fdb.Key(append(m.events.Bytes(), 0xFF))
		keyRange := fdb.KeyRange{Begin: start, End: end}

		// Read more than 1 to handle multi-chunk events
		iter := tx.GetRange(keyRange, fdb.RangeOptions{Limit: 256, Reverse: true}).Iterator()

		var chunks [][]byte
		var targetCursor []byte

		for iter.Advance() {
			kv, err := iter.Get()
			if err != nil {
				return nil, fmt.Errorf("failed to get event: %w", err)
			}

			// Extract cursor from key: [prefix][versionstamp (10)][event_index (1)][chunk_index (1)]
			if len(kv.Key) < prefixLen+cursorLength+1 {
				continue
			}
			cursor := kv.Key[prefixLen : prefixLen+cursorLength]

			if targetCursor == nil {
				targetCursor = cursor
			} else if !slices.Equal(cursor, targetCursor) {
				// We've moved to a different event, stop
				break
			}

			// Prepend chunk (since we're reading in reverse order)
			chunks = append([][]byte{kv.Value}, chunks...)
		}

		if len(chunks) == 0 {
			return nil, nil // no events yet
		}

		return m.db.Chunker.ReassembleChunks(chunks)
	})
	if err != nil {
		return 0, err
	}
	if eventData == nil {
		return 0, nil // no events yet
	}

	var event types.FirehoseEvent
	if err := proto.Unmarshal(eventData, &event); err != nil {
		return 0, fmt.Errorf("failed to unmarshal latest event: %w", err)
	}

	seq = event.UpstreamSeq
	span.SetAttributes(attribute.Int64("latest_seq", seq))
	return
}

// GetLatestVersionstamp returns the cursor of the most recent event stored in FDB.
// This is used to start subscribers at the "tip" when no cursor is provided.
// Returns nil if no events exist.
func (m *Models) GetLatestVersionstamp(ctx context.Context) (cursor []byte, err error) {
	_, span, done := foundation.Observe(ctx, m.db, "GetLatestVersionstamp")
	defer func() { done(err) }()

	cursor, err = foundation.ReadTransaction(m.db, func(tx fdb.ReadTransaction) ([]byte, error) {
		// Read the last key by doing a reverse range scan with limit 1
		start := m.events.FDBKey()
		end := fdb.Key(append(m.events.Bytes(), 0xFF))
		keyRange := fdb.KeyRange{Begin: start, End: end}

		// Reverse=true gives us the last key first
		iter := tx.GetRange(keyRange, fdb.RangeOptions{Limit: 1, Reverse: true}).Iterator()

		if !iter.Advance() {
			return nil, nil // no events yet
		}

		kv, err := iter.Get()
		if err != nil {
			return nil, fmt.Errorf("failed to get latest event: %w", err)
		}

		// Extract cursor from key: [prefix][versionstamp (10)][event_index (1)][chunk_index (1)]
		prefixLen := len(m.events.Bytes())
		if len(kv.Key) < prefixLen+cursorLength+1 {
			return nil, nil // malformed key
		}
		return kv.Key[prefixLen : prefixLen+cursorLength], nil
	})

	span.SetAttributes(attribute.String("cursor", hex.EncodeToString(cursor)))
	return
}

// GetEventsSince retrieves events starting from (but not including) the given cursor.
// If cursor is nil, retrieves from the beginning.
// Events may be chunked, so this function reassembles chunks before returning.
// Returns events and the cursor for the last event returned.
func (m *Models) GetEventsSince(ctx context.Context, cursor []byte, limit int) (events []*types.FirehoseEvent, nextCursor []byte, err error) {
	_, span, done := foundation.Observe(ctx, m.db, "GetEventsSince")
	defer func() { done(err) }()

	span.SetAttributes(
		attribute.Int("limit", limit),
		attribute.String("cursor", hex.EncodeToString(cursor)),
	)

	type result struct {
		events     []*types.FirehoseEvent
		nextCursor []byte
	}

	var res *result
	res, err = foundation.ReadTransaction(m.db, func(tx fdb.ReadTransaction) (*result, error) {
		prefixLen := len(m.events.Bytes())

		// Determine start key
		var startKey fdb.Key
		if len(cursor) == 0 {
			// Start from beginning of events subspace
			startKey = m.events.FDBKey()
		} else {
			// Start after the cursor (exclusive)
			// Cursor is versionstamp+event_index bytes, append 0xFF to skip all chunks of that event
			startKey = fdb.Key(append(m.events.Bytes(), cursor...))
			startKey = append(startKey, 0xFF)
		}

		// End key is the end of the events subspace
		endKey := fdb.Key(append(m.events.Bytes(), 0xFF))

		// Read more keys than limit since events may have multiple chunks
		// We'll stop when we have enough complete events or hit byte limit
		// Use StreamingModeWantAll to hint that we want all data upfront (reduces round-trips)
		rng := fdb.KeyRange{Begin: startKey, End: endKey}
		iter := tx.GetRange(rng, fdb.RangeOptions{
			Mode: fdb.StreamingModeWantAll,
		}).Iterator()

		var events []*types.FirehoseEvent
		var lastCursor []byte
		var currentCursor []byte
		var currentChunks [][]byte
		var accumulatedBytes int

		for iter.Advance() {
			kv, err := iter.Get()
			if err != nil {
				return nil, fmt.Errorf("failed to get event: %w", err)
			}

			accumulatedBytes += len(kv.Value)

			// Extract cursor from key: [prefix][versionstamp (10)][event_index (1)][chunk_index (1)]
			if len(kv.Key) < prefixLen+cursorLength+1 {
				continue // malformed key
			}
			eventCursor := kv.Key[prefixLen : prefixLen+cursorLength]

			// Check if this is a new event (different cursor)
			if currentCursor != nil && !slices.Equal(eventCursor, currentCursor) {
				// Reassemble and process the previous event
				eventData, err := m.db.Chunker.ReassembleChunks(currentChunks)
				if err != nil {
					return nil, fmt.Errorf("failed to reassemble chunks: %w", err)
				}

				var event types.FirehoseEvent
				if err := proto.Unmarshal(eventData, &event); err != nil {
					return nil, fmt.Errorf("failed to unmarshal event: %w", err)
				}

				events = append(events, &event)
				lastCursor = currentCursor

				// Check limits
				if len(events) >= limit || accumulatedBytes >= m.db.Chunker.MaxBatchBytes() {
					return &result{events: events, nextCursor: lastCursor}, nil
				}

				// Reset for new event
				currentChunks = nil
			}

			currentCursor = eventCursor
			currentChunks = append(currentChunks, kv.Value)
		}

		// Process the last event if we have chunks
		if len(currentChunks) > 0 {
			eventData, err := m.db.Chunker.ReassembleChunks(currentChunks)
			if err != nil {
				return nil, fmt.Errorf("failed to reassemble chunks: %w", err)
			}

			var event types.FirehoseEvent
			if err := proto.Unmarshal(eventData, &event); err != nil {
				return nil, fmt.Errorf("failed to unmarshal event: %w", err)
			}

			events = append(events, &event)
			lastCursor = currentCursor
		}

		return &result{events: events, nextCursor: lastCursor}, nil
	})
	if err != nil {
		return nil, nil, err
	}

	events = res.events
	nextCursor = res.nextCursor

	span.SetAttributes(
		attribute.Int("events_returned", len(events)),
		attribute.String("next_cursor", hex.EncodeToString(nextCursor)),
	)

	return
}

// GetVersionstampForSeq looks up the versionstamp cursor for the given upstream sequence number.
// This uses a floor lookup - it finds the greatest seq <= the requested value.
// Returns nil if no events exist with seq <= the requested value (caller should start from beginning).
// This is used to convert an upstream firehose cursor to an internal versionstamp cursor
// for efficient streaming.
func (m *Models) GetVersionstampForSeq(ctx context.Context, seq int64) ([]byte, error) {
	return m.getVersionstampForSeq(ctx, seq)
}

// getVersionstampForSeq looks up the versionstamp for the greatest upstream sequence
// number that is <= the requested seq (floor lookup). This handles gaps in sequence
// numbers - if the client requests cursor=1002 but we only have events 1000, 1005, 1010,
// we return the versionstamp for seq=1000 so we can stream events after it.
// Returns nil if no events exist with seq <= the requested value.
func (m *Models) getVersionstampForSeq(ctx context.Context, seq int64) ([]byte, error) {
	_, span, done := foundation.Observe(ctx, m.db, "getVersionstampForSeq")
	var err error
	defer func() { done(err) }()

	span.SetAttributes(attribute.Int64("requested_seq", seq))

	var cursor []byte
	cursor, err = foundation.ReadTransaction(m.db, func(tx fdb.ReadTransaction) ([]byte, error) {
		// Do a floor lookup: find the greatest key <= the requested seq.
		// We scan from the beginning of the index up to (and including) the requested seq,
		// in reverse order with limit 1.
		startKey := m.cursorIndex.FDBKey()
		// End key is exclusive, so we need seq+1 to include seq itself
		endKey := m.cursorIndex.Pack(tuple.Tuple{seq + 1})

		rng := fdb.KeyRange{Begin: startKey, End: endKey}
		iter := tx.GetRange(rng, fdb.RangeOptions{Limit: 1, Reverse: true}).Iterator()

		if !iter.Advance() {
			return nil, nil // no events with seq <= requested
		}

		kv, err := iter.Get()
		if err != nil {
			return nil, fmt.Errorf("failed to get cursor index entry: %w", err)
		}

		// The value is an 11-byte cursor (versionstamp + event_index;
		// the 4-byte offset suffix was stripped by FDB at commit time)
		if len(kv.Value) < cursorLength {
			return nil, fmt.Errorf("invalid cursor length in cursor index: %d", len(kv.Value))
		}
		return kv.Value[:cursorLength], nil
	})

	span.SetAttributes(attribute.String("cursor", hex.EncodeToString(cursor)))

	return cursor, err
}
