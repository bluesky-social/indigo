package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseOAuthScope(t *testing.T) {
	assert := assert.New(t)

	perms, err := ParseOAuthScope("")
	assert.Error(err)

	perms, err = ParseOAuthScope("atproto repo:*")
	assert.NoError(err)
	assert.Equal(1, len(perms))

	perms, err = ParseOAuthScope("atproto rpc:asdf")
	assert.NoError(err)
	assert.Equal(0, len(perms))
}
