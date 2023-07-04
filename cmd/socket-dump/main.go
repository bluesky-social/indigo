package main

import (
	"context"
	"log"
	"net/url"

	"github.com/bluesky-social/indigo/events"
	"github.com/gorilla/websocket"
)

func main() {
	ctx := context.Background()
	log.Println("connecting to WebSocket...")

	u := url.URL{Scheme: "ws", Host: "localhost:12832", Path: "/xrpc/com.atproto.sync.subscribeRepos", RawQuery: "cursor=0"}
	//u := url.URL{Scheme: "wss", Host: "bsky.social", Path: "/xrpc/com.atproto.sync.subscribeRepos"}

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("failed to connect to websocket: %v", err)
	}
	defer c.Close()

	pool := events.NewConsumerPool(8, 16, func(ctx context.Context, xe *events.XRPCStreamEvent) error {
		switch {
		case xe.RepoCommit != nil:
			return nil
		}
		return nil
	})

	err = events.HandleRepoStream(ctx, c, pool)
	log.Printf("HandleRepoStream returned unexpectedly: %+v...", err)
}
