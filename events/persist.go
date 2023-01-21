package events

import "sync"

type EventPersistence interface {
	Persist(e *Event)
	Playback(since int64, cb func(*Event) error) error
}

// MemPersister is the most naive implementation of event persistence
// ill do better later
type MemPersister struct {
	buf []*Event
	lk  sync.Mutex
	seq int64
}

func NewMemPersister() *MemPersister {
	return &MemPersister{}
}

func (mp *MemPersister) Persist(e *Event) {
	mp.lk.Lock()
	defer mp.lk.Unlock()
	mp.seq++
	e.Seq = mp.seq
	mp.buf = append(mp.buf, e)
}

func (mp *MemPersister) Playback(since int64, cb func(*Event) error) error {
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
