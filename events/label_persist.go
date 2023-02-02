package events

import "sync"

type LabelEventPersistence interface {
	Persist(e *LabelEvent)
	Playback(since int64, cb func(*LabelEvent) error) error
}

// MemLabelPersister is the most naive implementation of event persistence
// ill do better later
type MemLabelPersister struct {
	buf []*LabelEvent
	lk  sync.Mutex
	seq int64
}

func NewMemLabelPersister() *MemLabelPersister {
	return &MemLabelPersister{}
}

func (mp *MemLabelPersister) Persist(e *LabelEvent) {
	mp.lk.Lock()
	defer mp.lk.Unlock()
	mp.seq++
	e.Seq = mp.seq
	mp.buf = append(mp.buf, e)
}

func (mp *MemLabelPersister) Playback(since int64, cb func(*LabelEvent) error) error {
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
