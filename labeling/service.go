package labeling

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/bgs"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/pds"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	util "github.com/bluesky-social/indigo/util"
	cbg "github.com/whyrusleeping/cbor-gen"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/whyrusleeping/go-did"
	"gorm.io/gorm"
)

var log = logging.Logger("labelmaker")

type Server struct {
	db         *gorm.DB
	cs         *carstore.CarStore
	repoman    *repomgr.RepoManager
	bgsSlurper *bgs.Slurper
	levents    *events.LabelEventManager
	echo       *echo.Echo
	user       *LabelmakerRepoConfig
	kwl        []KeywordLabeler
}

type LabelmakerRepoConfig struct {
	handle     string
	did        string
	signingKey *did.PrivKey
	userId     util.Uid
}

// In addition to configuring the service, will connect to upstream BGS and start processing events. Won't handle HTTP or WebSocket endpoints until RunAPI() is called.
// 'useWss' is a flag to use SSL for outbound WebSocket connections
func NewServer(db *gorm.DB, cs *carstore.CarStore, keyFile, repoDid, repoHandle, plcUrl string, useWss bool) (*Server, error) {

	serkey, err := loadKey(keyFile)
	if err != nil {
		return nil, err
	}

	db.AutoMigrate(models.PDS{})

	didr := &api.PLCServer{Host: plcUrl}
	kmgr := indexer.NewKeyManager(didr, serkey)
	levtman := events.NewLabelEventManager(events.NewMemLabelPersister())
	repoman := repomgr.NewRepoManager(db, cs, kmgr)

	user := &LabelmakerRepoConfig{
		handle:     repoHandle,
		did:        repoDid,
		signingKey: serkey,
		userId:     1,
	}

	var kl = KeywordLabeler{value: "meta", keywords: []string{"the", "bluesky", "atproto"}}

	s := &Server{
		db:      db,
		repoman: repoman,
		levents: levtman,
		user:    user,
		kwl:     []KeywordLabeler{kl},
		// sluper configured below
	}

	// ensure that local labelmaker repo exists
	// NOTE: doesn't need to have app.bsky profile and actor config, this is just expediant (reusing an existing helper function)
	ctx := context.Background()
	head, _ := s.repoman.GetRepoRoot(ctx, s.user.userId)
	if head == cid.Undef {
		log.Info("initializing labelmaker repo")
		if err := s.repoman.InitNewActor(ctx, s.user.userId, s.user.handle, s.user.did, "Label Maker", pds.UserActorDeclCid, pds.UserActorDeclType); err != nil {
			return nil, fmt.Errorf("creating labelmaker repo: %w", err)
		}
	} else {
		log.Infof("found labelmaker repo: %s", head)
	}

	slurp := bgs.NewSlurper(db, s.handleBgsRepoEvent, useWss)
	s.bgsSlurper = slurp

	go levtman.Run()

	return s, nil
}

func (s *Server) SubscribeBGS(ctx context.Context, bgsUrl string, useWss bool) {
	// subscribe our RepoEvent slurper to the BGS, to receive incoming records for labeler
	log.Infof("subscribing to BGS: %s (SSL=%v)", bgsUrl, useWss)
	s.bgsSlurper.SubscribeToPds(ctx, bgsUrl, useWss)
}

// efficiency predicate to quickly discard events we know won't want to even parse
func (s *Server) wantAnyRecords(ctx context.Context, ra *events.RepoAppend) bool {

	for _, op := range ra.Ops {
		if op.Action != "create" && op.Action != "update" {
			continue
		}
		nsid := strings.SplitN(op.Path, "/", 2)[0]
		switch nsid {
		case "app.bsky.feed.post":
			return true
		case "app.bsky.actor.profile":
			return true
		default:
			continue
		}
	}
	return false
}

func (s *Server) labelRecord(ctx context.Context, did, nsid, uri, cid string, rec cbg.CBORMarshaler) ([]string, error) {
	log.Infof("labeling record: %v", uri)
	var labelVals []string
	switch nsid {
	case "app.bsky.feed.post":
		post, suc := rec.(*appbsky.FeedPost)
		if !suc {
			return []string{}, fmt.Errorf("record failed to deserialize from CBOR: %s", rec)
		}
		// run through all the keyword labelers on posts, saving any resulting labels
		for _, labeler := range s.kwl {
			for _, val := range labeler.LabelPost(*post) {
				labelVals = append(labelVals, val)
			}
		}
	case "app.bsky.actor.profile":
		profile, suc := rec.(*appbsky.ActorProfile)
		if !suc {
			return []string{}, fmt.Errorf("record failed to deserialize from CBOR: %s", rec)
		}
		// run through all the keyword labelers on posts, saving any resulting labels
		for _, labeler := range s.kwl {
			for _, val := range labeler.LabelActorProfile(*profile) {
				labelVals = append(labelVals, val)
			}
		}
	}
	return labelVals, nil
}

// Process incoming repo events coming from BGS, which includes new and updated
// records from any PDS. This function extracts records, handes them to the
// labeling routine, and then persists and broadcasts any resulting labels
func (s *Server) handleBgsRepoEvent(ctx context.Context, pds *models.PDS, evt *events.RepoStreamEvent) error {

	if evt.Append == nil {
		// TODO(bnewbold): is this really invalid? do we need to handle Info and Error events here?
		return fmt.Errorf("invalid repo append event")
	}

	// quick check if we can skip processing the CAR slice entirely
	if !s.wantAnyRecords(ctx, evt.Append) {
		return nil
	}

	// use an in-memory blockstore with repo wrapper to parse CAR slice
	sliceRepo, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Append.Blocks))
	if err != nil {
		log.Warnw("failed to parse CAR slice", "repoErr", err)
		return err
	}

	now := time.Now().Format(util.ISO8601)
	labels := []events.Label{}

	for _, op := range evt.Append.Ops {
		uri := "at://" + evt.Append.Repo + "/" + op.Path
		nsid := strings.SplitN(op.Path, "/", 2)[0]

		if !(op.Action == "create" || op.Action == "update") {
			continue
		}

		cid, rec, err := sliceRepo.GetRecord(ctx, op.Path)
		if err != nil {
			return fmt.Errorf("record not in CAR slice: %s", uri)
		}
		cidStr := cid.String()
		labelVals, err := s.labelRecord(ctx, s.user.did, nsid, uri, cidStr, rec)
		if err != nil {
			return err
		}
		for _, val := range labelVals {
			labels = append(labels, events.Label{
				SourceDid:  s.user.did,
				SubjectUri: uri,
				SubjectCid: &cidStr,
				Value:      val,
				Timestamp:  now,
			})
		}
	}

	// if any labels generated, persist them to repo...
	for i, l := range labels {
		path, _, err := s.repoman.CreateRecord(ctx, s.user.userId, "com.atproto.label.label", &l)
		if err != nil {
			return fmt.Errorf("failed to persist label in local repo: %w", err)
		}
		labeluri := "at://" + s.user.did + "/" + path
		labels[i].LabelUri = &labeluri
		log.Infof("persisted label: %s", labeluri)
	}

	// ... then re-publish as LabelStreamEvent
	log.Infof("%s", labels)
	if len(labels) > 0 {
		lev := events.LabelStreamEvent{
			Batch: &events.LabelBatch{
				// NOTE(bnewbold): seems like other code handles Seq field automatically
				Labels: labels,
			},
		}
		err = s.levents.AddEvent(&lev)
		if err != nil {
			return fmt.Errorf("failed to publish LabelStreamEvent: %w", err)
		}
	}
	// TODO(bnewbold): persist state that we successfully processed the repo event (aka,
	// persist "last" seq in database, or something like that). also above, at
	// the short-circuit
	return nil
}

func (s *Server) RunAPI(listen string) error {
	e := echo.New()
	s.echo = e
	e.HideBanner = true
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method} uri=${uri} status=${status} latency=${latency_human}\n",
	}))

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		fmt.Printf("Error at path=%s: %v\n", ctx.Path(), err)
		ctx.Response().WriteHeader(500)
	}

	s.RegisterHandlersComAtproto(e)
	// TODO(bnewbold): this is a speculative endpoint name
	e.GET("/xrpc/com.atproto.label.subscribeAllLabels", s.EventsLabelsWebsocket)

	return e.Start(listen)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}
