package keyword

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTokenInSet(t *testing.T) {
	assert := assert.New(t)

	keywords := []string{
		"example",
		"bunch",
	}

	assert.True(TokenInSet("example", keywords))
	assert.False(TokenInSet("Example", keywords))
	assert.False(TokenInSet("elephant", keywords))
}
