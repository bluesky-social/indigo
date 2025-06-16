package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func adminHandler(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if ok && username == "admin" && password == "secret" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, "{\"status\":\"success\"}")
		return
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="admin", charset="UTF-8"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}

func TestAdminAuth(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	var apierr *APIError

	srv := httptest.NewServer(http.HandlerFunc(adminHandler))
	defer srv.Close()

	{
		c := NewAPIClient(srv.URL)
		err := c.Get(ctx, syntax.NSID("com.example.get"), nil, nil)
		assert.ErrorAs(err, &apierr)
	}

	{
		c := NewAdminClient(srv.URL, "wrong")
		err := c.Get(ctx, syntax.NSID("com.example.get"), nil, nil)
		assert.ErrorAs(err, &apierr)
	}

	{
		c := NewAdminClient(srv.URL, "secret")
		err := c.Get(ctx, syntax.NSID("com.example.get"), nil, nil)
		assert.NoError(err)
	}
}
