package identity

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestMockDirectory(t *testing.T) {
	var err error
	assert := assert.New(t)
	ctx := context.Background()
	c := NewMockDirectory()
	id1 := Identity{
		DID:    syntax.DID("did:plc:abc111"),
		Handle: syntax.Handle("handle.example.com"),
	}
	id2 := Identity{
		DID:    syntax.DID("did:plc:abc222"),
		Handle: syntax.HandleInvalid,
	}
	id3 := Identity{
		DID:    syntax.DID("did:plc:abc333"),
		Handle: syntax.Handle("handle3.example.com"),
	}

	// first, empty directory
	_, err = c.LookupHandle(ctx, syntax.Handle("handle.example.com"))
	assert.ErrorIs(err, ErrHandleNotFound)
	_, err = c.LookupDID(ctx, syntax.DID("did:plc:abc123"))
	assert.ErrorIs(err, ErrDIDNotFound)

	c.Insert(id1)
	c.Insert(id2)
	c.Insert(id3)

	out, err := c.LookupHandle(ctx, syntax.Handle("handle.example.com"))
	assert.NoError(err)
	assert.Equal(&id1, out)
	out, err = c.LookupDID(ctx, syntax.DID("did:plc:abc111"))
	assert.NoError(err)
	assert.Equal(&id1, out)

	out, err = c.LookupDID(ctx, syntax.DID("did:plc:abc222"))
	assert.NoError(err)
	assert.True(out.Handle.IsInvalidHandle())

	_, err = c.LookupHandle(ctx, syntax.HandleInvalid)
	assert.ErrorIs(err, ErrHandleNotFound)
	_, err = c.LookupDID(ctx, syntax.DID("did:plc:abc999"))
	assert.ErrorIs(err, ErrDIDNotFound)

	did, err := c.ResolveHandle(ctx, syntax.Handle("handle.example.com"))
	assert.NoError(err)
	assert.Equal(id1.DID, did)
	_, err = c.ResolveHandle(ctx, syntax.Handle("notfound.example.com"))
	assert.ErrorIs(err, ErrHandleNotFound)

	_, err = c.ResolveDID(ctx, syntax.DID("did:plc:abc222"))
	assert.NoError(err)
	// TODO: verify structure matches

	_, err = c.ResolveDID(ctx, syntax.DID("did:plc:abc999"))
	assert.ErrorIs(err, ErrDIDNotFound)
}
