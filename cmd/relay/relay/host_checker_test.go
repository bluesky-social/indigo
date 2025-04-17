package relay

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestMockHostChecker(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	var err error

	hc := NewMockHostChecker()
	hc.Hosts["https://pds.example.com"] = true
	hc.Accounts["did:web:active.example.com"] = "active"

	assert.NoError(hc.CheckHost(ctx, "https://pds.example.com"))
	assert.Error(hc.CheckHost(ctx, ""))
	assert.Error(hc.CheckHost(ctx, "https://dummy.example.com"))

	s1, err := hc.FetchAccountStatus(ctx, &identity.Identity{DID: syntax.DID("did:web:active.example.com")})
	assert.NoError(err)
	assert.Equal("active", s1)

	_, err = hc.FetchAccountStatus(ctx, &identity.Identity{DID: syntax.DID("did:web:nope.example.com")})
	assert.Error(err)
}

// NOTE: this test does live network resolutions
func TestLiveHostChecker(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()
	var err error

	dir := identity.DefaultDirectory()
	hc := NewHostClient("indigo-tests")

	assert.NoError(hc.CheckHost(ctx, "https://morel.us-east.host.bsky.network"))
	assert.Error(hc.CheckHost(ctx, "https://dummy.example.com"))

	ident, err := dir.LookupHandle(ctx, syntax.Handle("atproto.com"))
	assert.NoError(err)

	s1, err := hc.FetchAccountStatus(ctx, ident)
	assert.NoError(err)
	assert.Equal("active", s1)

	ident.DID = syntax.DID("did:web:dummy.example.com")
	_, err = hc.FetchAccountStatus(ctx, ident)
	assert.Error(err)
}
