package events

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/bluesky-social/indigo/models"
	"github.com/cockroachdb/pebble"
)

type PebblePersist struct {
	broadcast func(*XRPCStreamEvent)
	db        *pebble.DB
}

func NewPebblePersistance(path string) (*PebblePersist, error) {
	db, err := pebble.Open(path, &pebble.Options{})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	pp := new(PebblePersist)
	pp.db = db
	return pp, nil
}

func (pp *PebblePersist) Persist(ctx context.Context, e *XRPCStreamEvent) error {
	err := e.Preserialize()
	if err != nil {
		return err
	}
	blob := e.Preserialized

	seq := e.Se

	var key [8]byte
	binary.BigEndian.PutUint64(key, seq)

	return nil
}
func (pp *PebblePersist) Playback(ctx context.Context, since int64, cb func(*XRPCStreamEvent) error) error {
	return nil
}
func (pp *PebblePersist) TakeDownRepo(ctx context.Context, usr models.Uid) error {
	return nil
}
func (pp *PebblePersist) Flush(context.Context) error {
	return nil
}
func (pp *PebblePersist) Shutdown(context.Context) error {
	return nil
}

func (pp *PebblePersist) SetEventBroadcaster(broadcast func(*XRPCStreamEvent)) {
	pp.broadcast = broadcast
}

func (pp *PebblePersist) GetLast(ctx context.Context) (*XRPCStreamEvent, error) {

}
