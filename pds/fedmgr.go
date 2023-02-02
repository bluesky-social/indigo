package pds

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/key"

	"github.com/gorilla/websocket"
	cbg "github.com/whyrusleeping/cbor-gen"
	"gorm.io/gorm"
)

var ErrTimeoutShutdown = fmt.Errorf("timed out waiting for new events")
var EventsTimeout = time.Minute

type IndexCallback func(context.Context, *Peering, *events.RepoEvent) error

// TODO: rename me
type Slurper struct {
	cb IndexCallback

	db         *gorm.DB
	signingKey *key.Key
}

func NewSlurper(cb IndexCallback, db *gorm.DB, signingKey *key.Key) Slurper {
	return Slurper{
		cb:         cb,
		db:         db,
		signingKey: signingKey,
	}
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
	cr := cbg.NewCborReader(bytes.NewReader(nil))
	for {
		if err := con.SetReadDeadline(time.Now().Add(EventsTimeout)); err != nil {
			return fmt.Errorf("failed to set read deadline: %w", err)
		}

		mt, r, err := con.NextReader()
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				return ErrTimeoutShutdown
			}

			return err
		}

		switch mt {
		default:
			return fmt.Errorf("We are reallly not prepared for this")
		case websocket.BinaryMessage:
			// ok
		}

		cr.SetReader(r)

		var header events.EventHeader
		if err := header.UnmarshalCBOR(cr); err != nil {
			return err
		}

		switch header.Type {
		case "data":
			var evt events.RepoEvent
			if err := evt.UnmarshalCBOR(cr); err != nil {
				return err
			}

			log.Infow("got event", "from", host.Host, "repo", evt.Repo)
			if err := s.cb(context.TODO(), host, &evt); err != nil {
				log.Errorf("failed to index event from %q: %s", host.Host, err)
			}
		default:
			return fmt.Errorf("unrecognized event stream type: %q", header.Type)
		}

	}
}
