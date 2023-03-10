package labeling

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/bgs"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/pds"
	"github.com/bluesky-social/indigo/repo"
	"github.com/bluesky-social/indigo/repomgr"
	util "github.com/bluesky-social/indigo/util"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/lestrrat-go/jwx/jwa"
	jwk "github.com/lestrrat-go/jwx/jwk"
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

func NewServer(db *gorm.DB, cs *carstore.CarStore, keyFile, repoDid, repoHandle, bgsUrl, plcUrl string) (*Server, error) {

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

	var kl = KeywordLabeler{value: "rude", keywords: []string{"🍆", "sex", "ab", "before", "yours", "the"}}

	s := &Server{
		db:      db,
		repoman: repoman,
		levents: levtman,
		user:    user,
		kwl:     []KeywordLabeler{kl},
		// sluper configured below
	}

	// ensure that local labelmaker repo exists
	// NOTE: doesn't need to have app.bsky profile and actor config, this is just expedient (reusing helper)
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

	// TODO(bnewbold): enforce ssl (last boolean argument here)
	slurp := bgs.NewSlurper(db, s.handleBgsRepoEvent, false)
	s.bgsSlurper = slurp

	// subscribe our RepoEvent slurper to the BGS, to receive incoming records for labeler
	useWebsocketSSL := false
	log.Infof("subscribing to BGS: %s (SSL=%v)", bgsUrl, useWebsocketSSL)
	s.bgsSlurper.SubscribeToPds(ctx, bgsUrl, useWebsocketSSL)

	// NOTE: this is where outgoing RepoEvents could be generated
	// should skip indexing (we are not a PDS) and just ship out repo event stream
	/*
		repoman.SetEventHandler(func(ctx context.Context, evt *repomgr.RepoEvent) {
			if err := ix.HandleRepoEvent(ctx, evt); err != nil {
				log.Errorw("handle repo event failed", "user", evt.User, "err", err)
			}
		})
	*/

	go levtman.Run()

	return s, nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}

// incoming repo events
func (s *Server) handleBgsRepoEvent(ctx context.Context, pds *models.PDS, evt *events.RepoStreamEvent) error {
	now := time.Now().Format(util.ISO8601)
	switch {
	case evt.Append != nil:
		// this is where we take incoming RepoEvents and label them
		// use an in-memory blockstore with repo wrapper to parse CAR
		// NOTE: could refactor to parse ops first, so we don't bother parsing the CAR if there are no posts/profiles to process (a common case, for likes/follows/reposts/etc)
		sliceRepo, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.Append.Blocks))
		if err != nil {
			log.Warnw("failed to parse CAR slice", "repoErr", err)
			return err
		}
		var labels []events.Label = []events.Label{}
		for _, op := range evt.Append.Ops {
			uri := "at://" + evt.Append.Repo + "/" + op.Path
			nsid := strings.SplitN(op.Path, "/", 2)[0]
			// filter to creation/update of ony post/profile records
			// TODO(bnewbold): how do I 'switch' on a tuple here in golang, instead of nested switch?
			switch op.Action {
			case "create", "update":
				log.Infof("labeling record: %v", uri)
				switch nsid {
				case "app.bsky.feed.post":
					cid, rec, err := sliceRepo.GetRecord(ctx, op.Path)
					if err != nil {
						return fmt.Errorf("record not in CAR slice: %s", uri)
					}
					cidStr := cid.String()
					post, suc := rec.(*bsky.FeedPost)
					if !suc {
						return fmt.Errorf("record failed to deserialize from CBOR: %s", rec)
					}
					// run through all the keyword labelers on posts, saving any resulting labels
					for _, labeler := range s.kwl {
						for _, val := range labeler.labelPost(*post) {
							labels = append(labels, events.Label{
								SourceDid:  s.user.did,
								SubjectUri: uri,
								SubjectCid: &cidStr,
								Value:      val,
								Timestamp:  now,
							})
						}
					}
				case "app.bsky.actor.profile":
					// NOTE: copypasta from post above, could refactor to not duplicate
					cid, rec, err := sliceRepo.GetRecord(ctx, op.Path)
					if err != nil {
						return fmt.Errorf("record not in CAR slice: %s", uri)
					}
					cidStr := cid.String()
					profile, suc := rec.(*bsky.ActorProfile)
					if !suc {
						return fmt.Errorf("record failed to deserialize from CBOR: %s", rec)
					}
					// run through all the keyword labelers on profiles, saving any resulting labels
					for _, labeler := range s.kwl {
						for _, val := range labeler.labelActorProfile(*profile) {
							labels = append(labels, events.Label{
								SourceDid:  s.user.did,
								SubjectUri: uri,
								SubjectCid: &cidStr,
								Value:      val,
								Timestamp:  now,
							})
						}
					}
				default:
					continue
				}
			default:
				continue
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
				// XXX(bnewbold): what should sequence number be? do I need to maintain that?
				Batch: &events.LabelBatch{
					Seq:    0,
					Labels: labels,
				},
			}
			err = s.levents.AddEvent(&lev)
			if err != nil {
				return fmt.Errorf("failed to publish LabelStreamEvent: %w", err)
			}
		}
		// TODO: update state that we successfully processed the repo event (aka, persist "last" seq in database, or something like that)
		return nil
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (s *Server) readRecordFunc(ctx context.Context, user util.Uid, c cid.Cid) (lexutil.CBOR, error) {
	bs, err := s.cs.ReadOnlySession(user)
	if err != nil {
		return nil, err
	}

	blk, err := bs.Get(ctx, c)
	if err != nil {
		return nil, err
	}

	return lexutil.CborDecodeValue(blk.RawData())
}

func loadKey(kfile string) (*did.PrivKey, error) {
	kb, err := os.ReadFile(kfile)
	if err != nil {
		return nil, err
	}

	sk, err := jwk.ParseKey(kb)
	if err != nil {
		return nil, err
	}

	var spk ecdsa.PrivateKey
	if err := sk.Raw(&spk); err != nil {
		return nil, err
	}
	curve, ok := sk.Get("crv")
	if !ok {
		return nil, fmt.Errorf("need a curve set")
	}

	var out string
	kts := string(curve.(jwa.EllipticCurveAlgorithm))
	switch kts {
	case "P-256":
		out = did.KeyTypeP256
	default:
		return nil, fmt.Errorf("unrecognized key type: %s", kts)
	}

	return &did.PrivKey{
		Raw:  &spk,
		Type: out,
	}, nil
}

func (s *Server) RunAPI(listen string) error {
	e := echo.New()
	s.echo = e
	e.HideBanner = true
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
	}))

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		fmt.Printf("HANDLER ERROR: (%s) %s\n", ctx.Path(), err)
		ctx.Response().WriteHeader(500)
	}

	s.RegisterHandlersComAtproto(e)
	e.GET("/events/v0/labels", s.EventsLabelsWebsocket)

	return e.Start(listen)
}
