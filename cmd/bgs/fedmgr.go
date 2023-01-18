package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/whyrusleeping/gosky/events"
	"gorm.io/gorm"
)

type IndexCallback func(context.Context, *PDS, *events.Event) error

// TODO: rename me
type Slurper struct {
	cb IndexCallback

	db *gorm.DB

	lk     sync.Mutex
	active map[string]*PDS
}

func NewSlurper(db *gorm.DB, cb IndexCallback) *Slurper {
	return &Slurper{
		cb:     cb,
		db:     db,
		active: make(map[string]*PDS),
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

	var peering PDS
	if err := s.db.First(&peering, "host = ?", host).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		// New PDS!
		npds := PDS{
			Host: host,
		}
		if err := s.db.Create(&npds).Error; err != nil {
			return err
		}

		peering = npds
	}

	s.active[host] = &peering

	go s.subscribeWithRedialer(&peering)

	return nil
}

func (s *Slurper) subscribeWithRedialer(host *PDS) {
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
				log.Printf("shutting down pds subscription to %s, no activity after %s", host.Host, EventsTimeout)
				return
			}
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

var ErrTimeoutShutdown = fmt.Errorf("timed out waiting for new events")

var EventsTimeout = time.Minute

func (s *Slurper) handleConnection(host *PDS, con *websocket.Conn) error {
	for {
		if err := con.SetReadDeadline(time.Now().Add(EventsTimeout)); err != nil {
			return fmt.Errorf("failed to set read deadline: %w", err)
		}

		mt, data, err := con.ReadMessage()
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				return ErrTimeoutShutdown
			}

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
