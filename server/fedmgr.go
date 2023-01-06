package schemagen

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/gorilla/websocket"
)

type FederationManager struct {
	indexCallback func(context.Context, string, *Event) error
}

func (fm *FederationManager) SubscribeToPds(ctx context.Context, host string) error {
	go fm.subscribeWithRedialer(host)

	return nil
}

func (fm *FederationManager) subscribeWithRedialer(host string) {
	d := websocket.Dialer{}

	var backoff int
	for {
		con, res, err := d.Dial(host+"/events", nil)
		if err != nil {
			fmt.Printf("dialing %q failed: %s", host, err)
			time.Sleep(sleepForBackoff(backoff))
			backoff++
		}
		_ = res

		if err := fm.handleConnection(host, con); err != nil {
			log.Printf("connection to %q failed: %s", host, err)
		}
	}
}

func sleepForBackoff(b int) time.Duration {
	if b == 0 {
		return 0
	}

	if b < 10 {
		return (time.Duration(b) * 2) + (time.Millisecond * time.Duration(rand.Intn(1000)))
	}

	return time.Second * 30
}

func (fm *FederationManager) handleConnection(host string, con *websocket.Conn) error {
	for {
		mt, data, err := con.ReadMessage()
		if err != nil {
			return err
		}

		_ = mt

		var ev Event
		if err := json.Unmarshal(data, &ev); err != nil {
			return fmt.Errorf("failed to unmarshal event: %w", err)
		}

		if err := fm.indexCallback(context.TODO(), host, &ev); err != nil {
			log.Printf("failed to index event from %q: %s", host, err)
		}
	}
}
