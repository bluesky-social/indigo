package automod

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

type CountStore interface {
	GetCount(ctx context.Context, name, val, period string) (int, error)
	Increment(ctx context.Context, name, val string) error
	IncrementPeriod(ctx context.Context, name, val, period string) error
	// TODO: batch increment method
	GetCountDistinct(ctx context.Context, name, bucket, period string) (int, error)
	IncrementDistinct(ctx context.Context, name, bucket, val string) error
}

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

func PeriodBucket(name, val, period string) string {
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

func (s MemCountStore) GetCount(ctx context.Context, name, val, period string) (int, error) {
	v, ok := s.Counts[PeriodBucket(name, val, period)]
	if !ok {
		return 0, nil
	}
	return v, nil
}

func (s MemCountStore) Increment(ctx context.Context, name, val string) error {
	for _, p := range []string{PeriodTotal, PeriodDay, PeriodHour} {
		s.IncrementPeriod(ctx, name, val, p)
	}
	return nil
}

func (s MemCountStore) IncrementPeriod(ctx context.Context, name, val, period string) error {
	k := PeriodBucket(name, val, period)
	v, ok := s.Counts[k]
	if !ok {
		v = 0
	}
	v = v + 1
	s.Counts[k] = v
	return nil
}

func (s MemCountStore) GetCountDistinct(ctx context.Context, name, bucket, period string) (int, error) {
	v, ok := s.DistinctCounts[PeriodBucket(name, bucket, period)]
	if !ok {
		return 0, nil
	}
	return len(v), nil
}

func (s MemCountStore) IncrementDistinct(ctx context.Context, name, bucket, val string) error {
	for _, p := range []string{PeriodTotal, PeriodDay, PeriodHour} {
		k := PeriodBucket(name, bucket, p)
		m, ok := s.DistinctCounts[k]
		if !ok {
			m = make(map[string]bool)
		}
		m[val] = true
		s.DistinctCounts[k] = m
	}
	return nil
}
