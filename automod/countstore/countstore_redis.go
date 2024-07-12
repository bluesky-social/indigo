package countstore

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

var redisCountPrefix string = "count/"
var redisDistinctPrefix string = "distinct/"

type RedisCountStore struct {
	Client *redis.Client
}

func NewRedisCountStore(redisURL string) (*RedisCountStore, error) {
	ctx := context.Background()
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opt)
	// check redis connection
	_, err = rdb.Ping(ctx).Result()
	if err != nil {
		return nil, err
	}
	rcs := RedisCountStore{
		Client: rdb,
	}
	return &rcs, nil
}

func (s *RedisCountStore) GetCount(ctx context.Context, name, val, period string) (int, error) {
	key := redisCountPrefix + periodBucket(name, val, period)
	c, err := s.Client.Get(ctx, key).Int()
	if err == redis.Nil {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	return c, nil
}

func (s *RedisCountStore) Increment(ctx context.Context, name, val string) error {

	var key string

	// increment multiple counters in a single redis round-trip
	multi := s.Client.Pipeline()

	key = redisCountPrefix + periodBucket(name, val, PeriodHour)
	multi.Incr(ctx, key)
	multi.Expire(ctx, key, 2*time.Hour)

	key = redisCountPrefix + periodBucket(name, val, PeriodDay)
	multi.Incr(ctx, key)
	multi.Expire(ctx, key, 48*time.Hour)

	key = redisCountPrefix + periodBucket(name, val, PeriodTotal)
	multi.Incr(ctx, key)
	// no expiration for total

	_, err := multi.Exec(ctx)
	return err
}

// Variant of Increment() which only acts on a single specified time period. The intended us of this variant is to control the total number of counters persisted, by using a relatively short time period, for which the counters will expire.
func (s *RedisCountStore) IncrementPeriod(ctx context.Context, name, val, period string) error {

	// multiple ops in a single redis round-trip
	multi := s.Client.Pipeline()

	key := redisCountPrefix + periodBucket(name, val, period)
	multi.Incr(ctx, key)

	switch period {
	case PeriodHour:
		multi.Expire(ctx, key, 2*time.Hour)
	case PeriodDay:
		multi.Expire(ctx, key, 48*time.Hour)
	}

	_, err := multi.Exec(ctx)
	return err
}

func (s *RedisCountStore) GetCountDistinct(ctx context.Context, name, val, period string) (int, error) {
	key := redisDistinctPrefix + periodBucket(name, val, period)
	c, err := s.Client.PFCount(ctx, key).Result()
	if err == redis.Nil {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	return int(c), nil
}

func (s *RedisCountStore) IncrementDistinct(ctx context.Context, name, bucket, val string) error {

	var key string

	// increment multiple counters in a single redis round-trip
	multi := s.Client.Pipeline()

	key = redisDistinctPrefix + periodBucket(name, bucket, PeriodHour)
	multi.PFAdd(ctx, key, val)
	multi.Expire(ctx, key, 2*time.Hour)

	key = redisDistinctPrefix + periodBucket(name, bucket, PeriodDay)
	multi.PFAdd(ctx, key, val)
	multi.Expire(ctx, key, 48*time.Hour)

	key = redisDistinctPrefix + periodBucket(name, bucket, PeriodTotal)
	multi.PFAdd(ctx, key, val)
	// no expiration for total

	_, err := multi.Exec(ctx)
	return err
}
