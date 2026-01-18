package models

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
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
	key := make([]byte, 0, len(prefix)+14)
	key = append(key, prefix...)
	key = append(key, make([]byte, 10)...) // versionstamp placeholder (10 zero bytes)
	key = binary.LittleEndian.AppendUint32(key, uint32(len(prefix)))

	_, err = foundation.Transaction(m.db, func(tx fdb.Transaction) (any, error) {
		// Write the event with a versionstamped key. At commit time, FDB atomically
		// assigns the versionstamp, ensuring this event gets a unique sequence number.
		tx.SetVersionstampedKey(fdb.Key(key), eventBuf)
		return nil, nil
	})

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
		startKey := m.events.FDBKey()
		endKey := fdb.Key(append(m.events.Bytes(), 0xFF))

		rng := fdb.KeyRange{Begin: startKey, End: endKey}
		// Reverse=true gives us the last key first
		iter := tx.GetRange(rng, fdb.RangeOptions{Limit: 1, Reverse: true}).Iterator()

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
