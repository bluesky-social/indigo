package identity

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

// NOTE: this hits the open internet! marked as skip below by default
func testCatalogLive(t *testing.T, c Catalog) {
	assert := assert.New(t)
	ctx := context.Background()

	handle := syntax.Handle("atproto.com")
	did := syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz")
	pds := "https://bsky.social"

	resp, err := c.LookupHandle(ctx, handle)
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

	resp, err = c.LookupDID(ctx, did)
	assert.NoError(err)
	assert.Equal(handle, resp.Handle)
	assert.Equal(did, resp.DID)
	assert.Equal(pds, resp.PDSEndpoint())

	_, err = c.LookupHandle(ctx, syntax.Handle("fake-dummy-no-resolve.atproto.com"))
	assert.Equal(ErrHandleNotFound, err)

	_, err = c.LookupDID(ctx, syntax.DID("did:web:fake-dummy-no-resolve.atproto.com"))
	assert.Equal(ErrDIDNotFound, err)

	_, err = c.LookupDID(ctx, syntax.DID("did:plc:fake-dummy-no-resolve.atproto.com"))
	assert.Equal(ErrDIDNotFound, err)

	_, err = c.LookupHandle(ctx, syntax.Handle("handle.invalid"))
	assert.Error(err)
}

func TestBasicCatalog(t *testing.T) {
	// XXX: t.Skip("skipping live network test")
	c := NewBasicCatalog(DefaultPLCURL)
	testCatalogLive(t, &c)
}

func TestCacheCatalog(t *testing.T) {
	// XXX: t.Skip("skipping live network test")
	inner := NewBasicCatalog(DefaultPLCURL)
	c := NewCacheCatalog(&inner)
	for i := 0; i < 3; i = i + 1 {
		testCatalogLive(t, &c)
	}
}
