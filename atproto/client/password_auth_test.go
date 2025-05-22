package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	case "/xrpc/com.example.get", "/xrpc/com.example.post":
		hdr := r.Header.Get("Authorization")
		if hdr == "Bearer access1" || hdr == "Bearer access2" {
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
		// simple GET requests, with token expire/retry
		c, err := LoginWithPassword(ctx, &dir, syntax.Handle("user1.example.com").AtIdentifier(), "password1", "")
		require.NoError(err)
		err = c.Get(ctx, syntax.NSID("com.example.get"), nil, nil)
		assert.NoError(err)
		err = c.Get(ctx, syntax.NSID("com.example.expire"), nil, nil)
		assert.NoError(err)
	}

	{
		// simple POST request, with token expire/retry
		c, err := LoginWithPassword(ctx, &dir, syntax.Handle("user1.example.com").AtIdentifier(), "password1", "")
		require.NoError(err)
		body := map[string]any{
			"a": 123,
			"b": "hello",
		}
		var out json.RawMessage
		err = c.Post(ctx, syntax.NSID("com.example.post"), body, &out)
		assert.NoError(err)
		err = c.Post(ctx, syntax.NSID("com.example.expire"), body, &out)
		assert.NoError(err)
	}

	{
		// POST with bytes.Buffer body
		c, err := LoginWithPassword(ctx, &dir, syntax.Handle("user1.example.com").AtIdentifier(), "password1", "")
		require.NoError(err)
		body := bytes.NewBufferString("some text")
		req := NewAPIRequest(MethodProcedure, syntax.NSID("com.example.expire"), body)
		req.Headers.Set("Content-Type", "text/plain")
		resp, err := c.Do(ctx, req)
		require.NoError(err)
		assert.Equal(200, resp.StatusCode)
	}

	{
		// POST with file on disk (can seek and retry)
		c, err := LoginWithPassword(ctx, &dir, syntax.Handle("user1.example.com").AtIdentifier(), "password1", "")
		require.NoError(err)
		f, err := os.Open("testdata/body.json")
		require.NoError(err)
		req := NewAPIRequest(MethodProcedure, syntax.NSID("com.example.expire"), f)
		req.Headers.Set("Content-Type", "application/json")
		resp, err := c.Do(ctx, req)
		require.NoError(err)
		assert.Equal(200, resp.StatusCode)
	}

	{
		// POST with pipe reader (can *not* retry)
		c, err := LoginWithPassword(ctx, &dir, syntax.Handle("user1.example.com").AtIdentifier(), "password1", "")
		require.NoError(err)
		r1, w1 := io.Pipe()
		go func() {
			fmt.Fprintf(w1, "some data")
			w1.Close()
		}()
		req1 := NewAPIRequest(MethodProcedure, syntax.NSID("com.example.post"), r1)
		req1.Headers.Set("Content-Type", "text/plain")
		resp, err := c.Do(ctx, req1)
		require.NoError(err)
		assert.Equal(200, resp.StatusCode)

		// expect this to fail (can't re-read from Pipe)
		r2, w2 := io.Pipe()
		go func() {
			fmt.Fprintf(w2, "some data")
			w2.Close()
		}()
		req2 := NewAPIRequest(MethodProcedure, syntax.NSID("com.example.expire"), r2)
		req2.Headers.Set("Content-Type", "text/plain")
		_, err = c.Do(ctx, req2)
		assert.Error(err)
	}
}
