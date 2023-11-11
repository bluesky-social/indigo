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
	GetCount(ctx context.Context, key, period string) (int, error)
	Increment(ctx context.Context, key string) (int, error)
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

func PeriodKey(key, period string) string {
	switch period {
	case PeriodTotal:
		return key
	case PeriodDay:
		t := time.Now().UTC().Format(time.DateOnly)
		return fmt.Sprintf("%s:%s", key, t)
	case PeriodHour:
		t := time.Now().UTC().Format(time.RFC3339)[0:13]
		return fmt.Sprintf("%s:%s", key, t)
	default:
		slog.Warn("unhandled counter period", "period", period)
		return key
	}
}

func (s *MemCountStore) GetCount(ctx context.Context, key, period string) (int, error) {
	v, ok := s.Counts[PeriodKey(key, period)]
	if !ok {
		return 0, nil
	}
	return v, nil
}

func (s *MemCountStore) Increment(ctx context.Context, key string) error {
	for _, p := range []string{PeriodTotal, PeriodDay, PeriodHour} {
		k := PeriodKey(key, p)
		v, ok := s.Counts[k]
		if !ok {
			v = 0
		}
		v = v + 1
		s.Counts[k] = v
	}
	return nil
}
