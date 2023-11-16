package automod

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

var redisCountPrefix string = "count/"

type RedisCountStore struct {
	Client *redis.Client
}

func NewRedisCountStore(redisURL string) (*RedisCountStore, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(opt)
	// check redis connection
	_, err = rdb.Ping(context.TODO()).Result()
	if err != nil {
		return nil, err
	}
	rcs := RedisCountStore{
		Client: rdb,
	}
	return &rcs, nil
}

func (s *RedisCountStore) GetCount(ctx context.Context, name, val, period string) (int, error) {
	key := redisCountPrefix + PeriodBucket(name, val, period)
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

	key = redisCountPrefix + PeriodBucket(name, val, PeriodHour)
	multi.Incr(ctx, key)
	multi.Expire(ctx, key, 2*time.Hour)

	key = redisCountPrefix + PeriodBucket(name, val, PeriodDay)
	multi.Incr(ctx, key)
	multi.Expire(ctx, key, 48*time.Hour)

	key = redisCountPrefix + PeriodBucket(name, val, PeriodTotal)
	multi.Incr(ctx, key)
	// no expiration for total

	_, err := multi.Exec(ctx)
	return err
}
