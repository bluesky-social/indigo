package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func redirectTo(location string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, location, http.StatusFound)
	})
}

func TestDialSubscribeReposRedirects(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteMessage(websocket.TextMessage, []byte("from-target"))
	}))
	defer target.Close()

	t.Run("follows redirect and maps http to ws", func(t *testing.T) {
		redirector := httptest.NewServer(redirectTo(target.URL))
		defer redirector.Close()

		conn, _, final, err := dialSubscribeRepos(ctx, &websocket.Dialer{}, "ws"+strings.TrimPrefix(redirector.URL, "http"), nil, true)
		if !assert.NoError(err) {
			return
		}
		defer conn.Close()
		assert.Equal("ws"+strings.TrimPrefix(target.URL, "http"), final)
		_, msg, err := conn.ReadMessage()
		assert.NoError(err)
		assert.Equal("from-target", string(msg))
	})

	t.Run("refuses downgrade from TLS", func(t *testing.T) {
		redirector := httptest.NewTLSServer(redirectTo(target.URL))
		defer redirector.Close()
		d := &websocket.Dialer{TLSClientConfig: redirector.Client().Transport.(*http.Transport).TLSClientConfig}

		_, _, _, err := dialSubscribeRepos(ctx, d, "wss"+strings.TrimPrefix(redirector.URL, "https"), nil, false)
		assert.ErrorContains(err, "insecure")
	})

	t.Run("refuses a multiple redirects", func(t *testing.T) {
		var loop *httptest.Server
		loop = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, loop.URL, http.StatusFound)
		}))
		defer loop.Close()

		_, _, _, err := dialSubscribeRepos(ctx, &websocket.Dialer{}, "ws"+strings.TrimPrefix(loop.URL, "http"), nil, true)
		assert.ErrorContains(err, "too many redirects")
	})
}
