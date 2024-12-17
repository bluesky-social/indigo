package pds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/api/atproto"
	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/carstore"
	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/indexer"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/models"
	"github.com/bluesky-social/indigo/notifs"
	pdsdata "github.com/bluesky-social/indigo/pds/data"
	"github.com/bluesky-social/indigo/plc"
	"github.com/bluesky-social/indigo/repomgr"
	"github.com/bluesky-social/indigo/util"
	bsutil "github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	gojwt "github.com/golang-jwt/jwt"
	"github.com/gorilla/websocket"
	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/whyrusleeping/go-did"
	"gorm.io/gorm"
)

type Server struct {
	db             *gorm.DB
	cs             carstore.CarStore
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

	log *slog.Logger
}

// serverListenerBootTimeout is how long to wait for the requested server socket
// to become available for use. This is an arbitrary timeout that should be safe
// on any platform, but there's no great way to weave this timeout without
// adding another parameter to the (at time of writing) long signature of
// NewServer.
const serverListenerBootTimeout = 5 * time.Second

func NewServer(db *gorm.DB, cs carstore.CarStore, serkey *did.PrivKey, handleSuffix, serviceUrl string, didr plc.PLCClient, jwtkey []byte) (*Server, error) {
	db.AutoMigrate(&User{})
	db.AutoMigrate(&Peering{})

	evtman := events.NewEventManager(events.NewMemPersister())

	kmgr := indexer.NewKeyManager(didr, serkey)

	repoman := repomgr.NewRepoManager(cs, kmgr)
	notifman := notifs.NewNotificationManager(db, repoman.GetRecord)

	rf := indexer.NewRepoFetcher(db, repoman, 10)

	ix, err := indexer.NewIndexer(db, notifman, evtman, didr, rf, false, true, true)
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

		log: slog.Default().With("system", "pds"),
	}

	repoman.SetEventHandler(func(ctx context.Context, evt *repomgr.RepoEvent) {
		if err := ix.HandleRepoEvent(ctx, evt); err != nil {
			s.log.Error("handle repo event failed", "user", evt.User, "err", err)
		}
	}, true)

	//ix.SendRemoteFollow = s.sendRemoteFollow
	ix.CreateExternalUser = s.createExternalUser

	feedgen, err := NewFeedGenerator(db, ix, s.readRecordFunc, s.log)
	if err != nil {
		return nil, err
	}

	s.feedgen = feedgen

	return s, nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}

func (s *Server) handleFedEvent(ctx context.Context, host *Peering, env *events.XRPCStreamEvent) error {
	fmt.Printf("[%s] got fed event from %q\n", s.serviceUrl, host.Host)
	switch {
	case env.RepoCommit != nil:
		evt := env.RepoCommit
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

		return s.repoman.HandleExternalUserEvent(ctx, host.ID, u.ID, u.Did, evt.Since, evt.Rev, evt.Blocks, evt.Ops)
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
		cfg, err := atproto.ServerDescribeServer(ctx, c)
		if err != nil {
			// TODO: failing this should not halt our indexing
			return nil, fmt.Errorf("failed to check unrecognized pds: %w", err)
		}

		// since handles can be anything, checking against this list does not matter...
		_ = cfg

		// TODO: could check other things, a valid response is good enough for now
		peering.Host = svc.ServiceEndpoint

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
		Handle:      sql.NullString{String: handle, Valid: true},
		DisplayName: "missing display name",
		Did:         did,
		Type:        "",
		PDS:         peering.ID,
	}
	if err := s.db.Create(subj).Error; err != nil {
		return nil, err
	}

	return subj, nil
}

func (s *Server) repoEventToFedEvent(ctx context.Context, evt *repomgr.RepoEvent) (*comatproto.SyncSubscribeRepos_Commit, error) {
	did, err := s.indexer.DidForUser(ctx, evt.User)
	if err != nil {
		return nil, err
	}

	out := &comatproto.SyncSubscribeRepos_Commit{
		Prev:   (*lexutil.LexLink)(evt.OldRoot),
		Blocks: evt.RepoSlice,
		Repo:   did,
		Time:   time.Now().Format(bsutil.ISO8601),
		//PrivUid: evt.User,
	}

	for _, op := range evt.Ops {
		out.Ops = append(out.Ops, &comatproto.SyncSubscribeRepos_RepoOp{
			Path:   op.Collection + "/" + op.Rkey,
			Action: string(op.Kind),
			Cid:    (*lexutil.LexLink)(op.RecCid),
		})
	}

	return out, nil
}

func (s *Server) readRecordFunc(ctx context.Context, user models.Uid, c cid.Cid) (lexutil.CBOR, error) {
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

func (s *Server) RunAPI(addr string) error {
	var lc net.ListenConfig
	ctx, cancel := context.WithTimeout(context.Background(), serverListenerBootTimeout)
	defer cancel()

	li, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return s.RunAPIWithListener(li)
}

func (s *Server) RunAPIWithListener(listen net.Listener) error {
	e := echo.New()
	s.echo = e
	e.HideBanner = true
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
	}))

	cfg := middleware.JWTConfig{
		Skipper: func(c echo.Context) bool {
			switch c.Path() {
			case "/xrpc/_health":
				return true
			case "/xrpc/com.atproto.sync.subscribeRepos":
				return true
			case "/xrpc/com.atproto.account.create":
				return true
			case "/xrpc/com.atproto.identity.resolveHandle":
				return true
			case "/xrpc/com.atproto.server.createAccount":
				return true
			case "/xrpc/com.atproto.server.createSession":
				return true
			case "/xrpc/com.atproto.server.describeServer":
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
			case "/.well-known/atproto-did":
				return true
			case "/takedownRepo":
				return true
			case "/suspendRepo":
				return true
			case "/deactivateRepo":
				return true
			case "/reactivateRepo":
				return true
			default:
				return false
			}
		},
		SigningKey: s.jwtSigningKey,
	}

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		fmt.Printf("PDS HANDLER ERROR: (%s) %s\n", ctx.Path(), err)

		// TODO: need to properly figure out where http error codes for error
		// types get decided. This spot is reasonable, but maybe a bit weird.
		// reviewers, please advise
		if errors.Is(err, ErrNoSuchUser) {
			ctx.Response().WriteHeader(404)
			return
		}

		ctx.Response().WriteHeader(500)
	}

	e.GET("/takedownRepo", func(c echo.Context) error {
		ctx := c.Request().Context()
		did := c.QueryParam("did")
		if did == "" {
			return fmt.Errorf("missing did")
		}

		if err := s.TakedownRepo(ctx, did); err != nil {
			return err
		}

		return c.String(200, "ok")
	})

	e.GET("/suspendRepo", func(c echo.Context) error {
		ctx := c.Request().Context()
		did := c.QueryParam("did")
		if did == "" {
			return fmt.Errorf("missing did")
		}

		if err := s.SuspendRepo(ctx, did); err != nil {
			return err
		}

		return c.String(200, "ok")
	})

	e.GET("/deactivateRepo", func(c echo.Context) error {
		ctx := c.Request().Context()
		did := c.QueryParam("did")
		if did == "" {
			return fmt.Errorf("missing did")
		}

		if err := s.DeactivateRepo(ctx, did); err != nil {
			return err
		}

		return c.String(200, "ok")
	})

	e.GET("/reactivateRepo", func(c echo.Context) error {
		ctx := c.Request().Context()
		did := c.QueryParam("did")
		if did == "" {
			return fmt.Errorf("missing did")
		}

		if err := s.ReactivateRepo(ctx, did); err != nil {
			return err
		}

		return c.String(200, "ok")
	})

	e.Use(middleware.JWTWithConfig(cfg), s.userCheckMiddleware)
	s.RegisterHandlersComAtproto(e)

	e.GET("/xrpc/com.atproto.sync.subscribeRepos", s.EventsHandler)
	e.GET("/xrpc/_health", s.HandleHealthCheck)
	e.GET("/.well-known/atproto-did", s.HandleResolveDid)

	// In order to support booting on random ports in tests, we need to tell the
	// Echo instance it's already got a port, and then use its StartServer
	// method to re-use that listener.
	e.Listener = listen
	srv := &http.Server{}
	return e.StartServer(srv)
}

type HealthStatus struct {
	Status  string `json:"status"`
	Message string `json:"msg,omitempty"`
}

func (s *Server) HandleHealthCheck(c echo.Context) error {
	if err := s.db.Exec("SELECT 1").Error; err != nil {
		s.log.Error("healthcheck can't connect to database", "err", err)
		return c.JSON(500, HealthStatus{Status: "error", Message: "can't connect to database"})
	} else {
		return c.JSON(200, HealthStatus{Status: "ok"})
	}
}

func (s *Server) HandleResolveDid(c echo.Context) error {
	ctx := c.Request().Context()

	handle := c.Request().Host
	if hh := c.Request().Header.Get("Host"); hh != "" {
		handle = hh
	}

	u, err := s.lookupUserByHandle(ctx, handle)
	if err != nil {
		return fmt.Errorf("resolving %q: %w", handle, err)
	}

	return c.String(200, u.Did)
}

type User = pdsdata.User

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

func (s *Server) getUser(ctx context.Context) (*User, error) {
	u, ok := ctx.Value("user").(*User)
	if !ok {
		return nil, fmt.Errorf("auth required")
	}

	//u.Did = ctx.Value("did").(string)

	return u, nil
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

type Peering = pdsdata.Peering

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

	ctx := c.Request().Context()

	ident := c.RealIP() + "-" + c.Request().UserAgent()

	evts, cancel, err := s.events.Subscribe(ctx, ident, func(evt *events.XRPCStreamEvent) bool {
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

	header := events.EventHeader{Op: events.EvtKindMessage}
	for evt := range evts {
		wc, err := conn.NextWriter(websocket.BinaryMessage)
		if err != nil {
			return err
		}

		var obj lexutil.CBOR

		switch {
		case evt.Error != nil:
			header.Op = events.EvtKindErrorFrame
			obj = evt.Error
		case evt.RepoCommit != nil:
			header.MsgType = "#commit"
			obj = evt.RepoCommit
		case evt.RepoHandle != nil:
			header.MsgType = "#handle"
			obj = evt.RepoHandle
		case evt.RepoIdentity != nil:
			header.MsgType = "#identity"
			obj = evt.RepoIdentity
		case evt.RepoAccount != nil:
			header.MsgType = "#account"
			obj = evt.RepoAccount
		case evt.RepoInfo != nil:
			header.MsgType = "#info"
			obj = evt.RepoInfo
		case evt.RepoMigrate != nil:
			header.MsgType = "#migrate"
			obj = evt.RepoMigrate
		case evt.RepoTombstone != nil:
			header.MsgType = "#tombstone"
			obj = evt.RepoTombstone
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

func (s *Server) UpdateUserHandle(ctx context.Context, u *User, handle string) error {
	if u.Handle == handle {
		// no change? move on
		s.log.Warn("attempted to change handle to current handle", "did", u.Did, "handle", handle)
		return nil
	}

	_, err := s.indexer.LookupUserByHandle(ctx, handle)
	if err == nil {
		return fmt.Errorf("handle %q is already in use", handle)
	}

	if err := s.plc.UpdateUserHandle(ctx, u.Did, handle); err != nil {
		return fmt.Errorf("failed to update users handle on plc: %w", err)
	}

	if err := s.db.Model(models.ActorInfo{}).Where("uid = ?", u.ID).UpdateColumn("handle", handle).Error; err != nil {
		return fmt.Errorf("failed to update handle: %w", err)
	}

	if err := s.db.Model(User{}).Where("id = ?", u.ID).UpdateColumn("handle", handle).Error; err != nil {
		return fmt.Errorf("failed to update handle: %w", err)
	}

	if err := s.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoHandle: &comatproto.SyncSubscribeRepos_Handle{
			Did:    u.Did,
			Handle: handle,
			Time:   time.Now().Format(util.ISO8601),
		},
	}); err != nil {
		return fmt.Errorf("failed to push event: %s", err)
	}

	// Also push an Identity event
	if err := s.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoIdentity: &comatproto.SyncSubscribeRepos_Identity{
			Did:  u.Did,
			Time: time.Now().Format(util.ISO8601),
		},
	}); err != nil {
		return fmt.Errorf("failed to push event: %s", err)
	}

	return nil
}

func (s *Server) TakedownRepo(ctx context.Context, did string) error {
	// Push an Account event
	if err := s.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoAccount: &comatproto.SyncSubscribeRepos_Account{
			Did:    did,
			Active: false,
			Status: &events.AccountStatusTakendown,
			Time:   time.Now().Format(util.ISO8601),
		},
	}); err != nil {
		return fmt.Errorf("failed to push event: %s", err)
	}

	return nil
}

func (s *Server) SuspendRepo(ctx context.Context, did string) error {
	// Push an Account event
	if err := s.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoAccount: &comatproto.SyncSubscribeRepos_Account{
			Did:    did,
			Active: false,
			Status: &events.AccountStatusSuspended,
			Time:   time.Now().Format(util.ISO8601),
		},
	}); err != nil {
		return fmt.Errorf("failed to push event: %s", err)
	}

	return nil
}

func (s *Server) DeactivateRepo(ctx context.Context, did string) error {
	// Push an Account event
	if err := s.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoAccount: &comatproto.SyncSubscribeRepos_Account{
			Did:    did,
			Active: false,
			Status: &events.AccountStatusDeactivated,
			Time:   time.Now().Format(util.ISO8601),
		},
	}); err != nil {
		return fmt.Errorf("failed to push event: %s", err)
	}

	return nil
}

func (s *Server) ReactivateRepo(ctx context.Context, did string) error {
	// Push an Account event
	if err := s.events.AddEvent(ctx, &events.XRPCStreamEvent{
		RepoAccount: &comatproto.SyncSubscribeRepos_Account{
			Did:    did,
			Active: true,
			Status: &events.AccountStatusActive,
			Time:   time.Now().Format(util.ISO8601),
		},
	}); err != nil {
		return fmt.Errorf("failed to push event: %s", err)
	}

	return nil
}

func (s *Server) Repoman() *repomgr.RepoManager {
	return s.repoman
}
