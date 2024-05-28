package events

import (
	"context"
	"fmt"
	"sync"

	"github.com/bluesky-social/indigo/models"
)

// YoloPersister is used for benchmarking, it has no persistence, it just emits events and forgets them
type YoloPersister struct {
	lk  sync.Mutex
	seq int64

	broadcast func(*XRPCStreamEvent)
}

func NewYoloPersister() *YoloPersister {
	return &YoloPersister{}
}

func (yp *YoloPersister) Persist(ctx context.Context, e *XRPCStreamEvent) error {
	yp.lk.Lock()
	defer yp.lk.Unlock()
	yp.seq++
	switch {
	case e.RepoCommit != nil:
		e.RepoCommit.Seq = yp.seq
	case e.RepoHandle != nil:
		e.RepoHandle.Seq = yp.seq
	case e.RepoIdentity != nil:
		e.RepoIdentity.Seq = yp.seq
	case e.RepoAccount != nil:
		e.RepoAccount.Seq = yp.seq
	case e.RepoMigrate != nil:
		e.RepoMigrate.Seq = yp.seq
	case e.RepoTombstone != nil:
		e.RepoTombstone.Seq = yp.seq
	case e.LabelLabels != nil:
		e.LabelLabels.Seq = yp.seq
	default:
		panic("no event in persist call")
	}

	yp.broadcast(e)

	return nil
}

func (mp *YoloPersister) Playback(ctx context.Context, since int64, cb func(*XRPCStreamEvent) error) error {
	return fmt.Errorf("playback not supported by yolo persister, test usage only")
}

func (yp *YoloPersister) TakeDownRepo(ctx context.Context, uid models.Uid) error {
	return fmt.Errorf("repo takedowns not currently supported by memory persister, test usage only")
}

func (yp *YoloPersister) SetEventBroadcaster(brc func(*XRPCStreamEvent)) {
	yp.broadcast = brc
}

func (yp *YoloPersister) Flush(ctx context.Context) error {
	return nil
}

func (yp *YoloPersister) Shutdown(ctx context.Context) error {
	return nil
}
