package events

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/bluesky-social/indigo/models"
	"github.com/cockroachdb/pebble"
	"time"
)

type PebblePersist struct {
	broadcast func(*XRPCStreamEvent)
	db        *pebble.DB

	prevSeq      int64
	prevSeqExtra uint32

	cancel func()
}

func NewPebblePersistance(path string) (*PebblePersist, error) {
	db, err := pebble.Open(path, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	pp := new(PebblePersist)
	pp.db = db
	return pp, nil
}

func (pp *PebblePersist) Persist(ctx context.Context, e *XRPCStreamEvent) error {
	err := e.Preserialize()
	if err != nil {
		return err
	}
	blob := e.Preserialized

	seq := e.Sequence()
	nowMillis := uint64(time.Now().UnixMilli())

	if seq < 0 {
		// persist with longer key {prev 8 byte key}{time}{int32 extra counter}
		pp.prevSeqExtra++
		var key [20]byte
		binary.BigEndian.PutUint64(key[:8], uint64(pp.prevSeq))
		binary.BigEndian.PutUint64(key[8:16], nowMillis)
		binary.BigEndian.PutUint32(key[16:], pp.prevSeqExtra)

		err = pp.db.Set(key[:], blob, pebble.Sync)
		return nil
	} else {
		pp.prevSeq = seq
		pp.prevSeqExtra = 0
		var key [16]byte
		binary.BigEndian.PutUint64(key[:8], uint64(seq))
		binary.BigEndian.PutUint64(key[8:16], nowMillis)

		err = pp.db.Set(key[:], blob, pebble.Sync)
	}

	return err
}

func eventFromPebbleIter(iter *pebble.Iterator) (*XRPCStreamEvent, error) {
	blob, err := iter.ValueAndErr()
	if err != nil {
		return nil, err
	}
	br := bytes.NewReader(blob)
	evt := new(XRPCStreamEvent)
	err = evt.Deserialize(br)
	if err != nil {
		return nil, err
	}
	evt.Preserialized = bytes.Clone(blob)
	return evt, nil
}

func (pp *PebblePersist) Playback(ctx context.Context, since int64, cb func(*XRPCStreamEvent) error) error {
	var key [8]byte
	binary.BigEndian.PutUint64(key[:], uint64(since))

	iter, err := pp.db.NewIterWithContext(ctx, &pebble.IterOptions{LowerBound: key[:]})
	if err != nil {
		return err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		evt, err := eventFromPebbleIter(iter)
		if err != nil {
			return err
		}

		err = cb(evt)
		if err != nil {
			return err
		}
	}

	return nil
}
func (pp *PebblePersist) TakeDownRepo(ctx context.Context, usr models.Uid) error {
	// TODO: implement filter on playback to ignore taken-down-repos?
	return nil
}
func (pp *PebblePersist) Flush(context.Context) error {
	return pp.db.Flush()
}
func (pp *PebblePersist) Shutdown(context.Context) error {
	if pp.cancel != nil {
		pp.cancel()
	}
	err := pp.db.Close()
	pp.db = nil
	return err
}

func (pp *PebblePersist) SetEventBroadcaster(broadcast func(*XRPCStreamEvent)) {
	pp.broadcast = broadcast
}

func (pp *PebblePersist) GetLast(ctx context.Context) (*XRPCStreamEvent, error) {
	iter, err := pp.db.NewIterWithContext(ctx, &pebble.IterOptions{})
	if err != nil {
		return nil, err
	}
	ok := iter.Last()
	if !ok {
		return nil, nil
	}
	evt, err := eventFromPebbleIter(iter)
	return evt, nil
}

// example;
// ```
// pp := NewPebblePersistance("/tmp/foo.pebble")
// go pp.GCThread(context.TODO(), 48 * time.Hour, 5 * time.Minute)
// ```
func (pp *PebblePersist) GCThread(ctx context.Context, retention, gcPeriod time.Duration) {
	ctx, cancel := context.WithCancel(ctx)
	pp.cancel = cancel
	ticker := time.NewTicker(gcPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			pp.GarbageCollect(ctx, retention)
		case <-ctx.Done():
			return
		}
	}
}
func (pp *PebblePersist) GarbageCollect(ctx context.Context, retention time.Duration) error {
	nowMillis := time.Now().UnixMilli()
	expired := uint64(nowMillis - retention.Milliseconds())
	iter, err := pp.db.NewIterWithContext(ctx, &pebble.IterOptions{})
	if err != nil {
		return err
	}
	defer iter.Close()
	todel := make(chan []byte, 100)
	go func() {
		for xkey := range todel {
			pp.db.Delete(xkey, nil)
		}
	}()
	defer close(todel)
	for iter.First(); iter.Valid(); iter.Next() {
		keyblob := iter.Key()
		keyTime := binary.BigEndian.Uint64(keyblob[8:16])
		if keyTime < expired {
			todel <- bytes.Clone(keyblob)
		} else {
			break
		}
	}
	return nil
}
