package schemagen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"fmt"
	"os"

	"github.com/golang-jwt/jwt"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	jwk "github.com/lestrrat-go/jwx/jwk"
	"github.com/whyrusleeping/gosky/carstore"
	"gorm.io/gorm"
)

type Server struct {
	db         *gorm.DB
	cs         *carstore.CarStore
	signingKey []byte

	fakeDid *FakeDid
}

func NewServer(db *gorm.DB, cs *carstore.CarStore, kfile string) (*Server, error) {
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

	serkey := elliptic.Marshal(spk.Curve, spk.X, spk.Y)

	db.AutoMigrate(&User{})

	return &Server{
		signingKey: serkey,
		db:         db,
		cs:         cs,
		fakeDid:    NewFakeDid(db),
	}, nil
}

func (s *Server) RunAPI(listen string) error {
	e := echo.New()

	cfg := middleware.JWTConfig{
		KeyFunc:    s.getKey,
		SigningKey: s.signingKey,
	}

	e.HTTPErrorHandler = func(err error, ctx echo.Context) {
		fmt.Println("HANDLER ERROR: ", err)
	}

	s.RegisterHandlersComAtproto(e.Group(""))
	s.RegisterHandlersAppBsky(e.Group("", middleware.JWTWithConfig(cfg)))

	return e.Start(listen)
}

type User struct {
	gorm.Model
	Handle      string
	Password    string
	RecoveryKey string
	DID         string
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
