package automod

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMemCountStoreBasics(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	cs := NewMemCountStore()

	c, err := cs.GetCount(ctx, "test1", "val1", PeriodTotal)
	assert.NoError(err)
	assert.Equal(0, c)
	assert.NoError(cs.Increment(ctx, "test1", "val1"))
	assert.NoError(cs.Increment(ctx, "test1", "val1"))
	c, err = cs.GetCount(ctx, "test1", "val1", PeriodTotal)
	assert.NoError(err)
	assert.Equal(2, c)

	c, err = cs.GetCountDistinct(ctx, "test2", "val2", PeriodTotal)
	assert.NoError(err)
	assert.Equal(0, c)
	assert.NoError(cs.IncrementDistinct(ctx, "test2", "val2", "one"))
	assert.NoError(cs.IncrementDistinct(ctx, "test2", "val2", "one"))
	assert.NoError(cs.IncrementDistinct(ctx, "test2", "val2", "one"))
	c, err = cs.GetCountDistinct(ctx, "test2", "val2", PeriodTotal)
	assert.NoError(err)
	assert.Equal(1, c)

	assert.NoError(cs.IncrementDistinct(ctx, "test2", "val2", "two"))
	assert.NoError(cs.IncrementDistinct(ctx, "test2", "val2", "three"))
	c, err = cs.GetCountDistinct(ctx, "test2", "val2", PeriodTotal)
	assert.NoError(err)
	assert.Equal(3, c)
}
