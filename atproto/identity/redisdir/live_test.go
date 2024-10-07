package redisdir

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/time/rate"

	"github.com/stretchr/testify/assert"
)

var redisLocalTestURL string = "redis://localhost:6379/0"

// NOTE: this hits the open internet! marked as skip below by default
func testDirectoryLive(t *testing.T, d identity.Directory) {
	assert := assert.New(t)
	ctx := context.Background()

	handle := syntax.Handle("atproto.com")
	did := syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz")
	pdsSuffix := "host.bsky.network"

	resp, err := d.LookupHandle(ctx, handle)
	assert.NoError(err)
	assert.Equal(handle, resp.Handle)
	assert.Equal(did, resp.DID)
	assert.True(strings.HasSuffix(resp.PDSEndpoint(), pdsSuffix))
	dh, err := resp.DeclaredHandle()
	assert.NoError(err)
	assert.Equal(handle, dh)
	pk, err := resp.PublicKey()
	assert.NoError(err)
	assert.NotNil(pk)

	resp, err = d.LookupDID(ctx, did)
	assert.NoError(err)
	assert.Equal(handle, resp.Handle)
	assert.Equal(did, resp.DID)
	assert.True(strings.HasSuffix(resp.PDSEndpoint(), pdsSuffix))

	_, err = d.LookupHandle(ctx, syntax.Handle("fake-dummy-no-resolve.atproto.com"))
	assert.Error(err)
	//assert.ErrorIs(err, identity.ErrHandleNotFound)

	_, err = d.LookupDID(ctx, syntax.DID("did:web:fake-dummy-no-resolve.atproto.com"))
	assert.Error(err)
	//assert.ErrorIs(err, identity.ErrDIDNotFound)

	_, err = d.LookupDID(ctx, syntax.DID("did:plc:fake-dummy-no-resolve.atproto.com"))
	assert.Error(err)
	//assert.ErrorIs(err, identity.ErrDIDNotFound)

	_, err = d.LookupHandle(ctx, syntax.HandleInvalid)
	assert.Error(err)
}

func TestRedisDirectory(t *testing.T) {
	t.Skip("TODO: skipping live network test")
	assert := assert.New(t)
	ctx := context.Background()
	inner := identity.BaseDirectory{}
	d, err := NewRedisDirectory(&inner, redisLocalTestURL, time.Hour*1, time.Hour*1, time.Hour*1, 1000)
	if err != nil {
		t.Fatal(err)
	}

	err = d.Purge(ctx, syntax.Handle("atproto.com").AtIdentifier())
	assert.NoError(err)
	err = d.Purge(ctx, syntax.Handle("fake-dummy-no-resolve.atproto.com").AtIdentifier())
	assert.NoError(err)
	err = d.Purge(ctx, syntax.DID("did:web:fake-dummy-no-resolve.atproto.com").AtIdentifier())
	assert.NoError(err)
	err = d.Purge(ctx, syntax.DID("did:plc:fake-dummy-no-resolve.atproto.com").AtIdentifier())
	assert.NoError(err)

	for i := 0; i < 3; i = i + 1 {
		testDirectoryLive(t, d)
	}
}

func TestRedisCoalesce(t *testing.T) {
	t.Skip("TODO: skipping live network test")

	assert := assert.New(t)
	handle := syntax.Handle("atproto.com")
	did := syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz")

	base := identity.BaseDirectory{
		PLCURL: "https://plc.directory",
		HTTPClient: http.Client{
			Timeout: time.Second * 15,
		},
		// Limit the number of requests we can make to the PLC to 1 per second
		PLCLimiter:            rate.NewLimiter(1, 1),
		TryAuthoritativeDNS:   true,
		SkipDNSDomainSuffixes: []string{".bsky.social"},
	}
	dir, err := NewRedisDirectory(&base, redisLocalTestURL, time.Hour*1, time.Hour*1, time.Hour*1, 1000)
	if err != nil {
		t.Fatal(err)
	}
	// All 60 routines launch at the same time, so they should all miss the cache initially
	routines := 60
	wg := sync.WaitGroup{}

	// Cancel the context after 2 seconds, if we're coalescing correctly, we should only make 1 request
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	for i := 0; i < routines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ident, err := dir.LookupDID(ctx, did)
			if err != nil {
				slog.Error("Failed lookup", "error", err)
			}
			assert.NoError(err)
			assert.Equal(handle, ident.Handle)

			ident, err = dir.LookupHandle(ctx, handle)
			if err != nil {
				slog.Error("Failed lookup", "error", err)
			}
			assert.NoError(err)
			assert.Equal(did, ident.DID)
		}()
	}
	wg.Wait()
}
