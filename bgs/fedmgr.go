package bgs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/models"
	"github.com/gorilla/websocket"
	cbg "github.com/whyrusleeping/cbor-gen"
	"gorm.io/gorm"
)

type IndexCallback func(context.Context, *models.PDS, *events.RepoEvent) error

// TODO: rename me
type Slurper struct {
	cb IndexCallback

	db *gorm.DB

	lk     sync.Mutex
	active map[string]*models.PDS
}

func NewSlurper(db *gorm.DB, cb IndexCallback) *Slurper {
	return &Slurper{
		cb:     cb,
		db:     db,
		active: make(map[string]*models.PDS),
	}
}

func (s *Slurper) SubscribeToPds(ctx context.Context, host string) error {
	// TODO: for performance, lock on the hostname instead of global
	s.lk.Lock()
	defer s.lk.Unlock()

	_, ok := s.active[host]
	if ok {
		return nil
	}

	var peering models.PDS
	if err := s.db.Find(&peering, "host = ?", host).Error; err != nil {
		return err
	}

	if peering.ID == 0 {
		// New PDS!
		npds := models.PDS{
			Host: host,
		}
		fmt.Println("CREATING NEW PDS", host)
		if err := s.db.Create(&npds).Error; err != nil {
			return err
		}

		peering = npds
	}

	s.active[host] = &peering

	go s.subscribeWithRedialer(&peering)

	return nil
}

func (s *Slurper) subscribeWithRedialer(host *models.PDS) {
	defer func() {
		s.lk.Lock()
		defer s.lk.Unlock()

		delete(s.active, host.Host)
	}()

	d := websocket.Dialer{}

	var backoff int
	for {

		con, res, err := d.Dial("ws://"+host.Host+"/events", nil)
		if err != nil {
			fmt.Printf("dialing %q failed: %s", host.Host, err)
			time.Sleep(sleepForBackoff(backoff))
			backoff++
			continue
		}

		fmt.Println("event subscription response code: ", res.StatusCode)

		if err := s.handleConnection(host, con); err != nil {
			if errors.Is(err, ErrTimeoutShutdown) {
				log.Infof("shutting down pds subscription to %s, no activity after %s", host.Host, EventsTimeout)
				return
			}
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

var ErrTimeoutShutdown = fmt.Errorf("timed out waiting for new events")

var EventsTimeout = time.Minute

func (s *Slurper) handleConnection(host *models.PDS, con *websocket.Conn) error {
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

			log.Infow("got remote repo event", "host", host.Host, "repo", evt.Repo)
			if err := s.cb(context.TODO(), host, &evt); err != nil {
				log.Errorf("failed to index event from %q: %s", host.Host, err)
			}
		default:
			return fmt.Errorf("unrecognized event stream type: %q", header.Type)
		}

	}
}
