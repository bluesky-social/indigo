package eventmgr

import (
	"context"
	"testing"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/cmd/relay/stream"

	"github.com/stretchr/testify/require"
)

type fakePersister struct {
	oldest, latest int64
	playback       []*stream.XRPCStreamEvent
	broadcast      func(*stream.XRPCStreamEvent)
}

func (f *fakePersister) Persist(ctx context.Context, e *stream.XRPCStreamEvent) error {
	return nil
}

func (f *fakePersister) Playback(ctx context.Context, since int64, cb func(*stream.XRPCStreamEvent) error) error {
	for _, e := range f.playback {
		if e.Sequence() > since {
			if err := cb(e); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *fakePersister) PlaybackBounds() (int64, int64, error) {
	return f.oldest, f.latest, nil
}

func (f *fakePersister) TakeDownRepo(ctx context.Context, uid uint64) error { return nil }
func (f *fakePersister) Flush(ctx context.Context) error                    { return nil }
func (f *fakePersister) Shutdown(ctx context.Context) error                 { return nil }

func (f *fakePersister) SetEventBroadcaster(fn func(*stream.XRPCStreamEvent)) {
	f.broadcast = fn
}

func commitEvent(seq int64) *stream.XRPCStreamEvent {
	return &stream.XRPCStreamEvent{
		RepoCommit: &comatproto.SyncSubscribeRepos_Commit{Seq: seq},
	}
}

func recvEvent(t *testing.T, ch <-chan *stream.XRPCStreamEvent) *stream.XRPCStreamEvent {
	t.Helper()
	select {
	case evt, ok := <-ch:
		require.True(t, ok, "event channel closed unexpectedly")
		return evt
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event")
		return nil
	}
}

func TestSubscribeFutureCursor(t *testing.T) {
	p := &fakePersister{oldest: 10, latest: 20, playback: []*stream.XRPCStreamEvent{commitEvent(15)}}
	em := NewEventManager(p)

	since := int64(25)
	ch, cleanup, err := em.Subscribe(t.Context(), "test", nil, &since)
	require.NoError(t, err)
	defer cleanup()

	evt := recvEvent(t, ch)
	require.NotNil(t, evt.Error)
	require.Equal(t, "FutureCursor", evt.Error.Error)
	require.Equal(t, "Cursor in the future.", evt.Error.Message)

	select {
	case _, ok := <-ch:
		require.False(t, ok, "channel should be closed after FutureCursor")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}

func TestSubscribeOutdatedCursor(t *testing.T) {
	p := &fakePersister{
		oldest:   100,
		latest:   200,
		playback: []*stream.XRPCStreamEvent{commitEvent(150), commitEvent(160)},
	}
	em := NewEventManager(p)

	since := int64(50)
	ch, cleanup, err := em.Subscribe(t.Context(), "test", nil, &since)
	require.NoError(t, err)
	defer cleanup()

	evt := recvEvent(t, ch)
	require.NotNil(t, evt.RepoInfo)
	require.Equal(t, "OutdatedCursor", evt.RepoInfo.Name)
	require.NotNil(t, evt.RepoInfo.Message)
	require.Equal(t, "Requested cursor exceeded limit. Possibly missing events", *evt.RepoInfo.Message)

	// playback resumes from the oldest retained events
	evt = recvEvent(t, ch)
	require.NotNil(t, evt.RepoCommit)
	require.Equal(t, int64(150), evt.RepoCommit.Seq)
}

func TestSubscribeCursorWithinBounds(t *testing.T) {
	p := &fakePersister{
		oldest:   100,
		latest:   200,
		playback: []*stream.XRPCStreamEvent{commitEvent(150)},
	}
	em := NewEventManager(p)

	// since == oldest-1 is the oldest non-outdated cursor: the client holds
	// every event the relay still has, so no info frame is emitted.
	since := int64(99)
	ch, cleanup, err := em.Subscribe(t.Context(), "test", nil, &since)
	require.NoError(t, err)
	defer cleanup()

	evt := recvEvent(t, ch)
	require.Nil(t, evt.RepoInfo)
	require.Nil(t, evt.Error)
	require.NotNil(t, evt.RepoCommit)
	require.Equal(t, int64(150), evt.RepoCommit.Seq)
}

func TestSubscribeCursorAtLatest(t *testing.T) {
	liveEvt := &stream.XRPCStreamEvent{
		RepoSync: &comatproto.SyncSubscribeRepos_Sync{
			Did:  "did:example:123",
			Rev:  "3jznqjqmjp23k",
			Seq:  21,
			Time: "2026-01-01T00:00:00.000Z",
		},
	}
	p := &fakePersister{
		oldest:   10,
		latest:   20,
		playback: []*stream.XRPCStreamEvent{liveEvt},
	}
	em := NewEventManager(p)

	// a cursor at the newest event is not in the future
	since := int64(20)
	ch, cleanup, err := em.Subscribe(t.Context(), "test", nil, &since)
	require.NoError(t, err)
	defer cleanup()

	// no error or info frame; the subscription proceeds to live events.
	// Push one through the broadcaster once the subscriber is registered.
	// The crossover replay picks it up from playback since live events are
	// persisted before broadcast.
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for {
			em.subsLk.Lock()
			n := len(em.subs)
			em.subsLk.Unlock()
			if n > 0 || time.Now().After(deadline) {
				break
			}
			time.Sleep(time.Millisecond)
		}
		p.broadcast(liveEvt)
	}()

	evt := recvEvent(t, ch)
	require.Nil(t, evt.Error)
	require.Nil(t, evt.RepoInfo)
	require.NotNil(t, evt.RepoSync)
	require.Equal(t, int64(21), evt.RepoSync.Seq)
}
