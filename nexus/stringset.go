package main

import "sync"

type StringSet struct {
	items map[string]bool
	mu    sync.RWMutex
}

func NewStringSet() *StringSet {
	return &StringSet{
		items: make(map[string]bool),
	}
}

func (s *StringSet) Add(item string) {
	s.mu.Lock()
	s.items[item] = true
	s.mu.Unlock()
}

func (s *StringSet) AddBatch(items []string) {
	s.mu.Lock()
	for _, item := range items {
		s.items[item] = true
	}
	s.mu.Unlock()
}

func (s *StringSet) Contains(item string) bool {
	s.mu.RLock()
	exists := s.items[item]
	s.mu.RUnlock()
	return exists
}
