package splitter

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	events "github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"
	"github.com/bluesky-social/indigo/models"
	"github.com/gorilla/websocket"
	logging "github.com/ipfs/go-log"
)

var log = logging.Logger("splitter")

type Splitter struct {
	Host   string
	erb    *EventRingBuffer
	events *events.EventManager
}

func NewSplitter(host string, persister events.EventPersistence) *Splitter {
	erb := NewEventRingBuffer(20000, 1000)

	em := events.NewEventManager(erb)
	return &Splitter{
		Host:   host,
		erb:    erb,
		events: em,
	}
}

func (s *Splitter) Start() error {
	return nil
}

func sleepForBackoff(b int) time.Duration {
	if b == 0 {
		return 0
	}

	if b < 50 {
		return time.Millisecond * time.Duration(rand.Intn(100)+(5*b))
	}

	return time.Second * 5
}

func (s *Splitter) subscribeWithRedialer(ctx context.Context, host string, cursor int64) {
	d := websocket.Dialer{}

	protocol := "wss"

	var backoff int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		url := fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", protocol, host, cursor)
		con, res, err := d.DialContext(ctx, url, nil)
		if err != nil {
			log.Warnw("dialing failed", "host", host, "err", err, "backoff", backoff)
			time.Sleep(sleepForBackoff(backoff))
			backoff++

			continue
		}

		log.Info("event subscription response code: ", res.StatusCode)

		if err := s.handleConnection(ctx, host, con, &cursor); err != nil {
			log.Warnf("connection to %q failed: %s", host, err)
		}
	}
}

func (s *Splitter) handleConnection(ctx context.Context, host string, con *websocket.Conn, lastCursor *int64) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sched := sequential.NewScheduler("splitter", s.events.AddEvent)
	return events.HandleRepoStream(ctx, con, sched)
}

func sequenceForEvent(evt *events.XRPCStreamEvent) int64 {
	switch {
	case evt.RepoCommit != nil:
		return evt.RepoCommit.Seq
	case evt.RepoHandle != nil:
		return evt.RepoHandle.Seq
	case evt.RepoMigrate != nil:
		return evt.RepoMigrate.Seq
	case evt.RepoTombstone != nil:
		return evt.RepoTombstone.Seq
	case evt.RepoInfo != nil:
		return -1
	default:
		return -1
	}
}

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
	// grab a snapshot of the current chunks
	er.lk.Lock()
	chunks := er.chunks
	er.lk.Unlock()

	i := len(chunks) - 1
	for ; i >= 0; i-- {
		c := chunks[i]
		evts := c.events()
		if since > sequenceForEvent(evts[len(evts)-1]) {
			i++
			break
		}
	}

	for _, c := range chunks[i:] {
		var nread int
		evts := c.events()
		for nread < len(evts) {
			for _, e := range evts[nread:] {
				if since > 0 && sequenceForEvent(e) < since {
					continue
				}
				since = 0

				if err := cb(e); err != nil {
					return err
				}
			}

			// recheck evts buffer to see if more were added while we were here
			evts = c.events()
		}
	}

	// TODO: probably also check for if new chunks were added while we were iterating...
	return nil
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
