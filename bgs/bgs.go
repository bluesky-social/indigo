package bgs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gorm.io/gorm"
)

var log = logging.Logger("bgs")

type BGS struct {
	Index   *indexer.Indexer
	db      *gorm.DB
	slurper *Slurper
	events  *events.EventManager
	didr    plc.PLCClient

	crawlOnly bool

	// TODO: at some point we will want to lock specific DIDs, this lock as is
	// is overly broad, but i dont expect it to be a bottleneck for now
	extUserLk sync.Mutex

	repoman *repomgr.RepoManager
}

func NewBGS(db *gorm.DB, ix *indexer.Indexer, repoman *repomgr.RepoManager, evtman *events.EventManager, didr plc.PLCClient, ssl bool) *BGS {
	db.AutoMigrate(User{})
	db.AutoMigrate(models.PDS{})

	bgs := &BGS{
		Index: ix,
		db:    db,

		repoman: repoman,
		events:  evtman,
		didr:    didr,
	}

	ix.CreateExternalUser = bgs.createExternalUser
	bgs.slurper = NewSlurper(db, bgs.handleFedEvent, ssl)
	return bgs
}

func (bgs *BGS) Start(listen string) error {
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
	}))

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		log.Errorf("HANDLER ERROR: (%s) %s", ctx.Path(), err)
		ctx.Response().WriteHeader(500)
	}

	// TODO: this API is temporary until we formalize what we want here
	e.POST("/add-target", bgs.handleAddTarget)

	e.GET("/xrpc/com.atproto.sync.subscribeAllRepos", bgs.EventsHandler)

	return e.Start(listen)
}

type User struct {
	gorm.Model
	Handle string `gorm:"uniqueIndex"`
	Did    string `gorm:"uniqueIndex"`
	PDS    uint
}

type addTargetBody struct {
	Host string `json:"host"`
}

// the ding-dong api
func (bgs *BGS) handleAddTarget(c echo.Context) error {
	var body addTargetBody
	if err := c.Bind(&body); err != nil {
		return err
	}

	if body.Host == "" {
		return fmt.Errorf("no host specified")
	}

	return bgs.slurper.SubscribeToPds(c.Request().Context(), body.Host)
}

func (bgs *BGS) EventsHandler(c echo.Context) error {
	fmt.Println("events handler")

	var since *int64
	if sinceVal := c.QueryParam("since"); sinceVal != "" {
		sval, err := strconv.ParseInt(sinceVal, 10, 64)
		if err != nil {
			return err
		}
		since = &sval
	}

	// TODO: authhhh
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return err
	}

	evts, cancel, err := bgs.events.Subscribe(func(evt *events.RepoStreamEvent) bool { return true }, since)
	if err != nil {
		return err
	}
	defer cancel()

	header := events.EventHeader{Type: events.EvtKindRepoAppend}
	for evt := range evts {
		wc, err := conn.NextWriter(websocket.BinaryMessage)
		if err != nil {
			return err
		}

		var obj util.CBOR

		switch {
		case evt.Append != nil:
			header.Type = events.EvtKindRepoAppend
			obj = evt.Append
		default:
			return fmt.Errorf("unrecognized event kind")
		}

		if err := header.MarshalCBOR(wc); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}

		if err := obj.MarshalCBOR(wc); err != nil {
			return fmt.Errorf("failed to write event: %w", err)
		}

		if err := wc.Close(); err != nil {
			return fmt.Errorf("failed to flush-close our event write: %w", err)
		}
	}

	return nil
}

func (bgs *BGS) lookupUserByDid(ctx context.Context, did string) (*User, error) {
	var u User
	if err := bgs.db.Find(&u, "did = ?", did).Error; err != nil {
		return nil, err
	}

	if u.ID == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &u, nil
}

func (bgs *BGS) handleFedEvent(ctx context.Context, host *models.PDS, env *events.RepoStreamEvent) error {
	switch {
	case env.Append != nil:
		evt := env.Append
		log.Infof("bgs got repo append event %d from %q: %s\n", evt.Seq, host.Host, evt.Repo)
		u, err := bgs.lookupUserByDid(ctx, evt.Repo)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("looking up event user: %w", err)
			}

			subj, err := bgs.createExternalUser(ctx, evt.Repo)
			if err != nil {
				return fmt.Errorf("fed event create external user: %w", err)
			}

			u = new(User)
			u.ID = subj.Uid
			u.Did = evt.Repo
		}

		var prevcid *cid.Cid
		if evt.Prev != "" {
			c, err := cid.Decode(evt.Prev)
			if err != nil {
				return fmt.Errorf("invalid value for prev cid in event: %w", err)
			}
			prevcid = &c
		}

		// TODO: if the user is already in the 'slow' path, we shouldnt even bother trying to fast path this event

		if err := bgs.repoman.HandleExternalUserEvent(ctx, host.ID, u.ID, u.Did, prevcid, evt.Blocks); err != nil {
			if !errors.Is(err, carstore.ErrRepoBaseMismatch) {
				return fmt.Errorf("handle user event failed: %w", err)
			}

			ai, err := bgs.Index.LookupUser(ctx, u.ID)
			if err != nil {
				return err
			}

			return bgs.Index.Crawler.AddToCatchupQueue(ctx, host, ai, evt)
		}

		return nil
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (s *BGS) createExternalUser(ctx context.Context, did string) (*models.ActorInfo, error) {
	log.Infof("create external user: %s", did)
	doc, err := s.didr.GetDocument(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("could not locate DID document for followed user (%s): %w", did, err)
	}

	if len(doc.Service) == 0 {
		return nil, fmt.Errorf("external followed user %s had no services in did document", did)
	}

	svc := doc.Service[0]
	durl, err := url.Parse(svc.ServiceEndpoint)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(durl.Host, "localhost:") {
		durl.Scheme = "http"
	}

	// TODO: the PDS's DID should also be in the service, we could use that to look up?
	var peering models.PDS
	if err := s.db.Find(&peering, "host = ?", durl.Host).Error; err != nil {
		log.Error("failed to find pds", durl.Host)
		return nil, err
	}

	c := &xrpc.Client{Host: durl.String()}

	if peering.ID == 0 {
		pdsdid, err := atproto.HandleResolve(ctx, c, "")
		if err != nil {
			// TODO: failing this shouldnt halt our indexing
			return nil, fmt.Errorf("failed to get accounts config for unrecognized pds: %w", err)
		}

		// TODO: could check other things, a valid response is good enough for now
		peering.Host = durl.Host
		peering.Did = pdsdid.Did
		peering.SSL = (durl.Scheme == "https")

		if err := s.db.Create(&peering).Error; err != nil {
			return nil, err
		}
	}

	if len(doc.AlsoKnownAs) == 0 {
		return nil, fmt.Errorf("user has no 'known as' field in their DID document")
	}

	hurl, err := url.Parse(doc.AlsoKnownAs[0])
	if err != nil {
		return nil, err
	}

	handle := hurl.Host

	res, err := atproto.HandleResolve(ctx, c, handle)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve users claimed handle (%q) on pds: %w", handle, err)
	}

	if res.Did != did {
		return nil, fmt.Errorf("claimed handle did not match servers response (%s != %s)", res.Did, did)
	}

	s.extUserLk.Lock()
	defer s.extUserLk.Unlock()

	exu, err := s.Index.LookupUserByDid(ctx, did)
	if err == nil {
		log.Warnf("lost the race to create a new user: %d", did)
		return exu, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	// TODO: request this users info from their server to fill out our data...
	u := User{
		Handle: handle,
		Did:    did,
		PDS:    peering.ID,
	}

	if err := s.db.Create(&u).Error; err != nil {
		// some debugging...
		return nil, fmt.Errorf("failed to create other pds user: %w", err)
	}

	// okay cool, its a user on a server we are peered with
	// lets make a local record of that user for the future
	subj := &models.ActorInfo{
		Uid:         u.ID,
		Handle:      handle,
		DisplayName: "", //*profile.DisplayName,
		Did:         did,
		DeclRefCid:  "", // profile.Declaration.Cid,
		Type:        "",
		PDS:         peering.ID,
	}
	if err := s.db.Create(subj).Error; err != nil {
		return nil, err
	}

	return subj, nil
}
