package schemagen

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt"
	"github.com/ipfs/go-cid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	jwk "github.com/lestrrat-go/jwx/jwk"
	appbskytypes "github.com/whyrusleeping/gosky/api/bsky"
	"github.com/whyrusleeping/gosky/carstore"
	"github.com/whyrusleeping/gosky/lex/util"
	"github.com/whyrusleeping/gosky/repomgr"
	"gorm.io/gorm"
)

type Server struct {
	db         *gorm.DB
	cs         *carstore.CarStore
	repoman    *repomgr.RepoManager
	feedgen    *FeedGenerator
	signingKey []byte

	handleSuffix string

	fakeDid *FakeDid
}

const UserActorDeclCid = "bafyreid27zk7lbis4zw5fz4podbvbs4fc5ivwji3dmrwa6zggnj4bnd57u"
const UserActorDeclType = "app.bsky.system.actorUser"

func NewServer(db *gorm.DB, cs *carstore.CarStore, kfile string) (*Server, error) {
	db.AutoMigrate(&User{})

	serkey, err := loadKey(kfile)
	if err != nil {
		return nil, err
	}

	s := &Server{
		signingKey:   serkey,
		db:           db,
		cs:           cs,
		fakeDid:      NewFakeDid(db),
		handleSuffix: ".pdstest",
	}

	feedgen, err := NewFeedGenerator(db, s.readRecordFunc)
	if err != nil {
		return nil, err
	}

	s.feedgen = feedgen
	s.repoman = repomgr.NewRepoManager(db, cs, feedgen.HandleRepoEvent)

	return s, nil
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

	cfg := middleware.JWTConfig{
		Skipper: func(c echo.Context) bool {
			fmt.Println("skipper: ", c.Path())
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

func (s *Server) checkTokenValidity(user *jwt.Token) (string, string, error) {
	claims, ok := user.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", fmt.Errorf("bad claims table")
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
	if err := s.db.First(&didEntry, "handle = ?", handle).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNoSuchUser
		}
		return nil, err
	}

	var u User
	if err := s.db.First(&u, "handle = ?", didEntry.Handle).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNoSuchUser
		}
		return nil, err
	}

	u.DID = didEntry.Did

	return &u, nil
}

func (s *Server) userCheckMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx := c.Request().Context()

		user, ok := c.Get("user").(*jwt.Token)
		if !ok {
			return next(c)
		}

		scope, did, err := s.checkTokenValidity(user)
		if err != nil {
			return fmt.Errorf("invalid token: %w", err)
		}

		_ = scope

		u, err := s.lookupUser(ctx, did)
		if err != nil {
			return err
		}

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
