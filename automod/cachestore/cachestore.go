package cachestore

import (
	"context"
)

type CacheStore interface {
	Get(ctx context.Context, name, key string) (string, error)
	Set(ctx context.Context, name, key string, val string) error
	Purge(ctx context.Context, name, key string) error
}
