package lexicon

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAcceptableMimeType(t *testing.T) {
	assert := assert.New(t)

	assert.True(acceptableMimeType("image/*", "image/png"))
	assert.True(acceptableMimeType("text/plain", "text/plain"))

	assert.False(acceptableMimeType("image/*", "text/plain"))
	assert.False(acceptableMimeType("text/plain", "image/png"))
	assert.False(acceptableMimeType("text/plain", ""))
	assert.False(acceptableMimeType("", "text/plain"))

	// TODO: application/json, application/json+thing
}
