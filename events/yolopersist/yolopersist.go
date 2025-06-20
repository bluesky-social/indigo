package yolopersist

import (
	"context"
	"fmt"
	"sync"

	"github.com/gander-social/gander-indigo-sovereign/events"
	"github.com/gander-social/gander-indigo-sovereign/models"
)

// YoloPersister is used for benchmarking, it has no persistence, it just emits events and forgets them
type YoloPersister struct {
	lk  sync.Mutex
	seq int64

	broadcast func(*events.XRPCStreamEvent)
}

func NewYoloPersister() *YoloPersister {
	return &YoloPersister{}
}

func (yp *YoloPersister) Persist(ctx context.Context, e *events.XRPCStreamEvent) error {
	yp.lk.Lock()
	defer yp.lk.Unlock()
	yp.seq++
	switch {
	case e.RepoCommit != nil:
		e.RepoCommit.Seq = yp.seq
	case e.RepoSync != nil:
		e.RepoSync.Seq = yp.seq
	case e.RepoIdentity != nil:
		e.RepoIdentity.Seq = yp.seq
	case e.RepoAccount != nil:
		e.RepoAccount.Seq = yp.seq
	case e.LabelLabels != nil:
		e.LabelLabels.Seq = yp.seq
	default:
		panic("no event in persist call")
	}

	yp.broadcast(e)

	return nil
}

func (mp *YoloPersister) Playback(ctx context.Context, since int64, cb func(*events.XRPCStreamEvent) error) error {
	return fmt.Errorf("playback not supported by yolo persister, test usage only")
}

func (yp *YoloPersister) TakeDownRepo(ctx context.Context, uid models.Uid) error {
	return fmt.Errorf("repo takedowns not currently supported by memory persister, test usage only")
}

func (yp *YoloPersister) SetEventBroadcaster(brc func(*events.XRPCStreamEvent)) {
	yp.broadcast = brc
}

func (yp *YoloPersister) Flush(ctx context.Context) error {
	return nil
}

func (yp *YoloPersister) Shutdown(ctx context.Context) error {
	return nil
}
