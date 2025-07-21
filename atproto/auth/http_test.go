package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func webHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	w.WriteHeader(http.StatusOK)
	did, ok := ctx.Value("did").(syntax.DID)
	if ok {
		w.Write([]byte(did.String()))
	} else {
		w.Write([]byte("hello world"))
	}
}

func TestAdminAuthMiddleware(t *testing.T) {
	assert := assert.New(t)

	pw1 := "secret123"
	pw2 := "secret789"

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	middle := AdminAuthMiddleware(webHome, []string{pw1, pw2})

	{
		resp := httptest.NewRecorder()
		middle(resp, req)
		assert.Equal(http.StatusUnauthorized, resp.Code)
	}

	{
		resp := httptest.NewRecorder()
		req.SetBasicAuth("admin", pw1)
		middle(resp, req)
		assert.Equal(http.StatusOK, resp.Code)
	}

	{
		resp := httptest.NewRecorder()
		req.SetBasicAuth("admin", pw2)
		middle(resp, req)
		assert.Equal(http.StatusOK, resp.Code)
	}

	{
		resp := httptest.NewRecorder()
		req.SetBasicAuth("wrong", pw2)
		middle(resp, req)
		assert.Equal(http.StatusUnauthorized, resp.Code)
	}

	{
		resp := httptest.NewRecorder()
		req.SetBasicAuth("admin", "wrong")
		middle(resp, req)
		assert.Equal(http.StatusUnauthorized, resp.Code)
	}
}

func TestServiceAuthMiddleware(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	iss := syntax.DID("did:example:iss")
	aud := "did:example:aud#svc"
	lxm := syntax.NSID("com.example.api")

	priv, err := crypto.GeneratePrivateKeyP256()
	require.NoError(err)
	pub, err := priv.PublicKey()
	require.NoError(err)

	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: iss,
		Keys: map[string]identity.VerificationMethod{
			"atproto": {
				Type:               "Multikey",
				PublicKeyMultibase: pub.Multibase(),
			},
		},
	})

	v := ServiceAuthValidator{
		Audience: aud,
		Dir:      &dir,
	}

	{
		// optional middleware, no auth
		req := httptest.NewRequest(http.MethodGet, "/xrpc/com.example.api", nil)
		middle := v.Middleware(webHome, false)
		resp := httptest.NewRecorder()
		middle(resp, req)
		assert.Equal(http.StatusOK, resp.Code)
		assert.Equal("hello world", string(resp.Body.Bytes()))
	}

	{
		// mandatory middleware, no auth
		req := httptest.NewRequest(http.MethodGet, "/xrpc/com.example.api", nil)
		middle := v.Middleware(webHome, true)
		resp := httptest.NewRecorder()
		middle(resp, req)
		assert.Equal(http.StatusUnauthorized, resp.Code)
	}

	{
		// mandatory middleware, valid auth
		tok, err := SignServiceAuth(iss, aud, time.Minute, &lxm, priv)
		require.NoError(err)
		req := httptest.NewRequest(http.MethodGet, "/xrpc/com.example.api", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		middle := v.Middleware(webHome, true)
		resp := httptest.NewRecorder()
		middle(resp, req)
		assert.Equal(http.StatusOK, resp.Code)
		assert.Equal(iss.String(), string(resp.Body.Bytes()))
	}

	{
		// mangled header
		req := httptest.NewRequest(http.MethodGet, "/xrpc/com.example.api", nil)
		req.Header.Set("Authorization", "Bearer dummy")
		middle := v.Middleware(webHome, false)
		resp := httptest.NewRecorder()
		middle(resp, req)
		assert.Equal(http.StatusUnauthorized, resp.Code)
	}

	{
		// wrong path
		tok, err := SignServiceAuth(iss, aud, time.Minute, &lxm, priv)
		require.NoError(err)
		req := httptest.NewRequest(http.MethodGet, "/xrpc/com.example.other.api", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		middle := v.Middleware(webHome, true)
		resp := httptest.NewRecorder()
		middle(resp, req)
		assert.Equal(http.StatusUnauthorized, resp.Code)
	}
}
