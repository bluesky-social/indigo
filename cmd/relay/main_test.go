package main

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/cmd/relay/relay"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeDatabaseURLForLog(t *testing.T) {
	assert.Equal(t, "sqlite://data/relay/relay.sqlite", safeDatabaseURLForLog("sqlite://data/relay/relay.sqlite"))
	assert.Equal(t, "postgres=<redacted>", safeDatabaseURLForLog("postgres=host=localhost user=relay password=secret dbname=relay"))
	assert.Equal(t, "postgres://relay:xxxxx@localhost:5432/relay?sslmode=disable", safeDatabaseURLForLog("postgres://relay:secret@localhost:5432/relay?sslmode=disable"))
	assert.Equal(t, "postgres://localhost:5432/relay?password=xxxxx&sslmode=disable", safeDatabaseURLForLog("postgres://localhost:5432/relay?sslmode=disable&password=secret"))
}

func TestAdminRoutesRequireAdminAuth(t *testing.T) {
	svc, err := NewService(&relay.Relay{
		Config: relay.RelayConfig{
			UserAgent: "indigo-relay-test",
		},
	}, &ServiceConfig{
		AdminPasswords:      []string{"correct-password"},
		ListenerBootTimeout: time.Second,
	})
	require.NoError(t, err)

	li, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer li.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.startWithListener(li)
	}()

	client := &http.Client{Timeout: time.Second}
	routes := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/admin/pds/list"},
		{method: http.MethodPost, path: "/admin/pds/changeLimits", body: `{}`},
		{method: http.MethodPost, path: "/admin/alerts/accountLimitSent", body: `{}`},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path+" without auth", func(t *testing.T) {
			req, err := http.NewRequest(route.method, "http://"+li.Addr().String()+route.path, strings.NewReader(route.body))
			require.NoError(t, err)
			if route.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)

			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			assert.Equal(t, "Basic", resp.Header.Get("WWW-Authenticate"))
		})

		t.Run(route.method+" "+route.path+" with wrong auth", func(t *testing.T) {
			req, err := http.NewRequest(route.method, "http://"+li.Addr().String()+route.path, strings.NewReader(route.body))
			require.NoError(t, err)
			if route.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			req.SetBasicAuth("admin", "wrong-password")

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)

			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			assert.Equal(t, "Basic", resp.Header.Get("WWW-Authenticate"))
		})
	}

	require.NoError(t, li.Close())
	select {
	case err := <-errCh:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for test relay server to stop")
	}
}
