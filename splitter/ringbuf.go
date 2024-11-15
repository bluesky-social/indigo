package splitter

import (
	"context"
	"sync"

	events "github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/models"
)

func NewEventRingBuffer(chunkSize, nchunks int) *EventRingBuffer {
	return &EventRingBuffer{
		chunkSize:     chunkSize,
		maxChunkCount: nchunks,
	}
}

type EventRingBuffer struct {
	lk            sync.Mutex
	chunks        []*ringChunk
	chunkSize     int
	maxChunkCount int

	broadcast func(*events.XRPCStreamEvent)
}

type ringChunk struct {
	lk  sync.Mutex
	buf []*events.XRPCStreamEvent
}

func (rc *ringChunk) append(evt *events.XRPCStreamEvent) {
	rc.lk.Lock()
	defer rc.lk.Unlock()
	rc.buf = append(rc.buf, evt)
}

func (rc *ringChunk) events() []*events.XRPCStreamEvent {
	rc.lk.Lock()
	defer rc.lk.Unlock()
	return rc.buf
}

func (er *EventRingBuffer) Persist(ctx context.Context, evt *events.XRPCStreamEvent) error {
	er.lk.Lock()
	defer er.lk.Unlock()

	if len(er.chunks) == 0 {
		er.chunks = []*ringChunk{new(ringChunk)}
	}

	last := er.chunks[len(er.chunks)-1]
	if len(last.buf) >= er.chunkSize {
		last = new(ringChunk)
		er.chunks = append(er.chunks, last)
		if len(er.chunks) > er.maxChunkCount {
			er.chunks = er.chunks[1:]
		}
	}

	last.append(evt)

	er.broadcast(evt)
	return nil
}

func (er *EventRingBuffer) Flush(context.Context) error {
	return nil
}

func (er *EventRingBuffer) Playback(ctx context.Context, since int64, cb func(*events.XRPCStreamEvent) error) error {
	// run playback a few times to get as close to 'live' as possible before returning
	for i := 0; i < 10; i++ {
		n, err := er.playbackRound(ctx, since, cb)
		if err != nil {
			return err
		}

		// playback had no new events
		if n-since == 0 {
			return nil
		}
		since = n
	}

	return nil
}

func (er *EventRingBuffer) playbackRound(ctx context.Context, since int64, cb func(*events.XRPCStreamEvent) error) (int64, error) {
	// grab a snapshot of the current chunks
	er.lk.Lock()
	chunks := er.chunks
	er.lk.Unlock()

	i := len(chunks) - 1
	for ; i >= 0; i-- {
		c := chunks[i]
		evts := c.events()
		if since > events.SequenceForEvent(evts[len(evts)-1]) {
			i++
			break
		}
	}
	if i < 0 {
		i = 0
	}

	var lastSeq int64 = since
	for _, c := range chunks[i:] {
		var nread int
		evts := c.events()
		for nread < len(evts) {
			for _, e := range evts[nread:] {
				nread++
				seq := events.SequenceForEvent(e)
				if seq <= since {
					continue
				}

				if err := cb(e); err != nil {
					return 0, err
				}
				lastSeq = seq
			}

			// recheck evts buffer to see if more were added while we were here
			evts = c.events()
		}
	}

	return lastSeq, nil
}

func (er *EventRingBuffer) SetEventBroadcaster(brc func(*events.XRPCStreamEvent)) {
	er.broadcast = brc
}

func (er *EventRingBuffer) Shutdown(context.Context) error {
	return nil
}

func (er *EventRingBuffer) TakeDownRepo(context.Context, models.Uid) error {
	return nil
}
