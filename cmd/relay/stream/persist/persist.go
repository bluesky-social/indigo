package persist

import (
	"context"

	"github.com/gander-social/gander-indigo-sovereign/cmd/relay/stream"
)

// Note that this interface looks generic, but some persisters might only work with RepoAppend or LabelLabels
type EventPersistence interface {
	Persist(ctx context.Context, e *stream.XRPCStreamEvent) error
	Playback(ctx context.Context, since int64, cb func(*stream.XRPCStreamEvent) error) error
	TakeDownRepo(ctx context.Context, uid uint64) error
	Flush(context.Context) error
	Shutdown(context.Context) error

	SetEventBroadcaster(func(*stream.XRPCStreamEvent))
}
