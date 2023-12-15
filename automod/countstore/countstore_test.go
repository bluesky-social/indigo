package countstore

import (
	"context"
	"sync"
	"testing"
	"time"

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

	for _, period := range []string{PeriodTotal, PeriodDay, PeriodHour} {
		c, err = cs.GetCount(ctx, "test1", "val1", period)
		assert.NoError(err)
		assert.Equal(2, c)
	}

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

	for _, period := range []string{PeriodTotal, PeriodDay, PeriodHour} {
		c, err = cs.GetCountDistinct(ctx, "test2", "val2", period)
		assert.NoError(err)
		assert.Equal(3, c)
	}
}

func TestMemCountStoreConcurrent(t *testing.T) {
	assert := assert.New(t)
	ctx := context.Background()

	cs := NewMemCountStore()

	c, err := cs.GetCount(ctx, "test1", "val1", PeriodTotal)
	assert.NoError(err)
	assert.Equal(0, c)

	// Increment two different values from four different goroutines.
	// Read from two more (don't assert values; just that there's no error,
	// and no race (run this with `-race`!).
	// A short sleep ensures the scheduler is yielded to, so that order is decently random,
	// and reads are interleaved with writes.
	var wg sync.WaitGroup
	fnInc := func(name, val string, times int) {
		for i := 0; i < times; i++ {
			assert.NoError(cs.Increment(ctx, name, val))
			assert.NoError(cs.IncrementDistinct(ctx, name, name, val))
			time.Sleep(time.Nanosecond)
		}
		wg.Done()
	}
	fnRead := func(name, val string, times int) {
		for i := 0; i < times; i++ {
			_, err := cs.GetCount(ctx, name, val, PeriodTotal)
			assert.NoError(err)
			time.Sleep(time.Nanosecond)
		}
	}
	wg.Add(4)
	go fnInc("test1", "val1", 10)
	go fnInc("test1", "val1", 10)
	go fnRead("test1", "val1", 10)
	go fnInc("test2", "val2", 6)
	go fnInc("test2", "val2", 6)
	go fnRead("test2", "val2", 6)
	wg.Wait()

	// One final read for each value after all writer routines are collected.
	// This one should match a fixed value of the sum of all writes.
	c, err = cs.GetCount(ctx, "test1", "val1", PeriodTotal)
	assert.NoError(err)
	assert.Equal(20, c)
	c, err = cs.GetCount(ctx, "test2", "val2", PeriodTotal)
	assert.NoError(err)
	assert.Equal(12, c)

	// And what of distinct counts?  Those should be 1.
	c, err = cs.GetCountDistinct(ctx, "test1", "test1", PeriodTotal)
	assert.NoError(err)
	assert.Equal(1, c)
	c, err = cs.GetCountDistinct(ctx, "test2", "test2", PeriodTotal)
	assert.NoError(err)
	assert.Equal(1, c)
}
