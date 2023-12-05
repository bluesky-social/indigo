package countstore

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	PeriodTotal = "total"
	PeriodDay   = "day"
	PeriodHour  = "hour"
)

// CountStore is an interface for storing incrementing event counts, bucketed into periods.
// It is implemented by MemCountStore and by RedisCountStore.
//
// Period bucketing works on the basis of the current date (as determined mid-call).
// See the `Period*` consts for the available period types.
//
// The "GetCount" and "Increment" methods perform actual counting.
// The "*Distinct" methods store booleans for values, rather than incrementing;
// fetching "GetCountDistinct" asks how many values have been seen at least once,
// rather than how many times they've been seen.
type CountStore interface {
	GetCount(ctx context.Context, name, val, period string) (int, error)
	Increment(ctx context.Context, name, val string) error
	// TODO: batch increment method
	GetCountDistinct(ctx context.Context, name, bucket, period string) (int, error)
	// REVIEW: s/IncrementDistinct/MarkDistinct/ ?
	IncrementDistinct(ctx context.Context, name, bucket, val string) error
}

func periodBucket(name, val, period string) string {
	switch period {
	case PeriodTotal:
		return fmt.Sprintf("%s/%s", name, val)
	case PeriodDay:
		t := time.Now().UTC().Format(time.DateOnly)
		return fmt.Sprintf("%s/%s/%s", name, val, t)
	case PeriodHour:
		t := time.Now().UTC().Format(time.RFC3339)[0:13]
		return fmt.Sprintf("%s/%s/%s", name, val, t)
	default:
		slog.Warn("unhandled counter period", "period", period)
		return fmt.Sprintf("%s/%s", name, val)
	}
}
