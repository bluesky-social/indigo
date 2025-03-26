package events

import (
	"context"

	"github.com/bluesky-social/indigo/cmd/relayered/models"
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
