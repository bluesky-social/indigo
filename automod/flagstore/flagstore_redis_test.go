package flagstore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedisFlagStoreBasics(t *testing.T) {
	t.Skip("live test, need redis running locally")
	assert := assert.New(t)
	ctx := context.Background()

	fs, err := NewRedisFlagStore("redis://localhost:6379/0")
	if err != nil {
		t.Fail()
	}

	l, err := fs.Get(ctx, "test1")
	assert.NoError(err)
	assert.Empty(l)

	assert.NoError(fs.Add(ctx, "test1", []string{"red", "green"}))
	assert.NoError(fs.Add(ctx, "test1", []string{"red", "blue"}))
	l, err = fs.Get(ctx, "test1")
	assert.NoError(err)
	assert.Equal(3, len(l))

	assert.NoError(fs.Remove(ctx, "test1", []string{"red", "blue", "orange"}))
	l, err = fs.Get(ctx, "test1")
	assert.NoError(err)
	assert.Equal([]string{"green"}, l)
	assert.NoError(fs.Remove(ctx, "test1", []string{"green"}))
}
