package events

import (
	"context"
	"fmt"
	"sync"

	"github.com/bluesky-social/indigo/models"
)

// Note that this interface looks generic, but some persisters might only work with RepoAppend or LabelLabels
type EventPersistence interface {
	Persist(ctx context.Context, e *XRPCStreamEvent) error
	Playback(ctx context.Context, since int64, cb func(*XRPCStreamEvent) error) error
	TakeDownRepo(ctx context.Context, usr models.Uid) error
	Flush(context.Context) error
	Shutdown(context.Context) error

	SetEventBroadcaster(func(*XRPCStreamEvent))
}

// MemPersister is the most naive implementation of event persistence
// This EventPersistence option works fine with all event types
// ill do better later
type MemPersister struct {
	buf []*XRPCStreamEvent
	lk  sync.Mutex
	seq int64

	broadcast func(*XRPCStreamEvent)
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
	case e.RepoIdentity != nil:
		e.RepoIdentity.Seq = mp.seq
	case e.RepoAccount != nil:
		e.RepoAccount.Seq = mp.seq
	case e.RepoMigrate != nil:
		e.RepoMigrate.Seq = mp.seq
	case e.RepoTombstone != nil:
		e.RepoTombstone.Seq = mp.seq
	case e.LabelLabels != nil:
		e.LabelLabels.Seq = mp.seq
	default:
		panic("no event in persist call")
	}
	mp.buf = append(mp.buf, e)

	mp.broadcast(e)

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

func (mp *MemPersister) TakeDownRepo(ctx context.Context, uid models.Uid) error {
	return fmt.Errorf("repo takedowns not currently supported by memory persister, test usage only")
}

func (mp *MemPersister) Flush(ctx context.Context) error {
	return nil
}

func (mp *MemPersister) SetEventBroadcaster(brc func(*XRPCStreamEvent)) {
	mp.broadcast = brc
}

func (mp *MemPersister) Shutdown(context.Context) error {
	return nil
}
