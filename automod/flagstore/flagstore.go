package flagstore

import (
	"context"
)

type FlagStore interface {
	Get(ctx context.Context, key string) ([]string, error)
	Add(ctx context.Context, key string, flags []string) error
	Remove(ctx context.Context, key string, flags []string) error
}
