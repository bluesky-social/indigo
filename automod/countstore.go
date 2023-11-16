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
	// TODO: batch increment method
}

// TODO: this implementation isn't race-safe (yet)!
type MemCountStore struct {
	Counts map[string]int
}

func NewMemCountStore() MemCountStore {
	return MemCountStore{
		Counts: make(map[string]int),
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
		k := PeriodBucket(name, val, p)
		v, ok := s.Counts[k]
		if !ok {
			v = 0
		}
		v = v + 1
		s.Counts[k] = v
	}
	return nil
}
