package labeling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api"
	appbsky "github.com/bluesky-social/indigo/api/bsky"
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

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/whyrusleeping/go-did"
	"gorm.io/gorm"
)

var log = logging.Logger("labelmaker")

type Server struct {
	db              *gorm.DB
	cs              *carstore.CarStore
	repoman         *repomgr.RepoManager
	bgsSlurper      *bgs.Slurper
	evtmgr          *events.LabelEventManager
	echo            *echo.Echo
	user            *RepoConfig
	kwl             []KeywordLabeler
	blobPdsUrl      string
	microNsfwImgUrl string
	sqrlUrl         string
}

type RepoConfig struct {
	Handle     string
	Did        string
	SigningKey *did.PrivKey
	UserId     util.Uid
}

// In addition to configuring the service, will connect to upstream BGS and start processing events. Won't handle HTTP or WebSocket endpoints until RunAPI() is called.
// 'useWss' is a flag to use SSL for outbound WebSocket connections
func NewServer(db *gorm.DB, cs *carstore.CarStore, kwl []KeywordLabeler, repoUser RepoConfig, plcUrl, blobPdsUrl, microNsfwImgUrl, sqrlUrl string, useWss bool) (*Server, error) {

	db.AutoMigrate(models.PDS{})

	didr := &api.PLCServer{Host: plcUrl}
	kmgr := indexer.NewKeyManager(didr, repoUser.SigningKey)
	evtmgr := events.NewEventManager(events.NewMemPersister())
	repoman := repomgr.NewRepoManager(db, cs, kmgr)

	s := &Server{
		db:              db,
		repoman:         repoman,
		evtmgr:          evtmgr,
		user:            &repoUser,
		kwl:             kwl,
		blobPdsUrl:      blobPdsUrl,
		microNsfwImgUrl: microNsfwImgUrl,
		sqrlUrl:         sqrlUrl,
		// sluper configured below
	}

	// ensure that local labelmaker repo exists
	// NOTE: doesn't need to have app.bsky profile and actor config, this is just expediant (reusing an existing helper function)
	ctx := context.Background()
	head, _ := s.repoman.GetRepoRoot(ctx, s.user.UserId)
	if head == cid.Undef {
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

// should we bother to fetch blob for processing?
func (s *Server) wantBlob(ctx context.Context, blob *lexutil.Blob) bool {
	log.Debugf("wantBlob blob=%v url=%s", blob, s.microNsfwImgUrl)
	// images
	if blob.MimeType == "image/png" || blob.MimeType == "image/jpeg" {
		// only an NSFW API is configured
		if s.microNsfwImgUrl != "" {
			return true
		}
	}
	return false
}

func (s *Server) labelRecord(ctx context.Context, did, nsid, uri, cid string, rec cbg.CBORMarshaler) ([]string, error) {
	log.Infof("labeling record: %v", uri)
	var labelVals []string
	var blobs []lexutil.Blob
	switch nsid {
	case "app.bsky.feed.post":
		post, suc := rec.(*appbsky.FeedPost)
		if !suc {
			return []string{}, fmt.Errorf("record failed to deserialize from CBOR: %s", rec)
		}
		/* XXX(bnewbold): debugging broken post record
		postJson, _ := json.Marshal(post)
		log.Infof("labeling post: %v", string(postJson))
		buf := new(bytes.Buffer)
		post.MarshalCBOR(buf)
		log.Infof("post CBOR: %v", buf.Bytes())
		*/

		// run through all the keyword labelers on posts, saving any resulting labels
		for _, labeler := range s.kwl {
			for _, val := range labeler.LabelPost(*post) {
				labelVals = append(labelVals, val)
			}
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
			return []string{}, fmt.Errorf("record failed to deserialize from CBOR: %s", rec)
		}
		// XXX(bnewbold): debugging broken profile record
		profileJson, _ := json.Marshal(profile)
		log.Infof("labeling profile: %v", string(profileJson))
		buf := new(bytes.Buffer)
		profile.MarshalCBOR(buf)
		log.Infof("profile CBOR: %v", buf.Bytes())

		// run through all the keyword labelers on posts, saving any resulting labels
		for _, labeler := range s.kwl {
			for _, val := range labeler.LabelActorProfile(*profile) {
				labelVals = append(labelVals, val)
			}
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
		if blob.Cid == "" {
			return []string{}, fmt.Errorf("received stub blob (CID undefined)")
		}

		if !s.wantBlob(ctx, &blob) {
			log.Infof("skipping blob: cid=%s", blob.Cid)
			continue
		}
		// download image for process
		blobBytes, err := s.downloadRepoBlob(ctx, did, &blob)
		// TODO(bnewbold): instead of erroring, just log any download problems
		if err != nil {
			return []string{}, err
		}

		blobLabels, err := s.labelBlob(ctx, did, blob, blobBytes)
		// TODO(bnewbold): again, instead of erroring, just log any download problems
		if err != nil {
			return []string{}, err
		}
		for _, val := range blobLabels {
			labelVals = append(labelVals, val)
		}
	}
	return labelVals, nil
}

func (s *Server) downloadRepoBlob(ctx context.Context, did string, blob *lexutil.Blob) ([]byte, error) {
	var blobBytes []byte

	if blob.Cid == "" {
		return []byte{}, fmt.Errorf("invalid blob to download (CID undefined)")
	}

	log.Infof("downloading blob pds=%s did=%s cid=%s", s.blobPdsUrl, did, blob.Cid)

	// TODO(bnewbold): more robust blob fetch code, by constructing query param
	// properly; looking up DID doc; using xrpc.Client (with persistend HTTP
	// client); etc.
	// blocked on getBlob atproto branch landing, with new Lexicon.
	// for now, just fetching from configured PDS (aka our single PDS)
	xrpcUrl := fmt.Sprintf("%s/xrpc/com.atproto.sync.getBlob?did=%s&cid=%s", s.blobPdsUrl, did, blob.Cid)

	resp, err := http.Get(xrpcUrl)
	if err != nil {
		return []byte{}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return []byte{}, fmt.Errorf("failed to fetch blob from PDS. did=%s cid=%s statusCode=%d", did, blob.Cid, resp.StatusCode)
	}

	blobBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		return blobBytes, nil
	}

	return blobBytes, nil
}

func (s *Server) labelBlob(ctx context.Context, did string, blob lexutil.Blob, blobBytes []byte) ([]string, error) {

	var labelVals []string

	if blob.Cid == "" {
		return []string{}, fmt.Errorf("invalid blob to label (CID undefined)")
	}

	if s.microNsfwImgUrl != "" {

		nsfwScore, err := PostMicroNsfwImg(s.microNsfwImgUrl, blob, blobBytes)
		if err != nil {
			return nil, err
		}
		// TODO(bnewbold): these score cutoffs are kind of arbitrary
		if nsfwScore.Porn > 0.85 {
			labelVals = append(labelVals, "porn")
		}
		if nsfwScore.Hentai > 0.85 {
			labelVals = append(labelVals, "hentai")
		}
		if nsfwScore.Sexy > 0.8 {
			labelVals = append(labelVals, "sexy")
		}
	}

	return labelVals, nil
}

// Process incoming repo events coming from BGS, which includes new and updated
// records from any PDS. This function extracts records, handes them to the
// labeling routine, and then persists and broadcasts any resulting labels
func (s *Server) handleBgsRepoEvent(ctx context.Context, pds *models.PDS, evt *events.XRPCStreamEvent) error {

	if evt.RepoAppend == nil {
		// TODO(bnewbold): is this really invalid? do we need to handle Info and Error events here?
		return fmt.Errorf("invalid repo append event")
	}

	// quick check if we can skip processing the CAR slice entirely
	if !s.wantAnyRecords(ctx, evt.RepoAppend) {
		return nil
	}

	// use an in-memory blockstore with repo wrapper to parse CAR slice
	sliceRepo, err := repo.ReadRepoFromCar(ctx, bytes.NewReader(evt.RepoAppend.Blocks))
	if err != nil {
		log.Warnw("failed to parse CAR slice", "repoErr", err)
		return err
	}

	now := time.Now().Format(util.ISO8601)
	labels := []events.Label{}

	for _, op := range evt.RepoAppend.Ops {
		uri := "at://" + evt.RepoAppend.Repo + "/" + op.Path
		nsid := strings.SplitN(op.Path, "/", 2)[0]

		if !(op.Action == "create" || op.Action == "update") {
			continue
		}

		cid, rec, err := sliceRepo.GetRecord(ctx, op.Path)
		if err != nil {
			return fmt.Errorf("record not in CAR slice: %s", uri)
		}
		cidStr := cid.String()
		labelVals, err := s.labelRecord(ctx, s.user.Did, nsid, uri, cidStr, rec)
		if err != nil {
			return err
		}
		for _, val := range labelVals {
			labels = append(labels, events.Label{
				SourceDid:  s.user.Did,
				SubjectUri: uri,
				SubjectCid: &cidStr,
				Value:      val,
				Timestamp:  now,
			})
		}
	}

	// if any labels generated, persist them to repo...
	for i, l := range labels {
		path, _, err := s.repoman.CreateRecord(ctx, s.user.UserId, "com.atproto.label.label", &l)
		if err != nil {
			return fmt.Errorf("failed to persist label in local repo: %w", err)
		}
		labeluri := "at://" + s.user.Did + "/" + path
		labels[i].LabelUri = &labeluri
		log.Infof("persisted label: %s", labeluri)
	}

	// ... then re-publish as XRPCStreamEvent
	log.Infof("broadcasting labels: %s", labels)
	if len(labels) > 0 {
		lev := events.XRPCStreamEvent{
			LabelBatch: &events.LabelBatch{
				// NOTE(bnewbold): seems like other code handles Seq field automatically
				Labels: labels,
			},
		}
		err = s.evtmgr.AddEvent(ctx, &lev)
		if err != nil {
			return fmt.Errorf("failed to publish XRPCStreamEvent: %w", err)
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
