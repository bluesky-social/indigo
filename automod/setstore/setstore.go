package setstore

import (
	"context"
	"encoding/json"
	"io"
	"os"
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

func (s *MemSetStore) LoadFromFileJSON(p string) error {

	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(f)
	if err != nil {
		return err
	}

	var rules map[string][]string
	if err := json.Unmarshal(raw, &rules); err != nil {
		return err
	}

	for name, l := range rules {
		m := make(map[string]bool, len(l))
		for _, val := range l {
			m[val] = true
		}
		s.Sets[name] = m
	}
	return nil
}
