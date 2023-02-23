package events

import (
	"context"
	"sync"
)

type EventPersistence interface {
	Persist(ctx context.Context, e *RepoStreamEvent) error
	Playback(ctx context.Context, since int64, cb func(*RepoStreamEvent) error) error
}

// MemPersister is the most naive implementation of event persistence
// ill do better later
type MemPersister struct {
	buf []*RepoStreamEvent
	lk  sync.Mutex
	seq int64
}

func NewMemPersister() *MemPersister {
	return &MemPersister{}
}

func (mp *MemPersister) Persist(ctx context.Context, e *RepoStreamEvent) error {
	mp.lk.Lock()
	defer mp.lk.Unlock()
	mp.seq++
	switch {
	case e.Append != nil:
		e.Append.Seq = mp.seq
	default:
		panic("no event in persist call")
	}
	mp.buf = append(mp.buf, e)

	return nil
}

func (mp *MemPersister) Playback(ctx context.Context, since int64, cb func(*RepoStreamEvent) error) error {
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
