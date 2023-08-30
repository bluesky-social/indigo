package identity

import (
	"context"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

// NOTE: this hits the open internet! marked as skip below by default
func testDirectoryLive(t *testing.T, d Directory) {
	assert := assert.New(t)
	ctx := context.Background()

	handle := syntax.Handle("atproto.com")
	did := syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz")
	pds := "https://bsky.social"

	resp, err := d.LookupHandle(ctx, handle)
	assert.NoError(err)
	assert.Equal(handle, resp.Handle)
	assert.Equal(did, resp.DID)
	assert.Equal(pds, resp.PDSEndpoint())
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
	assert.Equal(pds, resp.PDSEndpoint())

	_, err = d.LookupHandle(ctx, syntax.Handle("fake-dummy-no-resolve.atproto.com"))
	assert.Equal(ErrHandleNotFound, err)

	_, err = d.LookupDID(ctx, syntax.DID("did:web:fake-dummy-no-resolve.atproto.com"))
	assert.Equal(ErrDIDNotFound, err)

	_, err = d.LookupDID(ctx, syntax.DID("did:plc:fake-dummy-no-resolve.atproto.com"))
	assert.Equal(ErrDIDNotFound, err)

	_, err = d.LookupHandle(ctx, syntax.Handle("handle.invalid"))
	assert.Error(err)
}

func TestBaseDirectory(t *testing.T) {
	t.Skip("TODO: skipping live network test")
	d := BaseDirectory{}
	testDirectoryLive(t, &d)
}

func TestCacheDirectory(t *testing.T) {
	t.Skip("TODO: skipping live network test")
	inner := BaseDirectory{}
	d := NewCacheDirectory(&inner, 1000, time.Hour*1, time.Hour*1)
	for i := 0; i < 3; i = i + 1 {
		testDirectoryLive(t, &d)
	}
}
