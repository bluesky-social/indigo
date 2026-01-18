package models

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/bluesky-social/indigo/pkg/prototypes"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
)

const (
	// versionstampLength is the length of an FDB versionstamp (10 bytes)
	// 8 bytes for commit version + 2 bytes for batch order
	versionstampLength = 10
)

// WriteEvent writes a firehose event to FoundationDB with a versionstamped key.
// The event will be assigned a sequence number by FDB's versionstamp at commit time.
// It also writes a secondary index mapping upstream_seq -> versionstamp for O(1) cursor lookup.
func (m *Models) WriteEvent(ctx context.Context, event *prototypes.FirehoseEvent) (err error) {
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

	// Build a key with a versionstamp placeholder. When this transaction commits,
	// FDB will automatically replace the placeholder with a unique, monotonically
	// increasing 10-byte versionstamp. This gives us globally-ordered keys without
	// requiring coordination.
	//
	// The final key structure will be: [events_prefix][versionstamp]
	// This means iterating over the events subspace returns events in commit order.

	// The directory subspace prefix that identifies this as an event key
	prefix := m.events.Bytes()

	// Build the key with a versionstamp placeholder. The placeholder is 14 bytes:
	// 10 zero bytes (where FDB automatically writes the versionstamp) followed by
	// a 4-byte little-endian offset. The offset tells FDB where in the final key
	// the versionstamp should be placed, measured from the start of the key.
	//
	// Since we want the versionstamp right after the prefix, `offset = len(prefix)`.
	//
	// We pre-allocate the key slice to avoid accidentally mutating the prefix's
	// backing array if it happens to have spare capacity.
	eventKey := make([]byte, 0, len(prefix)+14)
	eventKey = append(eventKey, prefix...)
	eventKey = append(eventKey, make([]byte, 10)...) // versionstamp placeholder (10 zero bytes)
	eventKey = binary.LittleEndian.AppendUint32(eventKey, uint32(len(prefix)))

	// Build the cursor index key: [cursor_index_prefix][upstream_seq as big-endian int64]
	// Big-endian ensures lexicographic ordering matches numeric ordering.
	cursorIndexKey := m.cursorIndex.Pack(tuple.Tuple{event.UpstreamSeq})

	// Build the cursor index value with a versionstamp placeholder.
	// SetVersionstampedValue expects: [10 zero bytes][4-byte little-endian offset]
	// The offset is 0 because the versionstamp should be at the start of the value.
	cursorIndexValue := make([]byte, 14)
	// First 10 bytes are already zero (versionstamp placeholder)
	// Last 4 bytes are offset (0 in little-endian, which is also all zeros)
	// So the entire 14-byte slice is already correct as zero-initialized

	_, err = foundation.Transaction(m.db, func(tx fdb.Transaction) (any, error) {
		// Write the event with a versionstamped key. At commit time, FDB atomically
		// assigns the versionstamp, ensuring this event gets a unique sequence number.
		tx.SetVersionstampedKey(fdb.Key(eventKey), eventBuf)

		// Write the cursor index with the same versionstamp as the value.
		// This allows O(1) lookup: given an upstream_seq, we can find the versionstamp
		// and then use GetEventsSince for efficient continuation.
		tx.SetVersionstampedValue(cursorIndexKey, cursorIndexValue)

		return nil, nil
	})

	return
}

// GetLatestUpstreamSeq returns the upstream sequence number from the most recent
// event stored in FDB. This is used to resume consuming from the upstream firehose
// after a restart. Returns 0 if no events exist. We use the term "upstream" to refer
// to the upstream firehose server to which this cask instance points.
func (m *Models) GetLatestUpstreamSeq(ctx context.Context) (seq int64, err error) {
	_, span, done := foundation.Observe(ctx, m.db, "GetLatestUpstreamSeq")
	defer func() { done(err) }()

	var seqBuf []byte
	seqBuf, err = foundation.ReadTransaction(m.db, func(tx fdb.ReadTransaction) ([]byte, error) {
		// Read the last event by doing a reverse range scan with limit 1
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

		return kv.Value, nil
	})
	if err != nil {
		return 0, err
	}
	if seqBuf == nil {
		return 0, err // no events yet
	}

	var event prototypes.FirehoseEvent
	if err := proto.Unmarshal(seqBuf, &event); err != nil {
		return 0, fmt.Errorf("failed to unmarshal latest event: %w", err)
	}

	seq = event.UpstreamSeq
	span.SetAttributes(attribute.Int64("latest_seq", seq))
	return
}

// GetLatestVersionstamp returns the versionstamp of the most recent event stored in FDB.
// This is used to start subscribers at the "tip" when no cursor is provided.
// Returns nil if no events exist.
func (m *Models) GetLatestVersionstamp(ctx context.Context) (cursor []byte, err error) {
	_, span, done := foundation.Observe(ctx, m.db, "GetLatestVersionstamp")
	defer func() { done(err) }()

	cursor, err = foundation.ReadTransaction(m.db, func(tx fdb.ReadTransaction) ([]byte, error) {
		// Read the last event by doing a reverse range scan with limit 1
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

		// Extract versionstamp from key (after prefix)
		prefixLen := len(m.events.Bytes())
		if len(kv.Key) < prefixLen+versionstampLength {
			return nil, nil // malformed key
		}
		return kv.Key[prefixLen : prefixLen+versionstampLength], nil
	})

	span.SetAttributes(attribute.String("cursor", string(cursor)))
	return
}

// GetEventsSince retrieves events starting from (but not including) the given cursor.
// If cursor is nil, retrieves from the beginning.
// Returns events and the cursor for the last event returned.
func (m *Models) GetEventsSince(ctx context.Context, cursor []byte, limit int) (events []*prototypes.FirehoseEvent, nextCursor []byte, err error) {
	_, span, done := foundation.Observe(ctx, m.db, "GetEventsSince")
	defer func() { done(err) }()

	span.SetAttributes(
		attribute.Int("limit", limit),
		attribute.String("cursor", string(cursor)),
	)

	type result struct {
		events     []*prototypes.FirehoseEvent
		nextCursor []byte
	}

	var res *result
	res, err = foundation.ReadTransaction(m.db, func(tx fdb.ReadTransaction) (*result, error) {
		// determine start key
		var startKey fdb.Key
		if len(cursor) == 0 {
			// start from beginning of events subspace
			startKey = m.events.FDBKey()
		} else {
			// start after the cursor (exclusive)
			// cursor is the raw versionstamp bytes
			startKey = fdb.Key(append(m.events.Bytes(), cursor...))
			startKey = append(startKey, 0x00) // make it exclusive by adding a byte
		}

		// end key is the end of the events subspace
		endKey := fdb.Key(append(m.events.Bytes(), 0xFF))

		rng := fdb.KeyRange{Begin: startKey, End: endKey}
		iter := tx.GetRange(rng, fdb.RangeOptions{Limit: limit}).Iterator()

		var events []*prototypes.FirehoseEvent
		var lastKey []byte

		for iter.Advance() {
			kv, err := iter.Get()
			if err != nil {
				return nil, fmt.Errorf("failed to get event: %w", err)
			}

			// extract versionstamp from key (after prefix)
			prefixLen := len(m.events.Bytes())
			if len(kv.Key) < prefixLen+versionstampLength {
				continue // malformed key
			}
			versionstamp := kv.Key[prefixLen : prefixLen+versionstampLength]

			// parse event
			var event prototypes.FirehoseEvent
			if err := proto.Unmarshal(kv.Value, &event); err != nil {
				return nil, fmt.Errorf("failed to unmarshal event: %w", err)
			}

			events = append(events, &event)
			lastKey = versionstamp
		}

		return &result{events: events, nextCursor: lastKey}, nil
	})
	if err != nil {
		return nil, nil, err
	}

	events = res.events
	nextCursor = res.nextCursor

	span.SetAttributes(
		attribute.Int("events_returned", len(events)),
		attribute.String("next_cursor", string(nextCursor)),
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

	var versionstamp []byte
	versionstamp, err = foundation.ReadTransaction(m.db, func(tx fdb.ReadTransaction) ([]byte, error) {
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

		// The value is a 10-byte versionstamp (the 4-byte offset suffix was stripped by FDB)
		if len(kv.Value) < versionstampLength {
			return nil, fmt.Errorf("invalid versionstamp length in cursor index: %d", len(kv.Value))
		}
		return kv.Value[:versionstampLength], nil
	})

	span.SetAttributes(attribute.String("versionstamp", string(versionstamp)))

	return versionstamp, err
}
