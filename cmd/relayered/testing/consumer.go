package testing

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers/sequential"

	"github.com/gorilla/websocket"
)

// testing helper which receives a set of firehose events
type Consumer struct {
	Host     string
	Events   []*events.XRPCStreamEvent
	LastSeq  int64
	Timeout  time.Duration
	eventsLk sync.Mutex
	cancel   func()
}

func NewConsumer(host string) *Consumer {
	c := Consumer{
		Host:    host,
		Timeout: time.Second * 3,
	}
	return &c
}

func (c *Consumer) eventCallbacks() *events.RepoStreamCallbacks {
	rsc := &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			c.eventsLk.Lock()
			defer c.eventsLk.Unlock()
			c.Events = append(c.Events, &events.XRPCStreamEvent{RepoCommit: evt})
			c.LastSeq = evt.Seq
			return nil
		},
		RepoSync: func(evt *comatproto.SyncSubscribeRepos_Sync) error {
			c.eventsLk.Lock()
			defer c.eventsLk.Unlock()
			c.Events = append(c.Events, &events.XRPCStreamEvent{RepoSync: evt})
			c.LastSeq = evt.Seq
			return nil
		},
		RepoIdentity: func(evt *comatproto.SyncSubscribeRepos_Identity) error {
			c.eventsLk.Lock()
			defer c.eventsLk.Unlock()
			c.Events = append(c.Events, &events.XRPCStreamEvent{RepoIdentity: evt})
			c.LastSeq = evt.Seq
			return nil
		},
		RepoAccount: func(evt *comatproto.SyncSubscribeRepos_Account) error {
			c.eventsLk.Lock()
			defer c.eventsLk.Unlock()
			c.Events = append(c.Events, &events.XRPCStreamEvent{RepoAccount: evt})
			c.LastSeq = evt.Seq
			return nil
		},
		// NOTE: this is included to test that the events are *not* passed through; can be removed in the near future
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			c.eventsLk.Lock()
			defer c.eventsLk.Unlock()
			c.Events = append(c.Events, &events.XRPCStreamEvent{RepoHandle: evt})
			c.LastSeq = evt.Seq
			return nil
		},
	}
	return rsc
}

func (c *Consumer) Connect(ctx context.Context, cursor int) error {

	u := c.Host + "/xrpc/com.atproto.sync.subscribeRepos"
	if cursor >= 0 {
		u = u + fmt.Sprintf("?cursor=%d", cursor)
	}

	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(u, nil)
	if err != nil {
		return err
	}

	if resp.StatusCode != 101 {
		return fmt.Errorf("expected HTTP 101 for websocket: %d", resp.StatusCode)
	}

	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	seqScheduler := sequential.NewScheduler("test", c.eventCallbacks().EventHandler)
	go func() {
		if err := events.HandleRepoStream(ctx, conn, seqScheduler, nil); err != nil {
			slog.Error("consumer failed processing event", "err", err)
			cancel()
		}
	}()
	time.Sleep(time.Millisecond * 2) // XXX: is this good?
	return nil
}

func (c *Consumer) Count() int {
	c.eventsLk.Lock()
	defer c.eventsLk.Unlock()
	return len(c.Events)
}

func (c *Consumer) Clear() {
	c.eventsLk.Lock()
	defer c.eventsLk.Unlock()
	c.Events = []*events.XRPCStreamEvent{}
}

func (c *Consumer) Shutdown() {
	if c.cancel != nil {
		c.cancel()
	}
}

// connects to host and consumes 'count' events, then returns them. will try up to 'c.Timeout', and error if not enough events are seen
//
// cursor: pass -1 to consume from current
func (c *Consumer) ConsumeEvents(count int) ([]*events.XRPCStreamEvent, error) {
	// poll until we have enough events
	start := time.Now()
	for {
		if c.Count() >= count {
			break
		}
		if time.Since(start) > c.Timeout {
			return nil, fmt.Errorf("test stream consumer timeout: %s", c.Timeout)
		}
		time.Sleep(time.Millisecond * 5)
	}
	return c.Events, nil
}
