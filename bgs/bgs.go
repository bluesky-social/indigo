package bgs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/types"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/gorilla/websocket"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gorm.io/gorm"
)

var log = logging.Logger("bgs")

type BGS struct {
	index   *indexer.Indexer
	db      *gorm.DB
	slurper *Slurper
	events  *events.EventManager
	didr    plc.PLCClient

	repoman *repomgr.RepoManager
}

func NewBGS(db *gorm.DB, ix *indexer.Indexer, repoman *repomgr.RepoManager, evtman *events.EventManager, didr plc.PLCClient) *BGS {
	db.AutoMigrate(User{})
	db.AutoMigrate(PDS{})

	bgs := &BGS{
		index: ix,
		db:    db,

		repoman: repoman,
		events:  evtman,
		didr:    didr,
	}
	bgs.slurper = NewSlurper(db, bgs.handleFedEvent)
	return bgs
}

func (bgs *BGS) Start(listen string) error {
	e := echo.New()

	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
	}))

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		fmt.Printf("HANDLER ERROR: (%s) %s\n", ctx.Path(), err)
		ctx.Response().WriteHeader(500)
	}

	// TODO: this API is temporary until we formalize what we want here
	e.POST("/add-target", bgs.handleAddTarget)

	e.GET("/events", bgs.EventsHandler)

	return e.Start(listen)
}

type PDS struct {
	gorm.Model

	Host string
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
	// TODO: authhhh
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return err
	}

	var since *int64
	if sinceHeader := c.Request().Header.Get("since"); sinceHeader != "" {
		sval, err := strconv.ParseInt(sinceHeader, 10, 64)
		if err != nil {
			return err
		}
		since = &sval
	}

	evts, cancel, err := bgs.events.Subscribe(func(evt *events.Event) bool { return true }, since)
	if err != nil {
		return err
	}
	defer cancel()

	for evt := range evts {
		fmt.Println("outgoing event repo: ", evt.Repo)
		if err := conn.WriteJSON(evt); err != nil {
			return err
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

func (bgs *BGS) handleFedEvent(ctx context.Context, host *PDS, evt *events.Event) error {
	log.Infof("bgs got fed event from %q: %s %s\n", host.Host, evt.Kind, evt.Repo)
	switch evt.Kind {
	case events.EvtKindRepoChange:
		u, err := bgs.lookupUserByDid(ctx, evt.Repo)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("looking up event user: %w", err)
			}

			subj, err := bgs.createExternalUser(ctx, evt.Repo)
			if err != nil {
				return err
			}

			u = new(User)
			u.ID = subj.Uid
		}

		return bgs.repoman.HandleExternalUserEvent(ctx, host.ID, u.ID, evt.RepoOps, evt.CarSlice)
	default:
		return fmt.Errorf("unrecognized fed event kind: %q", evt.Kind)
	}
	return nil
}

func (s *BGS) createExternalUser(ctx context.Context, did string) (*types.ActorInfo, error) {
	log.Infof("create external user: %s", did)
	doc, err := s.didr.GetDocument(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("could not locate DID document for followed user: %s", err)
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
	var peering PDS
	if err := s.db.First(&peering, "host = ?", durl.Host).Error; err != nil {
		fmt.Println("failed to find pds", durl.Host)
		return nil, err
	}

	var handle string
	if len(doc.AlsoKnownAs) > 0 {
		hurl, err := url.Parse(doc.AlsoKnownAs[0])
		if err != nil {
			return nil, err
		}

		handle = hurl.Host
	}

	c := &xrpc.Client{Host: durl.String()}
	profile, err := bsky.ActorGetProfile(ctx, c, did)
	if err != nil {
		return nil, err
	}

	if handle != profile.Handle {
		return nil, fmt.Errorf("mismatch in handle between did document and pds profile (%s != %s)", handle, profile.Handle)
	}

	// TODO: request this users info from their server to fill out our data...
	u := User{
		Handle: handle,
		Did:    did,
		PDS:    peering.ID,
	}

	if err := s.db.Create(&u).Error; err != nil {
		return nil, fmt.Errorf("failed to create other pds user: %w", err)
	}

	// okay cool, its a user on a server we are peered with
	// lets make a local record of that user for the future
	subj := &types.ActorInfo{
		Uid:         u.ID,
		Handle:      handle,
		DisplayName: *profile.DisplayName,
		Did:         did,
		DeclRefCid:  profile.Declaration.Cid,
		Type:        "",
		PDS:         peering.ID,
	}
	if err := s.db.Create(subj).Error; err != nil {
		return nil, err
	}

	return subj, nil
}
