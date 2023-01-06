package schemagen

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
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
	jwk "github.com/lestrrat-go/jwx/jwk"
	jwt "github.com/lestrrat-go/jwx/jwt"
	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
	appbskytypes "github.com/whyrusleeping/gosky/api/bsky"
	"github.com/whyrusleeping/gosky/carstore"
	"github.com/whyrusleeping/gosky/lex/util"
	"github.com/whyrusleeping/gosky/repomgr"
	"github.com/whyrusleeping/gosky/xrpc"
	"gorm.io/gorm"
)

type Server struct {
	db         *gorm.DB
	cs         *carstore.CarStore
	repoman    *repomgr.RepoManager
	feedgen    *FeedGenerator
	notifman   *NotificationManager
	indexer    *Indexer
	events     *EventManager
	signingKey []byte

	handleSuffix string

	fakeDid *FakeDid
}

const UserActorDeclCid = "bafyreid27zk7lbis4zw5fz4podbvbs4fc5ivwji3dmrwa6zggnj4bnd57u"
const UserActorDeclType = "app.bsky.system.actorUser"

func NewServer(db *gorm.DB, cs *carstore.CarStore, kfile string, handleSuffix string) (*Server, error) {
	db.AutoMigrate(&User{})

	serkey, err := loadKey(kfile)
	if err != nil {
		return nil, err
	}

	evtman := NewEventManager()

	repoman := repomgr.NewRepoManager(db, cs)
	notifman := NewNotificationManager(db, repoman)

	ix, err := NewIndexer(db, notifman, evtman)
	if err != nil {
		return nil, err
	}

	s := &Server{
		signingKey:   serkey,
		db:           db,
		cs:           cs,
		notifman:     notifman,
		indexer:      ix,
		fakeDid:      NewFakeDid(db),
		events:       evtman,
		repoman:      repoman,
		handleSuffix: handleSuffix,
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

	return s, nil
}

func (s *Server) repoEventToFedEvent(ctx context.Context, evt *repomgr.RepoEvent) (*Event, error) {
	out := &Event{
		CarSlice: evt.RepoSlice,
	}

	switch evt.Kind {
	case repomgr.EvtKindCreateRecord:
		out.Kind = EvtKindUpdateRecord
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

func loadKey(kfile string) ([]byte, error) {
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
	return elliptic.Marshal(spk.Curve, spk.X, spk.Y), nil
}

func (s *Server) RunAPI(listen string) error {
	e := echo.New()
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
			default:
				return false
			}
		},
		//KeyFunc:    s.getKey,
		SigningKey: s.signingKey,
	}

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		fmt.Println("HANDLER ERROR: ", err)
	}

	e.Use(middleware.JWTWithConfig(cfg), s.userCheckMiddleware)
	s.RegisterHandlersComAtproto(e)
	s.RegisterHandlersAppBsky(e)

	return e.Start(listen)
}

type User struct {
	gorm.Model
	Handle      string `gorm:"uniqueIndex"`
	Password    string
	RecoveryKey string
	Email       string
	DID         string `gorm:"uniqueIndex"`
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
	var didEntry FakeDidMapping
	if err := s.db.First(&didEntry, "did = ?", did).Error; err != nil {
		return nil, err
	}

	var u User
	if err := s.db.First(&u, "handle = ?", didEntry.Handle).Error; err != nil {
		return nil, err
	}

	return &u, nil
}

var ErrNoSuchUser = fmt.Errorf("no such user")

func (s *Server) lookupUserByHandle(ctx context.Context, handle string) (*User, error) {
	var didEntry FakeDidMapping
	if err := s.db.Find(&didEntry, "handle = ?", handle).Error; err != nil {
		return nil, err
	}
	if didEntry.ID == 0 {
		return nil, ErrNoSuchUser
	}

	var u User
	if err := s.db.Find(&u, "handle = ?", didEntry.Handle).Error; err != nil {
		return nil, err
	}
	if u.ID == 0 {
		return nil, ErrNoSuchUser
	}

	u.DID = didEntry.Did

	return &u, nil
}

func (s *Server) userCheckMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		user, ok := c.Get("user").(*gojwt.Token)
		if !ok {
			return next(c)
		}

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
		ctx = context.WithValue(ctx, "token", user)

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

func (s *Server) getKey(token *jwt.Token) (interface{}, error) {
	fmt.Println("token: ", token)

	return nil, nil
}

func (s *Server) getUser(ctx context.Context) (*User, error) {
	u, ok := ctx.Value("user").(*User)
	if !ok {
		return nil, fmt.Errorf("auth required")
	}

	u.DID = ctx.Value("did").(string)

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
	Approved bool
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
		Host: peering.Host,
		Auth: auth,
	}

	if err := comatprototypes.PeeringFollow(ctx, c, &comatprototypes.PeeringFollow_Input{
		Users: []string{followed},
	}); err != nil {
		return err
	}

	return nil
}
