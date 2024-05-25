package flagstore

import (
	"context"

	"github.com/redis/go-redis/v9"
)

var redisFlagsPrefix string = "flags/"

type RedisFlagStore struct {
	Client *redis.Client
}

func NewRedisFlagStore(redisURL string) (*RedisFlagStore, error) {
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
	rcs := RedisFlagStore{
		Client: rdb,
	}
	return &rcs, nil
}

func (s *RedisFlagStore) Get(ctx context.Context, key string) ([]string, error) {
	rkey := redisFlagsPrefix + key
	l, err := s.Client.SMembers(ctx, rkey).Result()
	if err == redis.Nil {
		return []string{}, nil
	} else if err != nil {
		return nil, err
	}
	return l, nil
}

func (s *RedisFlagStore) Add(ctx context.Context, key string, flags []string) error {
	if len(flags) == 0 {
		return nil
	}
	l := []interface{}{}
	for _, v := range flags {
		l = append(l, v)
	}
	rkey := redisFlagsPrefix + key
	return s.Client.SAdd(ctx, rkey, l...).Err()
}

func (s *RedisFlagStore) Remove(ctx context.Context, key string, flags []string) error {
	if len(flags) == 0 {
		return nil
	}
	l := []interface{}{}
	for _, v := range flags {
		l = append(l, v)
	}
	rkey := redisFlagsPrefix + key
	return s.Client.SRem(ctx, rkey, l...).Err()
}
