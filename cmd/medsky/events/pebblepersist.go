package events

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/models"
	"github.com/cockroachdb/pebble"
)

type PebblePersist struct {
	broadcast func(*XRPCStreamEvent)
	db        *pebble.DB

	prevSeq      int64
	prevSeqExtra uint32

	cancel func()

	options PebblePersistOptions
}

type PebblePersistOptions struct {
	// path where pebble will create a directory full of files
	DbPath string

	// Throw away posts older than some time ago
	PersistDuration time.Duration

	// Throw away old posts every so often
	GCPeriod time.Duration

	// MaxBytes is what we _try_ to keep disk usage under
	MaxBytes uint64
}

var DefaultPebblePersistOptions = PebblePersistOptions{
	PersistDuration: time.Minute * 20,
	GCPeriod:        time.Minute * 5,
	MaxBytes:        1024 * 1024 * 1024, // 1 GiB
}

// Create a new EventPersistence which stores data in pebbledb
// nil opts is ok
func NewPebblePersistance(opts *PebblePersistOptions) (*PebblePersist, error) {
	if opts == nil {
		opts = &DefaultPebblePersistOptions
	}
	db, err := pebble.Open(opts.DbPath, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", opts.DbPath, err)
	}
	pp := new(PebblePersist)
	pp.options = *opts
	pp.db = db
	return pp, nil
}

func setKeySeqMillis(key []byte, seq, millis int64) {
	binary.BigEndian.PutUint64(key[:8], uint64(seq))
	binary.BigEndian.PutUint64(key[8:16], uint64(millis))
}

func (pp *PebblePersist) Persist(ctx context.Context, e *XRPCStreamEvent) error {
	err := e.Preserialize()
	if err != nil {
		return err
	}
	blob := e.Preserialized

	seq := e.Sequence()
	nowMillis := time.Now().UnixMilli()

	if seq < 0 {
		// persist with longer key {prev 8 byte key}{time}{int32 extra counter}
		pp.prevSeqExtra++
		var key [20]byte
		setKeySeqMillis(key[:], seq, nowMillis)
		binary.BigEndian.PutUint32(key[16:], pp.prevSeqExtra)

		err = pp.db.Set(key[:], blob, pebble.Sync)
	} else {
		pp.prevSeq = seq
		pp.prevSeqExtra = 0
		var key [16]byte
		setKeySeqMillis(key[:], seq, nowMillis)

		err = pp.db.Set(key[:], blob, pebble.Sync)
	}

	if err != nil {
		return err
	}
	pp.broadcast(e)

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

var ErrNoLast = errors.New("no last event")

func (pp *PebblePersist) GetLast(ctx context.Context) (seq, millis int64, evt *XRPCStreamEvent, err error) {
	iter, err := pp.db.NewIterWithContext(ctx, &pebble.IterOptions{})
	if err != nil {
		return 0, 0, nil, err
	}
	ok := iter.Last()
	if !ok {
		return 0, 0, nil, ErrNoLast
	}
	evt, err = eventFromPebbleIter(iter)
	keyblob := iter.Key()
	seq = int64(binary.BigEndian.Uint64(keyblob[:8]))
	millis = int64(binary.BigEndian.Uint64(keyblob[8:16]))
	return seq, millis, evt, nil
}

// example;
// ```
// pp := NewPebblePersistance("/tmp/foo.pebble")
// go pp.GCThread(context.Background(), 48 * time.Hour, 5 * time.Minute)
// ```
func (pp *PebblePersist) GCThread(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	pp.cancel = cancel
	ticker := time.NewTicker(pp.options.GCPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			err := pp.GarbageCollect(ctx)
			if err != nil {
				log.Error("GC err", "err", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

var zeroKey [16]byte
var ffffKey [16]byte

func init() {
	setKeySeqMillis(zeroKey[:], 0, 0)
	for i := range ffffKey {
		ffffKey[i] = 0xff
	}
}

func (pp *PebblePersist) GarbageCollect(ctx context.Context) error {
	nowMillis := time.Now().UnixMilli()
	expired := nowMillis - pp.options.PersistDuration.Milliseconds()
	iter, err := pp.db.NewIterWithContext(ctx, &pebble.IterOptions{})
	if err != nil {
		return err
	}
	defer iter.Close()
	// scan keys to find last expired, then delete range
	var seq int64 = int64(-1)
	var lastKeyTime int64
	for iter.First(); iter.Valid(); iter.Next() {
		keyblob := iter.Key()

		keyTime := int64(binary.BigEndian.Uint64(keyblob[8:16]))
		if keyTime <= expired {
			lastKeyTime = keyTime
			seq = int64(binary.BigEndian.Uint64(keyblob[:8]))
		} else {
			break
		}
	}

	// TODO: use pp.options.MaxBytes

	sizeBefore, _ := pp.db.EstimateDiskUsage(zeroKey[:], ffffKey[:])
	if seq == -1 {
		// nothing to delete
		log.Info("pebble gc nop", "size", sizeBefore)
		return nil
	}
	var key [16]byte
	setKeySeqMillis(key[:], seq, lastKeyTime)
	log.Info("pebble gc start", "to", hex.EncodeToString(key[:]))
	err = pp.db.DeleteRange(zeroKey[:], key[:], pebble.Sync)
	if err != nil {
		return err
	}
	sizeAfter, _ := pp.db.EstimateDiskUsage(zeroKey[:], ffffKey[:])
	log.Info("pebble gc", "before", sizeBefore, "after", sizeAfter)
	start := time.Now()
	err = pp.db.Compact(zeroKey[:], key[:], true)
	if err != nil {
		log.Warn("pebble gc compact", "err", err)
	}
	dt := time.Since(start)
	log.Info("pebble gc compact ok", "dt", dt)
	return nil
}
