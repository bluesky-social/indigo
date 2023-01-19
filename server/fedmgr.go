package schemagen

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/key"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type IndexCallback func(context.Context, *Peering, *events.Event) error

// TODO: rename me
type Slurper struct {
	cb IndexCallback

	db         *gorm.DB
	signingKey *key.Key
}

func (s *Slurper) SubscribeToPds(ctx context.Context, host string) error {
	var peering Peering
	if err := s.db.First(&peering, "host = ?", host).Error; err != nil {
		return err
	}

	go s.subscribeWithRedialer(&peering)

	return nil
}

func (s *Slurper) subscribeWithRedialer(host *Peering) {
	d := websocket.Dialer{}

	var backoff int
	for {
		h := http.Header{
			"DID": []string{s.signingKey.DID()},
		}

		con, res, err := d.Dial("ws://"+host.Host+"/events", h)
		if err != nil {
			fmt.Printf("dialing %q failed: %s", host.Host, err)
			time.Sleep(sleepForBackoff(backoff))
			backoff++
			continue
		}

		fmt.Println("event subscription response code: ", res.StatusCode)

		if err := s.handleConnection(host, con); err != nil {
			log.Printf("connection to %q failed: %s", host.Host, err)
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

func (s *Slurper) handleConnection(host *Peering, con *websocket.Conn) error {
	for {
		mt, data, err := con.ReadMessage()
		if err != nil {
			return err
		}

		_ = mt

		var ev events.Event
		if err := json.Unmarshal(data, &ev); err != nil {
			return fmt.Errorf("failed to unmarshal event: %w", err)
		}

		fmt.Println("got event: ", host.Host, ev.Kind)
		if err := s.cb(context.TODO(), host, &ev); err != nil {
			log.Printf("failed to index event from %q: %s", host.Host, err)
		}
	}
}
