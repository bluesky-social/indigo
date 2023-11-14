package automod

import (
	"context"
)

type SetStore interface {
	InSet(ctx context.Context, name, val string) (bool, error)
}

// TODO: this implementation isn't race-safe (yet)!
type MemSetStore struct {
	Sets map[string]map[string]bool
}

func NewMemSetStore() MemSetStore {
	return MemSetStore{
		Sets: make(map[string]map[string]bool),
	}
}

func (s MemSetStore) InSet(ctx context.Context, name, val string) (bool, error) {
	set, ok := s.Sets[name]
	if !ok {
		// NOTE: currently returns false when entire set isn't found
		return false, nil
	}
	_, ok = set[val]
	return ok, nil
}
