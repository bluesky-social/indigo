package flagstore

import (
	"context"
)

type MemFlagStore struct {
	Data map[string][]string
}

func NewMemFlagStore() MemFlagStore {
	return MemFlagStore{
		Data: make(map[string][]string),
	}
}

func (s MemFlagStore) Get(ctx context.Context, key string) ([]string, error) {
	v, ok := s.Data[key]
	if !ok {
		return []string{}, nil
	}
	return v, nil
}

func (s MemFlagStore) Add(ctx context.Context, key string, flags []string) error {
	v, ok := s.Data[key]
	if !ok {
		v = []string{}
	}
	v = append(v, flags...)
	v = dedupeStrings(v)
	s.Data[key] = v
	return nil
}

// does not error if flags not in set
func (s MemFlagStore) Remove(ctx context.Context, key string, flags []string) error {
	if len(flags) == 0 {
		return nil
	}
	v, ok := s.Data[key]
	if !ok {
		v = []string{}
	}
	m := make(map[string]bool, len(v))
	for _, f := range v {
		m[f] = true
	}
	for _, f := range flags {
		delete(m, f)
	}
	out := []string{}
	for f, _ := range m {
		out = append(out, f)
	}
	s.Data[key] = out
	return nil
}
