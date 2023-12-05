package countstore

import (
	"context"
)

// TODO: this implementation isn't race-safe (yet)!
type MemCountStore struct {
	Counts         map[string]int
	DistinctCounts map[string]map[string]bool
}

func NewMemCountStore() MemCountStore {
	return MemCountStore{
		Counts:         make(map[string]int),
		DistinctCounts: make(map[string]map[string]bool),
	}
}

func (s MemCountStore) GetCount(ctx context.Context, name, val, period string) (int, error) {
	v, ok := s.Counts[periodBucket(name, val, period)]
	if !ok {
		return 0, nil
	}
	return v, nil
}

func (s MemCountStore) Increment(ctx context.Context, name, val string) error {
	for _, p := range []string{PeriodTotal, PeriodDay, PeriodHour} {
		k := periodBucket(name, val, p)
		v := s.Counts[k]
		v = v + 1
		s.Counts[k] = v
	}
	return nil
}

func (s MemCountStore) GetCountDistinct(ctx context.Context, name, bucket, period string) (int, error) {
	v, ok := s.DistinctCounts[periodBucket(name, bucket, period)]
	if !ok {
		return 0, nil
	}
	return len(v), nil
}

func (s MemCountStore) IncrementDistinct(ctx context.Context, name, bucket, val string) error {
	for _, p := range []string{PeriodTotal, PeriodDay, PeriodHour} {
		k := periodBucket(name, bucket, p)
		m, ok := s.DistinctCounts[k]
		if !ok {
			m = make(map[string]bool)
		}
		m[val] = true
		s.DistinctCounts[k] = m
	}
	return nil
}
