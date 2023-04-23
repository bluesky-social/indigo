package bgs

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/models"

	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type IndexCallback func(context.Context, *models.PDS, *events.XRPCStreamEvent) error

// TODO: rename me
type Slurper struct {
	cb IndexCallback

	db *gorm.DB

	lk     sync.Mutex
	active map[string]*models.PDS

	ssl bool
}

func NewSlurper(db *gorm.DB, cb IndexCallback, ssl bool) *Slurper {
	return &Slurper{
		cb:     cb,
		db:     db,
		active: make(map[string]*models.PDS),
		ssl:    ssl,
	}
}

func (s *Slurper) SubscribeToPds(ctx context.Context, host string, reg bool) error {
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
			Host:       host,
			SSL:        s.ssl,
			Registered: reg,
		}
		if err := s.db.Create(&npds).Error; err != nil {
			return err
		}

		peering = npds
	}

	if !peering.Registered && reg {
		peering.Registered = true
		if err := s.db.Model(models.PDS{}).Where("id = ?", peering.ID).Update("registered", true).Error; err != nil {
			return err
		}
	}

	s.active[host] = &peering

	go s.subscribeWithRedialer(&peering)

	return nil
}

func (s *Slurper) RestartAll() error {
	s.lk.Lock()
	defer s.lk.Unlock()

	var all []models.PDS
	if err := s.db.Find(&all).Error; err != nil {
		return err
	}

	for _, pds := range all {
		pds := pds
		s.active[pds.Host] = &pds
		go s.subscribeWithRedialer(&pds)
	}

	return nil
}

func (s *Slurper) subscribeWithRedialer(host *models.PDS) {
	defer func() {
		s.lk.Lock()
		defer s.lk.Unlock()

		delete(s.active, host.Host)
	}()

	d := websocket.Dialer{}

	protocol := "ws"
	if s.ssl {
		protocol = "wss"
	}

	cursor := host.Cursor

	var backoff int
	for {
		url := fmt.Sprintf("%s://%s/xrpc/com.atproto.sync.subscribeRepos?cursor=%d", protocol, host.Host, cursor)
		con, res, err := d.Dial(url, nil)
		if err != nil {
			log.Warnf("dialing %q failed: %s", host.Host, err)
			time.Sleep(sleepForBackoff(backoff))
			backoff++
			continue
		}

		log.Info("event subscription response code: ", res.StatusCode)

		if err = s.handleConnection(host, con, &cursor); err != nil {
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

func (s *Slurper) handleConnection(host *models.PDS, con *websocket.Conn, lastCursor *int64) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	return events.HandleRepoStream(ctx, con, &events.RepoStreamCallbacks{
		RepoCommit: func(evt *comatproto.SyncSubscribeRepos_Commit) error {
			log.Infow("got remote repo event", "host", host.Host, "repo", evt.Repo, "seq", evt.Seq)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoCommit: evt,
			}); err != nil {
				log.Errorf("failed handling event from %q (%d): %s", host.Host, evt.Seq, err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(host, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoHandle: func(evt *comatproto.SyncSubscribeRepos_Handle) error {
			log.Infow("got remote handle update event", "host", host.Host, "did", evt.Did, "handle", evt.Handle)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoHandle: evt,
			}); err != nil {
				log.Errorf("failed handling event from %q (%d): %s", host.Host, evt.Seq, err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(host, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoMigrate: func(evt *comatproto.SyncSubscribeRepos_Migrate) error {
			log.Infow("got remote repo migrate event", "host", host.Host, "did", evt.Did, "migrateTo", evt.MigrateTo)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoMigrate: evt,
			}); err != nil {
				log.Errorf("failed handling event from %q (%d): %s", host.Host, evt.Seq, err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(host, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoTombstone: func(evt *comatproto.SyncSubscribeRepos_Tombstone) error {
			log.Infow("got remote repo tombstone event", "host", host.Host, "did", evt.Did)
			if err := s.cb(context.TODO(), host, &events.XRPCStreamEvent{
				RepoTombstone: evt,
			}); err != nil {
				log.Errorf("failed handling event from %q (%d): %s", host.Host, evt.Seq, err)
			}
			*lastCursor = evt.Seq

			if err := s.updateCursor(host, *lastCursor); err != nil {
				return fmt.Errorf("updating cursor: %w", err)
			}

			return nil
		},
		RepoInfo: func(info *comatproto.SyncSubscribeRepos_Info) error {
			log.Infow("info event", "name", info.Name, "message", info.Message, "host", host.Host)
			return nil
		},
		// TODO: all the other event types (handle change, migration, etc)
		Error: func(errf *events.ErrorFrame) error {
			return fmt.Errorf("error frame: %s: %s", errf.Error, errf.Message)
		},
	})
}

func (s *Slurper) updateCursor(host *models.PDS, curs int64) error {
	return s.db.Model(models.PDS{}).Where("id = ?", host.ID).UpdateColumn("cursor", curs).Error
}

func (s *Slurper) GetActiveList() []string {
	s.lk.Lock()
	defer s.lk.Unlock()
	var out []string
	for k := range s.active {
		out = append(out, k)
	}

	return out
}
