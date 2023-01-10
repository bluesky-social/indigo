package schemagen

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log"
	"net/mail"
	"os"
	"strings"
	"time"

	gojwt "github.com/golang-jwt/jwt"
	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/lestrrat-go/jwx/jwa"
	jwk "github.com/lestrrat-go/jwx/jwk"
	jwt "github.com/lestrrat-go/jwx/jwt"
	"github.com/whyrusleeping/go-did"
	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
	appbskytypes "github.com/whyrusleeping/gosky/api/bsky"
	"github.com/whyrusleeping/gosky/carstore"
	"github.com/whyrusleeping/gosky/key"
	"github.com/whyrusleeping/gosky/lex/util"
	"github.com/whyrusleeping/gosky/repomgr"
	"github.com/whyrusleeping/gosky/xrpc"
	"gorm.io/gorm"
)

type Server struct {
	db            *gorm.DB
	cs            *carstore.CarStore
	repoman       *repomgr.RepoManager
	feedgen       *FeedGenerator
	notifman      *NotificationManager
	indexer       *Indexer
	events        *EventManager
	signingKey    *key.Key
	echo          *echo.Echo
	jwtSigningKey []byte

	handleSuffix string
	serviceUrl   string

	plc PLCClient
}

const UserActorDeclCid = "bafyreid27zk7lbis4zw5fz4podbvbs4fc5ivwji3dmrwa6zggnj4bnd57u"
const UserActorDeclType = "app.bsky.system.actorUser"

type PLCClient interface {
	GetDocument(ctx context.Context, didstr string) (*did.Document, error)
	CreateDID(ctx context.Context, sigkey *key.Key, recovery string, handle string, service string) (string, error)
}

func NewServer(db *gorm.DB, cs *carstore.CarStore, kfile string, handleSuffix, serviceUrl string, didr PLCClient, jwtkey []byte) (*Server, error) {
	db.AutoMigrate(&User{})
	db.AutoMigrate(&Peering{})

	serkey, err := loadKey(kfile)
	if err != nil {
		return nil, err
	}

	evtman := NewEventManager()

	repoman := repomgr.NewRepoManager(db, cs)
	notifman := NewNotificationManager(db, repoman)

	ix, err := NewIndexer(db, notifman, evtman, didr)
	if err != nil {
		return nil, err
	}

	s := &Server{
		signingKey:    serkey,
		db:            db,
		cs:            cs,
		notifman:      notifman,
		indexer:       ix,
		plc:           didr,
		events:        evtman,
		repoman:       repoman,
		handleSuffix:  handleSuffix,
		serviceUrl:    serviceUrl,
		jwtSigningKey: jwtkey,
	}

	repoman.SetEventHandler(func(ctx context.Context, evt *repomgr.RepoEvent) {
		ix.HandleRepoEvent(ctx, evt)

		fe, err := s.repoEventToFedEvent(context.TODO(), evt)
		if err != nil {
			log.Println("event conversion error: ", err)
			return
		}
		if fe != nil {
			if err := evtman.AddEvent(fe); err != nil {
				log.Println("failed to push event: ", err)
			}
		}
	})

	ix.sendRemoteFollow = s.sendRemoteFollow

	feedgen, err := NewFeedGenerator(db, ix, s.readRecordFunc)
	if err != nil {
		return nil, err
	}

	s.feedgen = feedgen

	go evtman.Run()

	return s, nil
}

func (s *Server) handleFedEvent(ctx context.Context, host *Peering, evt *Event) error {
	fmt.Printf("[%s] got fed event from %q: %s\n", s.serviceUrl, host.Host, evt.Kind)
	switch evt.Kind {
	case EvtKindCreateRecord:
		u, err := s.lookupUserByDid(ctx, evt.User)
		if err != nil {
			return err
		}

		return s.repoman.HandleExternalUserEvent(ctx, host.ID, repomgr.EvtKindCreateRecord, u.ID, evt.Collection, evt.Rkey, evt.CarSlice)
	case EvtKindUpdateRecord:
	default:
		return fmt.Errorf("unrecognized fed event kind: %q", evt.Kind)
	}
	return nil
}

func (s *Server) repoEventToFedEvent(ctx context.Context, evt *repomgr.RepoEvent) (*Event, error) {
	out := &Event{
		CarSlice: evt.RepoSlice,
	}

	switch evt.Kind {
	case repomgr.EvtKindCreateRecord:
		out.Kind = EvtKindCreateRecord
	case repomgr.EvtKindUpdateRecord:
		out.Kind = EvtKindUpdateRecord
	case repomgr.EvtKindInitActor:
		return nil, nil
	default:
		return nil, fmt.Errorf("unrecognized repo event kind: %q", evt.Kind)
	}

	did, err := s.indexer.didForUser(ctx, evt.User)
	if err != nil {
		return nil, err
	}

	out.uid = evt.User
	out.User = did
	out.Collection = evt.Collection
	out.Rkey = evt.Rkey

	return out, nil
}

func (s *Server) readRecordFunc(ctx context.Context, user uint, c cid.Cid) (any, error) {
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

func loadKey(kfile string) (*key.Key, error) {
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

	return &key.Key{
		Raw:  &spk,
		Type: string(curve.(jwa.EllipticCurveAlgorithm)),
	}, nil
}

func (s *Server) RunAPI(listen string) error {
	e := echo.New()
	s.echo = e
	e.Use(middleware.LoggerWithConfig(middleware.LoggerConfig{
		Format: "method=${method}, uri=${uri}, status=${status} latency=${latency_human}\n",
	}))

	cfg := middleware.JWTConfig{
		Skipper: func(c echo.Context) bool {
			switch c.Path() {
			case "/xrpc/com.atproto.account.create":
				return true
			case "/xrpc/com.atproto.session.create":
				return true
			case "/xrpc/com.atproto.server.getAccountsConfig":
				return true
			case "/xrpc/app.bsky.actor.getProfile":
				fmt.Println("TODO: currently not requiring auth on get profile endpoint")
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
		fmt.Println("HANDLER ERROR: ", err)
	}

	e.Use(middleware.JWTWithConfig(cfg), s.userCheckMiddleware)
	s.RegisterHandlersComAtproto(e)
	s.RegisterHandlersAppBsky(e)
	e.GET("/events", s.EventsHandler)

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

func infoToActorRef(ai *ActorInfo) *appbskytypes.ActorRef_WithInfo {
	return &appbskytypes.ActorRef_WithInfo{
		Declaration: &appbskytypes.SystemDeclRef{
			Cid:       ai.DeclRefCid,
			ActorType: ai.Type,
		},
		Handle:      ai.Handle,
		DisplayName: &ai.DisplayName,
		Did:         ai.Did,
	}
}

func (s *Server) invalidateToken(ctx context.Context, u *User, tok *jwt.Token) error {
	panic("nyi")
}

func (s *Server) CreateScene(ctx context.Context, u *User, handle string, recovery *string) (interface{}, error) {
	panic("nyi")
}

type Peering struct {
	gorm.Model
	Host     string
	Did      string
	Approved bool
}

func (s *Server) HackAddPeering(host string, did string) error {
	// TODO: this method is just for proof of concept since i'm punting on
	// figuring out how the peering arrangements get set up.

	if err := s.db.Create(&Peering{
		Host:     host,
		Did:      did,
		Approved: true,
	}).Error; err != nil {
		return err
	}

	if err := s.SubscribeToPds(context.TODO(), host); err != nil {
		return err
	}

	return nil
}

func (s *Server) sendRemoteFollow(ctx context.Context, followed string, followedPDS uint) error {
	var peering Peering
	if err := s.db.First(&peering, "id = ?", followedPDS).Error; err != nil {
		return fmt.Errorf("failed to find followed users pds: %w", err)
	}

	auth, err := s.createCrossServerAuthToken(ctx, peering.Host)
	if err != nil {
		return err
	}

	c := &xrpc.Client{
		Host: "http://" + peering.Host, // TODO: maybe its correct to just put the protocol prefix in the database
		Auth: auth,
	}

	if err := comatprototypes.PeeringFollow(ctx, c, &comatprototypes.PeeringFollow_Input{
		Users: []string{followed},
	}); err != nil {
		return err
	}

	return nil
}

func (s *Server) AddRemoteFollow(ctx context.Context, opdsdid string, u string) error {
	var peering Peering
	if err := s.db.First(&peering, "did = ?", opdsdid).Error; err != nil {
		return err
	}

	uu, err := s.lookupUser(ctx, u)
	if err != nil {
		return err
	}

	if err := s.db.Create(&ExternalFollow{
		PDS: peering.ID,
		Uid: uu.ID,
	}).Error; err != nil {
		return err
	}

	return nil
}

func (s *Server) peerHasFollow(ctx context.Context, peer uint, user uint) (bool, error) {
	var extfollow ExternalFollow
	if err := s.db.Debug().Find(&extfollow, "pds = ? AND uid = ?", peer, user).Error; err != nil {
		return false, err
	}

	return extfollow.ID != 0, nil
}
