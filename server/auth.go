package schemagen

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/lestrrat-go/jwx/jwa"
	jwt "github.com/lestrrat-go/jwx/jwt"
	"github.com/whyrusleeping/gosky/xrpc"
)

const actorUserDeclarationCid = "bafyreid27zk7lbis4zw5fz4podbvbs4fc5ivwji3dmrwa6zggnj4bnd57u"

func makeToken(subject string, scope string, exp time.Time) jwt.Token {
	tok := jwt.New()
	tok.Set("scope", scope)
	tok.Set("sub", subject)
	tok.Set("iat", time.Now().Unix())
	tok.Set("exp", exp.Unix())

	return tok

}

func (s *Server) createAuthTokenForUser(ctx context.Context, handle, did string) (*xrpc.AuthInfo, error) {
	accessTok := makeToken(did, "com.atproto.access", time.Now().Add(24*time.Hour))
	refreshTok := makeToken(did, "com.atproto.refresh", time.Now().Add(7*24*time.Hour))

	rval := make([]byte, 10)
	rand.Read(rval)
	refreshTok.Set("jti", base64.StdEncoding.EncodeToString(rval))

	accSig, err := jwt.Sign(accessTok, jwa.HS256, s.signingKey)
	if err != nil {
		return nil, err
	}

	refSig, err := jwt.Sign(refreshTok, jwa.HS256, s.signingKey)
	if err != nil {
		return nil, err
	}

	return &xrpc.AuthInfo{
		AccessJwt:  string(accSig),
		RefreshJwt: string(refSig),
		Handle:     handle,
		Did:        did,
	}, nil
}

func (s *Server) createCrossServerAuthToken(ctx context.Context, otherpds string) (*xrpc.AuthInfo, error) {
	accessTok := makeToken(otherpds, "com.atproto.federation", time.Now().Add(24*time.Hour))

	rval := make([]byte, 10)
	rand.Read(rval)

	accSig, err := jwt.Sign(accessTok, jwa.HS256, s.signingKey)
	if err != nil {
		return nil, err
	}

	return &xrpc.AuthInfo{
		AccessJwt: string(accSig),
	}, nil
}
