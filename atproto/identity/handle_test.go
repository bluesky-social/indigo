package identity

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/stretchr/testify/assert"
)

func TestParseTXTResp(t *testing.T) {
	assert := assert.New(t)

	did, err := parseTXTResp([]string{"did=did:plc:ewvi7nxzyoun6zhxrhs64oiz"})
	assert.NoError(err)
	assert.Equal(syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz"), did)

	// records without the "did=" prefix are ignored
	did, err = parseTXTResp([]string{"v=spf1 -all", "did=did:plc:ewvi7nxzyoun6zhxrhs64oiz"})
	assert.NoError(err)
	assert.Equal(syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz"), did)

	// duplicate records with the same DID are not ambiguous
	did, err = parseTXTResp([]string{"did=did:plc:ewvi7nxzyoun6zhxrhs64oiz", "did=did:plc:ewvi7nxzyoun6zhxrhs64oiz"})
	assert.NoError(err)
	assert.Equal(syntax.DID("did:plc:ewvi7nxzyoun6zhxrhs64oiz"), did)

	_, err = parseTXTResp([]string{})
	assert.ErrorIs(err, ErrHandleNotFound)

	_, err = parseTXTResp([]string{"v=spf1 -all"})
	assert.ErrorIs(err, ErrHandleNotFound)

	_, err = parseTXTResp([]string{"did=not-a-did"})
	assert.ErrorIs(err, ErrHandleResolutionFailed)

	// multiple records with different DIDs are ambiguous, and resolution fails
	_, err = parseTXTResp([]string{"did=did:plc:ewvi7nxzyoun6zhxrhs64oiz", "did=did:web:example.com"})
	assert.ErrorIs(err, ErrHandleResolutionFailed)
}
