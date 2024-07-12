package cachestore

import (
	"context"
	"time"

	"github.com/go-redis/cache/v9"
	"github.com/redis/go-redis/v9"
)

type RedisCacheStore struct {
	Data *cache.Cache
	TTL  time.Duration
}

var _ CacheStore = (*RedisCacheStore)(nil)

func NewRedisCacheStore(redisURL string, ttl time.Duration) (*RedisCacheStore, error) {
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
	data := cache.New(&cache.Options{
		Redis:      rdb,
		LocalCache: cache.NewTinyLFU(10_000, ttl),
	})
	return &RedisCacheStore{
		Data: data,
		TTL:  ttl,
	}, nil
}

func redisCacheKey(name, key string) string {
	return "cache/" + name + "/" + key
}

func (s RedisCacheStore) Get(ctx context.Context, name, key string) (string, error) {
	var val string
	err := s.Data.Get(ctx, redisCacheKey(name, key), &val)
	if err == cache.ErrCacheMiss {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

func (s RedisCacheStore) Set(ctx context.Context, name, key string, val string) error {
	return s.Data.Set(&cache.Item{
		Ctx:   ctx,
		Key:   redisCacheKey(name, key),
		Value: val,
		TTL:   s.TTL,
	})
}

func (s RedisCacheStore) Purge(ctx context.Context, name, key string) error {
	err := s.Data.Delete(ctx, redisCacheKey(name, key))
	if err == cache.ErrCacheMiss {
		return nil
	}
	return err
}
