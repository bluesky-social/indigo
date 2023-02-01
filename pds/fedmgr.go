package pds

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/key"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type IndexCallback func(context.Context, *Peering, *events.RepoEvent) error

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
			log.Errorf("dialing %q failed: %s", host.Host, err)
			time.Sleep(sleepForBackoff(backoff))
			backoff++
			continue
		}

		log.Infof("event subscription response code: %d", res.StatusCode)

		if err := s.handleConnection(host, con); err != nil {
			log.Warnf("connection to %q failed: %s", host.Host, err)
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

		var ev events.RepoEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			return fmt.Errorf("failed to unmarshal event: %w", err)
		}

		log.Infow("got event", "from", host.Host)
		if err := s.cb(context.TODO(), host, &ev); err != nil {
			log.Errorf("failed to index event from %q: %s", host.Host, err)
		}
	}
}
