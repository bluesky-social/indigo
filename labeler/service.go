package labeler

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/api"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
	label "github.com/bluesky-social/indigo/api/label"
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
	cbg "github.com/whyrusleeping/cbor-gen"

	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/whyrusleeping/go-did"
	"gorm.io/gorm"
)

var log = logging.Logger("labelmaker")

type Server struct {
	db                  *gorm.DB
	cs                  *carstore.CarStore
	repoman             *repomgr.RepoManager
	bgsSlurper          *bgs.Slurper
	evtmgr              *events.EventManager
	echo                *echo.Echo
	user                *RepoConfig
	blobPdsURL          string
	xrpcProxyURL        *url.URL
	xrpcProxyAuthHeader string
	kwLabelers          []KeywordLabeler
	muNSFWImgLabeler    *MicroNSFWImgLabeler
	hiveAILabeler       *HiveAILabeler
	sqrlLabeler         *SQRLLabeler
}

type RepoConfig struct {
	Handle     string
	Did        string
	Password   string
	SigningKey *did.PrivKey
	UserId     util.Uid
}

// In addition to configuring the service, will connect to upstream BGS and start processing events. Won't handle HTTP or WebSocket endpoints until RunAPI() is called.
// 'useWss' is a flag to use SSL for outbound WebSocket connections
func NewServer(db *gorm.DB, cs *carstore.CarStore, repoUser RepoConfig, plcURL, blobPdsURL, xrpcProxyURL, xrpcProxyAdminPassword string, useWss bool) (*Server, error) {

	db.AutoMigrate(models.PDS{})
	db.AutoMigrate(models.Label{})
	db.AutoMigrate(models.ModerationAction{})
	db.AutoMigrate(models.ModerationActionSubjectBlobCid{})
	db.AutoMigrate(models.ModerationReport{})
	db.AutoMigrate(models.ModerationReportResolution{})

	didr := &api.PLCServer{Host: plcURL}
	kmgr := indexer.NewKeyManager(didr, repoUser.SigningKey)
	evtmgr := events.NewEventManager(events.NewMemPersister())
	repoman := repomgr.NewRepoManager(db, cs, kmgr)

	if repoUser.Password == "" || repoUser.Did == "" || repoUser.Handle == "" {
		return nil, fmt.Errorf("bad labeler repo config (empty string)")
	}

	proxyURL, err := url.ParseRequestURI(xrpcProxyURL)
	if err != nil {
		return nil, fmt.Errorf("could not parse XRPC proxy URL (%v): %v", xrpcProxyURL, err)
	}
	xrpcProxyAuthHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:"+xrpcProxyAdminPassword))

	s := &Server{
		db:                  db,
		repoman:             repoman,
		evtmgr:              evtmgr,
		user:                &repoUser,
		blobPdsURL:          blobPdsURL,
		xrpcProxyURL:        proxyURL,
		xrpcProxyAuthHeader: xrpcProxyAuthHeader,
		// sluper configured below
	}

	// ensure that local labelmaker repo exists
	// NOTE: doesn't need to have app.bsky profile and actor config, this is just expediant (reusing an existing helper function)
	ctx := context.Background()
	head, _ := s.repoman.GetRepoRoot(ctx, s.user.UserId)
	if !head.Defined() {
		log.Info("initializing labelmaker repo")
		if err := s.repoman.InitNewActor(ctx, s.user.UserId, s.user.Handle, s.user.Did, "Label Maker", pds.UserActorDeclCid, pds.UserActorDeclType); err != nil {
			return nil, fmt.Errorf("creating labelmaker repo: %w", err)
		}
	} else {
		log.Infof("found labelmaker repo: %s", head)
	}

	slurp := bgs.NewSlurper(db, s.handleBgsRepoEvent, useWss)
	s.bgsSlurper = slurp

	go evtmgr.Run()

	return s, nil
}

func (s *Server) AddKeywordLabeler(kwl KeywordLabeler) {
	log.Infof("configuring keyword labeler")
	s.kwLabelers = append(s.kwLabelers, kwl)
}

func (s *Server) AddMicroNSFWImgLabeler(url string) {
	log.Infof("configuring micro-NSFW-img labeler url=%s", url)
	mnil := NewMicroNSFWImgLabeler(url)
	s.muNSFWImgLabeler = &mnil
}

func (s *Server) AddHiveAILabeler(apiToken string) {
	log.Infof("configuring Hive AI labeler")
	hal := NewHiveAILabeler(apiToken)
	s.hiveAILabeler = &hal
}

func (s *Server) AddSQRLLabeler(url string) {
	log.Infof("configuring SQRL labeler url=%s", url)
	sl := NewSQRLLabeler(url)
	s.sqrlLabeler = &sl
}

// call this *after* all the labelers are configured
func (s *Server) SubscribeBGS(ctx context.Context, bgsURL string, useWss bool) {
	// subscribe our RepoEvent slurper to the BGS, to receive incoming records for labeler
	log.Infof("subscribing to BGS: %s (SSL=%v)", bgsURL, useWss)
	s.bgsSlurper.SubscribeToPds(ctx, bgsURL, useWss)
}

// efficiency predicate to quickly discard events we know that we shouldn't even bother parsing
func (s *Server) wantAnyRecords(ctx context.Context, ra *comatproto.SyncSubscribeRepos_Commit) bool {

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

// should we bother to fetch blob for processing?
func (s *Server) wantBlob(ctx context.Context, blob *lexutil.LexBlob) bool {
	log.Debugf("wantBlob blob=%v", blob)
	// images
	if blob.MimeType == "image/png" || blob.MimeType == "image/jpeg" {
		// only an image API is configured
		if s.muNSFWImgLabeler != nil || s.hiveAILabeler != nil {
			return true
		}
	}
	return false
}

func (s *Server) labelRecord(ctx context.Context, did, nsid, uri, cidStr string, rec cbg.CBORMarshaler) ([]string, error) {
	log.Infof("labeling record: %v", uri)
	var labelVals []string
	var blobs []lexutil.LexBlob
	switch nsid {
	case "app.bsky.feed.post":
		post, suc := rec.(*appbsky.FeedPost)
		if !suc {
			return nil, fmt.Errorf("record failed to deserialize from CBOR: %s", rec)
		}

		// run through all the keyword labelers on posts, saving any resulting labels
		for _, labeler := range s.kwLabelers {
			for _, val := range labeler.LabelPost(*post) {
				labelVals = append(labelVals, val)
			}
		}

		if s.sqrlLabeler != nil {
			sqrlVals, err := s.sqrlLabeler.LabelPost(ctx, *post)
			if err != nil {
				return nil, fmt.Errorf("failed to label post with SQRL: %v", err)
			}
			labelVals = append(labelVals, sqrlVals...)
		}

		// record any image blobs for processing
		if post.Embed != nil && post.Embed.EmbedImages != nil {
			for _, eii := range post.Embed.EmbedImages.Images {
				blobs = append(blobs, *eii.Image)
			}
		}
	case "app.bsky.actor.profile":
		profile, suc := rec.(*appbsky.ActorProfile)
		if !suc {
			return nil, fmt.Errorf("record failed to deserialize from CBOR: %s", rec)
		}

		// run through all the keyword labelers on posts, saving any resulting labels
		for _, labeler := range s.kwLabelers {
			for _, val := range labeler.LabelProfile(*profile) {
				labelVals = append(labelVals, val)
			}
		}

		if s.sqrlLabeler != nil {
			sqrlVals, err := s.sqrlLabeler.LabelProfile(ctx, *profile)
			if err != nil {
				return nil, fmt.Errorf("failed to label profile with SQRL: %v", err)
			}
			labelVals = append(labelVals, sqrlVals...)
		}

		// record avatar and/or banner blobs for processing
		if profile.Avatar != nil {
			blobs = append(blobs, *profile.Avatar)
		}
		if profile.Banner != nil {
			blobs = append(blobs, *profile.Banner)
		}
	}

	log.Infof("will process %d blobs", len(blobs))
	for _, blob := range blobs {
		if !blob.Ref.Defined() {
			return nil, fmt.Errorf("received stub blob (CID undefined)")
		}

		if !s.wantBlob(ctx, &blob) {
			log.Infof("skipping blob: cid=%s", blob.Ref.String())
			continue
		}
		// download image for process
		blobBytes, err := s.downloadRepoBlob(ctx, did, &blob)
		// TODO(bnewbold): instead of erroring, just log any download problems
		if err != nil {
			return nil, err
		}

		blobLabels, err := s.labelBlob(ctx, did, blob, blobBytes)
		// TODO(bnewbold): again, instead of erroring, just log any download problems
		if err != nil {
			return nil, err
		}
		labelVals = append(labelVals, blobLabels...)
	}
	return dedupeStrings(labelVals), nil
}

func (s *Server) downloadRepoBlob(ctx context.Context, did string, blob *lexutil.LexBlob) ([]byte, error) {
	var blobBytes []byte

	if !blob.Ref.Defined() {
		return nil, fmt.Errorf("invalid blob to download (CID undefined)")
	}

	log.Infof("downloading blob pds=%s did=%s cid=%s", s.blobPdsURL, did, blob.Ref.String())

	// TODO(bnewbold): more robust blob fetch code, by constructing query param
	// properly; looking up DID doc; using xrpc.Client (with persistend HTTP
	// client); etc.
	// blocked on getBlob atproto branch landing, with new Lexicon.
	// for now, just fetching from configured PDS (aka our single PDS)
	xrpcURL := fmt.Sprintf("%s/xrpc/com.atproto.sync.getBlob?did=%s&cid=%s", s.blobPdsURL, did, blob.Ref.String())

	resp, err := http.Get(xrpcURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch blob from PDS. did=%s cid=%s statusCode=%d", did, blob.Ref.String(), resp.StatusCode)
	}

	blobBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return blobBytes, nil
}

func (s *Server) labelBlob(ctx context.Context, did string, blob lexutil.LexBlob, blobBytes []byte) ([]string, error) {

	var labelVals []string

	if !blob.Ref.Defined() {
		return nil, fmt.Errorf("invalid blob to label (CID undefined)")
	}

	if s.muNSFWImgLabeler != nil {

		nsfwLabels, err := s.muNSFWImgLabeler.LabelBlob(ctx, blob, blobBytes)
		if err != nil {
			return nil, err
		}
		labelVals = append(labelVals, nsfwLabels...)
	}

	if s.hiveAILabeler != nil {

		hiveLabels, err := s.hiveAILabeler.LabelBlob(ctx, blob, blobBytes)
		if err != nil {
			return nil, err
		}
		labelVals = append(labelVals, hiveLabels...)
	}

	return labelVals, nil
}

// Process incoming repo events coming from BGS, which includes new and updated
// records from any PDS. This function extracts records, handes them to the
// labeling routine, and then persists and broadcasts any resulting labels
func (s *Server) handleBgsRepoEvent(ctx context.Context, pds *models.PDS, evt *events.XRPCStreamEvent) error {

	if evt.RepoCommit == nil {
		// TODO(bnewbold): is this really invalid? do we need to handle Info and Error events here?
		return fmt.Errorf("invalid repo commit event")
	}

	// quick check if we can skip processing the CAR slice entirely
	if !s.wantAnyRecords(ctx, evt.RepoCommit) {
		return nil
	}

	// use an in-memory blockstore with repo wrapper to parse CAR slice
	sliceRepo, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.RepoCommit.Blocks))
	if err != nil {
		log.Warnw("failed to parse CAR slice", "repoErr", err)
		return err
	}

	labels := []*label.Label{}
	for _, op := range evt.RepoCommit.Ops {
		uri := "at://" + evt.RepoCommit.Repo + "/" + op.Path
		nsid := strings.SplitN(op.Path, "/", 2)[0]

		if !(op.Action == "create" || op.Action == "update") {
			continue
		}

		cid, rec, err := sliceRepo.GetRecord(ctx, op.Path)
		if err != nil {
			return fmt.Errorf("record not in CAR slice: %s", uri)
		}
		cidStr := cid.String()
		labelVals, err := s.labelRecord(ctx, evt.RepoCommit.Repo, nsid, uri, cidStr, rec)
		if err != nil {
			return err
		}
		for _, val := range labelVals {
			// apply labels with this pattern to the whole repo, not the record
			if strings.HasPrefix(val, "repo:") {
				val = strings.SplitN(val, ":", 2)[1]
				labels = append(labels, &label.Label{
					Src: s.user.Did,
					Uri: "at://" + evt.RepoCommit.Repo,
					Val: val,
					//Neg
					//Cts
				})
			} else {
				labels = append(labels, &label.Label{
					Src: s.user.Did,
					Uri: uri,
					Cid: &cidStr,
					Val: val,
					//Neg
					//Cts
				})
			}
		}
	}

	// persist and emit events, as needed
	if err := s.CommitLabels(ctx, labels, false); err != nil {
		return err
	}

	// TODO(bnewbold): persist state that we successfully processed the repo event (aka,
	// persist "last" seq in database, or something like that). also above, at
	// the short-circuit
	return nil
}

// crude auth middleware to require "admin token" authentication on a subset of
// routes. Does not implement the usual atproto JWT-based auth. "admin token"
// auth is just HTTP Basic auth with username "admin" and a static password.
// TODO: either transition to some other auth scheme, or review this more carefully
func (s *Server) adminAuthMiddleware() echo.MiddlewareFunc {
	config := middleware.BasicAuthConfig{
		Skipper: func(c echo.Context) bool {
			path := c.Request().URL.Path
			// all admin paths require auth
			if strings.HasPrefix(path, "/xrpc/com.atproto.admin.") {
				return false
			}
			// TODO: will need more complex auth on this endpoint eventually
			if strings.HasPrefix(path, "/xrpc/com.atproto.report.create") {
				return false
			}
			// everything else defaults open
			return true
		},
		Validator: func(username, password string, c echo.Context) (bool, error) {
			// this is the default HTTP Basic validator from echo docs
			// "Be careful to use constant time comparison to prevent timing attacks"
			if subtle.ConstantTimeCompare([]byte(username), []byte("admin")) == 1 &&
				subtle.ConstantTimeCompare([]byte(password), []byte(s.user.Password)) == 1 {
				return true, nil
			}
			log.Warnw("auth failed", "username", string(username))
			return false, nil
		},
		Realm: "AtprotoLabeler",
	}
	return middleware.BasicAuthWithConfig(config)
}

func (s *Server) RunAPI(listen string) error {
	e := echo.New()
	s.echo = e
	e.HideBanner = true
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method} uri=${uri} status=${status} latency=${latency_human}\n",
	}))
	e.Use(s.adminAuthMiddleware())

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		code := 500
		if he, ok := err.(*echo.HTTPError); ok {
			code = he.Code
		}
		log.Warnw("HTTP request error", "statusCode", code, "path", ctx.Path(), "err", err)
		ctx.Response().WriteHeader(code)
	}

	if err := s.RegisterHandlersComAtproto(e); err != nil {
		return err
	}
	if err := s.RegisterProxyHandlers(e); err != nil {
		return err
	}
	// single websocket endpoint
	e.GET("/xrpc/com.atproto.label.subscribeLabels", s.EventsLabelsWebsocket)

	log.Infof("starting labelmaker XRPC and WebSocket daemon at: %s", listen)
	return e.Start(listen)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}
