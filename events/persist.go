package events

import (
	"context"
	"sync"
)

// Note that this interface looks generic, but some persisters might only work with RepoAppend or LabelBatch
type EventPersistence interface {
	Persist(ctx context.Context, e *XRPCStreamEvent) error
	Playback(ctx context.Context, since int64, cb func(*XRPCStreamEvent) error) error
}

// MemPersister is the most naive implementation of event persistence
// This EventPersistence option works fine with all event types
// ill do better later
type MemPersister struct {
	buf []*XRPCStreamEvent
	lk  sync.Mutex
	seq int64
}

func NewMemPersister() *MemPersister {
	return &MemPersister{}
}

func (mp *MemPersister) Persist(ctx context.Context, e *XRPCStreamEvent) error {
	mp.lk.Lock()
	defer mp.lk.Unlock()
	mp.seq++
	switch {
	case e.RepoCommit != nil:
		e.RepoCommit.Seq = mp.seq
	case e.RepoHandle != nil:
		e.RepoHandle.Seq = mp.seq
	case e.RepoMigrate != nil:
		e.RepoMigrate.Seq = mp.seq
	case e.RepoTombstone != nil:
		e.RepoTombstone.Seq = mp.seq
	case e.LabelBatch != nil:
		e.LabelBatch.Seq = mp.seq
	default:
		panic("no event in persist call")
	}
	mp.buf = append(mp.buf, e)

	return nil
}

func (mp *MemPersister) Playback(ctx context.Context, since int64, cb func(*XRPCStreamEvent) error) error {
	mp.lk.Lock()
	l := len(mp.buf)
	mp.lk.Unlock()

	if since >= int64(l) {
		return nil
	}

	// TODO: abusing the fact that buf[0].seq is currently always 1
	for _, e := range mp.buf[since:l] {
		if err := cb(e); err != nil {
			return err
		}
	}

	return nil
}
