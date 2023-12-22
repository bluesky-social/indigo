package flagstore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlagStoreBasics(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	fs := NewMemFlagStore()

	l, err := fs.Get(ctx, "test1")
	assert.NoError(err)
	assert.Empty(l)

	assert.NoError(fs.Add(ctx, "test1", []string{"red", "green"}))
	assert.NoError(fs.Add(ctx, "test1", []string{"red", "blue"}))
	l, err = fs.Get(ctx, "test1")
	assert.NoError(err)
	assert.Equal(3, len(l))

	assert.NoError(fs.Remove(ctx, "test1", []string{"red", "blue"}))
	l, err = fs.Get(ctx, "test1")
	assert.NoError(err)
	assert.Equal([]string{"green"}, l)
}
