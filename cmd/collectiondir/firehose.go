package main

import (
	"context"
	"fmt"
	"github.com/bluesky-social/indigo/events"
	"github.com/gorilla/websocket"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

type Firehose struct {
	Log *slog.Logger

	Host string
	Seq  int64

	events chan<- *events.XRPCStreamEvent
}

func (fh *Firehose) subscribeWithRedialer(ctx context.Context, fhevents chan<- *events.XRPCStreamEvent) error {
	defer close(fhevents)
	d := websocket.Dialer{}

	rurl, err := url.Parse(fh.Host)
	if err != nil {
		rurl = new(url.URL)
		rurl.Host = fh.Host
		rurl.Scheme = "wss"
	} else {
		if rurl.Scheme == fh.Host {
			rurl.Scheme = "wss"
		}
		if rurl.Scheme == "https" || rurl.Scheme == "wss" {
			rurl.Scheme = "wss"
		} else if rurl.Scheme == "http" || rurl.Scheme == "ws" {
			rurl.Scheme = "ws"
		} else if rurl.Scheme == "" {
			rurl.Scheme = "wss"
		} else {
			return fmt.Errorf("host unknown scheme %#v", rurl.Scheme)
		}
	}
	//protocol := "wss"
	subscribeReposUrl := rurl.JoinPath("/xrpc/com.atproto.sync.subscribeRepos")
	fh.events = fhevents

	var backoff int
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		header := http.Header{
			"User-Agent": []string{"collectiondir"},
		}

		if fh.Seq >= 0 {
			subscribeReposUrl.RawQuery = fmt.Sprintf("cursor=%d", fh.Seq)
		}
		url := subscribeReposUrl.String()
		con, res, err := d.DialContext(ctx, url, header)
		if err != nil {
			fh.Log.Warn("dialing failed", "url", url, "err", err, "backoff", backoff)
			time.Sleep(time.Duration(5+backoff) * time.Second)
			backoff++

			continue
		} else {
			backoff = 0
		}

		fh.Log.Info("event subscription response", "code", res.StatusCode)

		if err := fh.handleConnection(ctx, con); err != nil {
			fh.Log.Warn("connection failed", "host", fh.Host, "err", err)
		}
	}
}

func (fh *Firehose) handleConnection(ctx context.Context, con *websocket.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	return events.HandleRepoStream(ctx, con, fh, fh.Log)
}

// AddWork is part of events.Scheduler
func (fh *Firehose) AddWork(ctx context.Context, repo string, val *events.XRPCStreamEvent) error {
	tsv, ok := val.GetSequence()
	if ok {
		fh.Seq = tsv
	}
	fh.events <- val
	return nil
}

// Shutdown is part of events.Scheduler
func (fh *Firehose) Shutdown() {
	// unneeded in this usage
}
