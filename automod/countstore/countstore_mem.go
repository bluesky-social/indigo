package countstore

import (
	"context"

	"github.com/puzpuzpuz/xsync/v3"
)

type MemCountStore struct {
	// Counts is keyed by a string that is a munge of "{name}/{val}[/{period}]",
	// where period is either absent (meaning all-time total)
	// or a string describing that timeperiod (either "YYYY-MM-DD" or that plus a literal "T" and "HH").
	//
	// (Using a values for `name` and `val` with slashes in them is perhaps inadvisable, as it may be ambiguous.)
	Counts         *xsync.MapOf[string, int]
	DistinctCounts *xsync.MapOf[string, *xsync.MapOf[string, bool]]
}

func NewMemCountStore() MemCountStore {
	return MemCountStore{
		Counts:         xsync.NewMapOf[string, int](),
		DistinctCounts: xsync.NewMapOf[string, *xsync.MapOf[string, bool]](),
	}
}

func (s MemCountStore) GetCount(ctx context.Context, name, val, period string) (int, error) {
	v, ok := s.Counts.Load(periodBucket(name, val, period))
	if !ok {
		return 0, nil
	}
	return v, nil
}

func (s MemCountStore) Increment(ctx context.Context, name, val string) error {
	for _, p := range []string{PeriodTotal, PeriodDay, PeriodHour} {
		if err := s.IncrementPeriod(ctx, name, val, p); err != nil {
			return err
		}
	}
	return nil
}

func (s MemCountStore) IncrementPeriod(ctx context.Context, name, val, period string) error {
	k := periodBucket(name, val, period)
	s.Counts.Compute(k, func(oldVal int, _ bool) (int, bool) {
		return oldVal + 1, false
	})
	return nil
}

func (s MemCountStore) GetCountDistinct(ctx context.Context, name, bucket, period string) (int, error) {
	v, ok := s.DistinctCounts.Load(periodBucket(name, bucket, period))
	if !ok {
		return 0, nil
	}
	return v.Size(), nil
}

func (s MemCountStore) IncrementDistinct(ctx context.Context, name, bucket, val string) error {
	for _, p := range []string{PeriodTotal, PeriodDay, PeriodHour} {
		k := periodBucket(name, bucket, p)
		s.DistinctCounts.Compute(k, func(nested *xsync.MapOf[string, bool], _ bool) (*xsync.MapOf[string, bool], bool) {
			if nested == nil {
				nested = xsync.NewMapOf[string, bool]()
			}
			nested.Store(val, true)
			return nested, false
		})
	}
	return nil
}
