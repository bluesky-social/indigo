package countstore

import (
	"context"
)

const (
	PeriodTotal = "total"
	PeriodDay   = "day"
	PeriodHour  = "hour"
)

type CountStore interface {
	GetCount(ctx context.Context, name, val, period string) (int, error)
	Increment(ctx context.Context, name, val string) error
	// TODO: batch increment method
	GetCountDistinct(ctx context.Context, name, bucket, period string) (int, error)
	IncrementDistinct(ctx context.Context, name, bucket, val string) error
}
