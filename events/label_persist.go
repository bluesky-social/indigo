package events

import (
	"context"
	"sync"
)

type LabelEventPersistence interface {
	Persist(ctx context.Context, e *LabelStreamEvent) error
	Playback(ctx context.Context, since int64, cb func(*LabelStreamEvent) error) error
}

// MemLabelPersister is the most naive implementation of event persistence
// ill do better later
type MemLabelPersister struct {
	buf []*LabelStreamEvent
	lk  sync.Mutex
	seq int64
}

func NewMemLabelPersister() *MemLabelPersister {
	return &MemLabelPersister{}
}

func (mp *MemLabelPersister) Persist(ctx context.Context, e *LabelStreamEvent) error {
	mp.lk.Lock()
	defer mp.lk.Unlock()
	mp.seq++
	switch {
	case e.Batch != nil:
		e.Batch.Seq = mp.seq
	default:
		panic("no event in persist call")
	}
	mp.buf = append(mp.buf, e)

	return nil
}

func (mp *MemLabelPersister) Playback(ctx context.Context, since int64, cb func(*LabelStreamEvent) error) error {
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
