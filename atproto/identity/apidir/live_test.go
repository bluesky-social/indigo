package apidir

import (
	"context"
	"testing"

	"github.com/gander-social/gander-indigo-sovereign/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestBasicLookups(t *testing.T) {
	t.Skip("skipping live network test")
	assert := assert.New(t)
	ctx := context.Background()
	var err error

	dir := NewAPIDirectory("http://localhost:6600")

	_, err = dir.LookupDID(ctx, syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz"))
	assert.NoError(err)

	_, err = dir.ResolveDID(ctx, syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz"))
	assert.NoError(err)

	_, err = dir.LookupHandle(ctx, syntax.Handle("atproto.com"))
	assert.NoError(err)

	_, err = dir.ResolveHandle(ctx, syntax.Handle("atproto.com"))
	assert.NoError(err)

	_, err = dir.LookupHandle(ctx, syntax.Handle("dummy-handle.atproto.com"))
	assert.Error(err)

	_, err = dir.ResolveHandle(ctx, syntax.Handle("dummy-handle.atproto.com"))
	assert.Error(err)
}
