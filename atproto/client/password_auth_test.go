package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pwHandler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/xrpc/com.atproto.server.refreshSession":
		//fmt.Println("refreshSession handler...")
		hdr := r.Header.Get("Authorization")
		if hdr != "Bearer refresh1" {
			fmt.Printf("refreshSession header: %s\n", hdr)
			w.Header().Set("WWW-Authenticate", `Bearer`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"did":        "did:web:account.example.com",
			"accessJwt":  "access2",
			"refreshJwt": "refresh2",
		})
		return
	case "/xrpc/com.atproto.server.createSession":
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			fmt.Println("createSession Content-Type")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			fmt.Println("createSession JSON")
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		if body["identifier"] != "did:web:account.example.com" || body["password"] != "password1" {
			fmt.Println("createSession wrong password")
			http.Error(w, "Bad Request", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"did":        body["identifier"],
			"accessJwt":  "access1",
			"refreshJwt": "refresh1",
		})
		return
	case "/xrpc/com.example.get":
		hdr := r.Header.Get("Authorization")
		if hdr == "Bearer access1" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, "{\"status\":\"success\"}")
			return
		} else {
			fmt.Printf("get header: %s\n", hdr)
			w.Header().Set("WWW-Authenticate", `Bearer`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	case "/xrpc/com.example.expire":
		hdr := r.Header.Get("Authorization")
		if hdr == "Bearer access1" {
			//fmt.Println("forcing refresh...")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			fmt.Fprintln(w, "{\"error\":\"ExpiredToken\"}")
			return
		} else if hdr == "Bearer access2" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, "{\"status\":\"success\"}")
			return
		} else {
			fmt.Printf("expire header: %s\n", hdr)
			w.Header().Set("WWW-Authenticate", `Bearer`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	default:
		http.NotFound(w, r)
		return
	}
}

func TestPasswordAuth(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)
	ctx := context.Background()
	//var apierr *APIError

	srv := httptest.NewServer(http.HandlerFunc(pwHandler))
	defer srv.Close()

	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:    "did:web:account.example.com",
		Handle: "user1.example.com",
		Services: map[string]identity.Service{
			"atproto_pds": {
				Type: "AtprotoPersonalDataServer",
				URL:  srv.URL,
			},
		},
	})

	{
		c, err := LoginWithPassword(ctx, &dir, syntax.Handle("user1.example.com").AtIdentifier(), "password1", "")
		require.NoError(err)
		err = c.Get(ctx, syntax.NSID("com.example.get"), nil, nil)
		assert.NoError(err)
		err = c.Get(ctx, syntax.NSID("com.example.expire"), nil, nil)
		assert.NoError(err)
	}
}
