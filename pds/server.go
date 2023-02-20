package pds

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"

	atproto "github.com/bluesky-social/indigo/api/atproto"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/notifs"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/xrpc"
	gojwt "github.com/golang-jwt/jwt"
	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/lestrrat-go/jwx/jwa"
	jwk "github.com/lestrrat-go/jwx/jwk"
	jwt "github.com/lestrrat-go/jwx/jwt"
	"github.com/whyrusleeping/go-did"
	"gorm.io/gorm"
)

var log = logging.Logger("pds")

type Server struct {
	db             *gorm.DB
	cs             *carstore.CarStore
	repoman        *repomgr.RepoManager
	feedgen        *FeedGenerator
	notifman       notifs.NotificationManager
	indexer        *indexer.Indexer
	events         *events.EventManager
	signingKey     *did.PrivKey
	echo           *echo.Echo
	jwtSigningKey  []byte
	enforcePeering bool

	handleSuffix string
	serviceUrl   string

	plc plc.PLCClient
}

const UserActorDeclCid = "bafyreid27zk7lbis4zw5fz4podbvbs4fc5ivwji3dmrwa6zggnj4bnd57u"
const UserActorDeclType = "app.bsky.system.actorUser"

func NewServer(db *gorm.DB, cs *carstore.CarStore, kfile string, handleSuffix, serviceUrl string, didr plc.PLCClient, jwtkey []byte) (*Server, error) {
	db.AutoMigrate(&User{})
	db.AutoMigrate(&Peering{})

	serkey, err := loadKey(kfile)
	if err != nil {
		return nil, err
	}

	evtman := events.NewEventManager()

	kmgr := indexer.NewKeyManager(didr, serkey)

	repoman := repomgr.NewRepoManager(db, cs, kmgr)
	notifman := notifs.NewNotificationManager(db, repoman.GetRecord)

	ix, err := indexer.NewIndexer(db, notifman, evtman, didr, repoman, false, true)
	if err != nil {
		return nil, err
	}

	s := &Server{
		signingKey:     serkey,
		db:             db,
		cs:             cs,
		notifman:       notifman,
		indexer:        ix,
		plc:            didr,
		events:         evtman,
		repoman:        repoman,
		handleSuffix:   handleSuffix,
		serviceUrl:     serviceUrl,
		jwtSigningKey:  jwtkey,
		enforcePeering: false,
	}

	repoman.SetEventHandler(func(ctx context.Context, evt *repomgr.RepoEvent) {
		if err := ix.HandleRepoEvent(ctx, evt); err != nil {
			log.Errorw("handle repo event failed", "user", evt.User, "err", err)
		}
	})

	//ix.SendRemoteFollow = s.sendRemoteFollow
	ix.CreateExternalUser = s.createExternalUser

	feedgen, err := NewFeedGenerator(db, ix, s.readRecordFunc)
	if err != nil {
		return nil, err
	}

	s.feedgen = feedgen

	go evtman.Run()

	return s, nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}

func (s *Server) handleFedEvent(ctx context.Context, host *Peering, env *events.RepoStreamEvent) error {
	fmt.Printf("[%s] got fed event from %q\n", s.serviceUrl, host.Host)
	switch {
	case env.Append != nil:
		evt := env.Append
		u, err := s.lookupUserByDid(ctx, evt.Repo)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("looking up event user: %w", err)
			}

			subj, err := s.createExternalUser(ctx, evt.Repo)
			if err != nil {
				return err
			}

			u = new(User)
			u.ID = subj.Uid
		}

		var pcid *cid.Cid
		if evt.Prev != "" {
			prev, err := cid.Decode(evt.Prev)
			if err != nil {
				return err
			}

			pcid = &prev
		}

		return s.repoman.HandleExternalUserEvent(ctx, host.ID, u.ID, u.Did, pcid, evt.Blocks)
	default:
		return fmt.Errorf("invalid fed event")
	}
}

func (s *Server) createExternalUser(ctx context.Context, did string) (*models.ActorInfo, error) {
	doc, err := s.plc.GetDocument(ctx, did)
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

	// TODO: the PDS's DID should also be in the service, we could use that to look up?
	var peering Peering
	if err := s.db.Find(&peering, "host = ?", durl.Host).Error; err != nil {
		return nil, err
	}

	c := &xrpc.Client{Host: svc.ServiceEndpoint}

	if peering.ID == 0 {
		pdsdid, err := atproto.HandleResolve(ctx, c, "")
		if err != nil {
			// TODO: failing this shouldnt halt our indexing
			return nil, fmt.Errorf("failed to get accounts config for unrecognized pds: %w", err)
		}

		// TODO: could check other things, a valid response is good enough for now
		peering.Host = svc.ServiceEndpoint
		peering.Did = pdsdid.Did

		if err := s.db.Create(&peering).Error; err != nil {
			return nil, err
		}
	}

	var handle string
	if len(doc.AlsoKnownAs) > 0 {
		hurl, err := url.Parse(doc.AlsoKnownAs[0])
		if err != nil {
			return nil, err
		}

		handle = hurl.Host
	}

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
	subj := &models.ActorInfo{
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

func (s *Server) repoEventToFedEvent(ctx context.Context, evt *repomgr.RepoEvent) (*events.RepoAppend, error) {
	did, err := s.indexer.DidForUser(ctx, evt.User)
	if err != nil {
		return nil, err
	}

	var prevcid string
	if evt.OldRoot != nil {
		prevcid = evt.OldRoot.String()
	}

	out := &events.RepoAppend{
		Prev:   prevcid,
		Blocks: evt.RepoSlice,
		Repo:   did,
		//PrivUid: evt.User,
	}

	return out, nil
}

func (s *Server) readRecordFunc(ctx context.Context, user uint, c cid.Cid) (util.CBOR, error) {
	bs, err := s.cs.ReadOnlySession(user)
	if err != nil {
		return nil, err
	}

	blk, err := bs.Get(ctx, c)
	if err != nil {
		return nil, err
	}

	return util.CborDecodeValue(blk.RawData())
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

	cfg := middleware.JWTConfig{
		Skipper: func(c echo.Context) bool {
			switch c.Path() {
			case "/xrpc/com.atproto.sync.subscribeAllRepos":
				return true
			case "/xrpc/com.atproto.account.create":
				return true
			case "/xrpc/com.atproto.handle.resolve":
				return true
			case "/xrpc/com.atproto.session.create":
				return true
			case "/xrpc/com.atproto.server.getAccountsConfig":
				return true
			case "/xrpc/app.bsky.actor.getProfile":
				fmt.Println("TODO: currently not requiring auth on get profile endpoint")
				return true
			case "/xrpc/com.atproto.sync.getRepo":
				fmt.Println("TODO: currently not requiring auth on get repo endpoint")
				return true
			case "/xrpc/com.atproto.peering.follow", "/events":
				auth := c.Request().Header.Get("Authorization")

				did := c.Request().Header.Get("DID")
				ctx := c.Request().Context()
				ctx = context.WithValue(ctx, "did", did)
				ctx = context.WithValue(ctx, "auth", auth)
				c.SetRequest(c.Request().WithContext(ctx))
				return true
			default:
				return false
			}
		},
		SigningKey: s.jwtSigningKey,
	}

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		fmt.Printf("HANDLER ERROR: (%s) %s\n", ctx.Path(), err)
		ctx.Response().WriteHeader(500)
	}

	e.Use(middleware.JWTWithConfig(cfg), s.userCheckMiddleware)
	s.RegisterHandlersComAtproto(e)
	s.RegisterHandlersAppBsky(e)
	e.GET("/xrpc/com.atproto.sync.subscribeAllRepos", s.EventsHandler)

	return e.Start(listen)
}

type User struct {
	gorm.Model
	Handle      string `gorm:"uniqueIndex"`
	Password    string
	RecoveryKey string
	Email       string
	Did         string `gorm:"uniqueIndex"`
	PDS         uint
}

type RefreshToken struct {
	gorm.Model
	Token string
}

func toTime(i interface{}) (time.Time, error) {
	ival, ok := i.(float64)
	if !ok {
		return time.Time{}, fmt.Errorf("invalid type for timestamp: %T", i)
	}

	return time.Unix(int64(ival), 0), nil
}

func (s *Server) checkTokenValidity(user *gojwt.Token) (string, string, error) {
	claims, ok := user.Claims.(gojwt.MapClaims)
	if !ok {
		return "", "", fmt.Errorf("invalid token claims map")
	}

	iat, ok := claims["iat"]
	if !ok {
		return "", "", fmt.Errorf("iat not set")
	}

	tiat, err := toTime(iat)
	if err != nil {
		return "", "", err
	}

	if tiat.After(time.Now()) {
		return "", "", fmt.Errorf("iat cannot be in the future")
	}

	exp, ok := claims["exp"]
	if !ok {
		return "", "", fmt.Errorf("exp not set")
	}

	texp, err := toTime(exp)
	if err != nil {
		return "", "", err
	}

	if texp.Before(time.Now()) {
		return "", "", fmt.Errorf("token expired")
	}

	did, ok := claims["sub"]
	if !ok {
		return "", "", fmt.Errorf("expected user did in subject")
	}

	didstr, ok := did.(string)
	if !ok {
		return "", "", fmt.Errorf("expected subject to be a string")
	}

	scope, ok := claims["scope"]
	if !ok {
		return "", "", fmt.Errorf("expected scope to be set")
	}

	scopestr, ok := scope.(string)
	if !ok {
		return "", "", fmt.Errorf("expected scope to be a string")
	}

	return scopestr, didstr, nil
}

func (s *Server) lookupUser(ctx context.Context, didorhandle string) (*User, error) {
	if strings.HasPrefix(didorhandle, "did:") {
		return s.lookupUserByDid(ctx, didorhandle)
	}

	return s.lookupUserByHandle(ctx, didorhandle)
}

func (s *Server) lookupUserByDid(ctx context.Context, did string) (*User, error) {
	var u User
	if err := s.db.First(&u, "did = ?", did).Error; err != nil {
		return nil, err
	}

	return &u, nil
}

var ErrNoSuchUser = fmt.Errorf("no such user")

func (s *Server) lookupUserByHandle(ctx context.Context, handle string) (*User, error) {
	var u User
	if err := s.db.Find(&u, "handle = ?", handle).Error; err != nil {
		return nil, err
	}
	fmt.Println("USER: ", handle)
	if u.ID == 0 {
		return nil, ErrNoSuchUser
	}

	return &u, nil
}

func (s *Server) userCheckMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		user, ok := c.Get("user").(*gojwt.Token)
		if !ok {
			return next(c)
		}
		ctx = context.WithValue(ctx, "token", user)

		scope, did, err := s.checkTokenValidity(user)
		if err != nil {
			return fmt.Errorf("invalid token: %w", err)
		}

		u, err := s.lookupUser(ctx, did)
		if err != nil {
			return err
		}

		ctx = context.WithValue(ctx, "authScope", scope)
		ctx = context.WithValue(ctx, "user", u)
		ctx = context.WithValue(ctx, "did", did)

		c.SetRequest(c.Request().WithContext(ctx))
		return next(c)
	}
}

func (s *Server) handleAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		authstr := c.Request().Header.Get("Authorization")
		_ = authstr

		return nil
	}
}

func (s *Server) getUser(ctx context.Context) (*User, error) {
	u, ok := ctx.Value("user").(*User)
	if !ok {
		return nil, fmt.Errorf("auth required")
	}

	//u.Did = ctx.Value("did").(string)

	return u, nil
}

func convertRecordTo(from any, to any) error {
	b, err := json.Marshal(from)
	if err != nil {
		return err
	}

	return json.Unmarshal(b, to)
}

func validateEmail(email string) error {
	_, err := mail.ParseAddress(email)
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) validateHandle(handle string) error {
	if !strings.HasSuffix(handle, s.handleSuffix) {
		return fmt.Errorf("invalid handle")
	}

	if strings.Contains(strings.TrimSuffix(handle, s.handleSuffix), ".") {
		return fmt.Errorf("invalid handle")
	}

	return nil
}

func (s *Server) invalidateToken(ctx context.Context, u *User, tok *jwt.Token) error {
	panic("nyi")
}

type Peering struct {
	gorm.Model
	Host     string
	Did      string
	Approved bool
}

func (s *Server) EventsHandler(c echo.Context) error {
	conn, err := websocket.Upgrade(c.Response().Writer, c.Request(), c.Response().Header(), 1<<10, 1<<10)
	if err != nil {
		return err
	}

	var peering *Peering
	if s.enforcePeering {
		did := c.Request().Header.Get("DID")
		if did != "" {
			if err := s.db.First(peering, "did = ?", did).Error; err != nil {
				return err
			}
		}
	}

	evts, cancel, err := s.events.Subscribe(func(evt *events.RepoStreamEvent) bool {
		if !s.enforcePeering {
			return true
		}
		if peering.ID == 0 {
			return true
		}

		for _, pid := range evt.PrivRelevantPds {
			if pid == peering.ID {
				return true
			}
		}

		return false
	}, nil)
	if err != nil {
		return err
	}
	defer cancel()

	header := events.EventHeader{Op: events.EvtKindRepoAppend}
	for evt := range evts {
		wc, err := conn.NextWriter(websocket.BinaryMessage)
		if err != nil {
			return err
		}

		var obj util.CBOR

		switch {
		case evt.Append != nil:
			header.Op = events.EvtKindRepoAppend
			obj = evt.Append
		case evt.Info != nil:
			header.Op = events.EvtKindInfoFrame
			obj = evt.Info
		case evt.Error != nil:
			header.Op = events.EvtKindErrorFrame
			obj = evt.Error
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
