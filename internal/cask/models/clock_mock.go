package models

import (
	"sync"
	"time"
)

// MockClock is a controllable clock for testing
type MockClock struct {
	mu      sync.Mutex
	current time.Time
	waiters []*mockWaiter
}

type mockWaiter struct {
	deadline time.Time
	ch       chan time.Time
}

func NewMockClock(start time.Time) *MockClock {
	return &MockClock{current: start}
}

func (c *MockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *MockClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch := make(chan time.Time, 1)
	deadline := c.current.Add(d)

	// If deadline already passed, fire immediately
	if !deadline.After(c.current) {
		ch <- c.current
		return ch
	}

	c.waiters = append(c.waiters, &mockWaiter{
		deadline: deadline,
		ch:       ch,
	})
	return ch
}

// Advance moves the clock forward and wakes any expired waiters
func (c *MockClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.current = c.current.Add(d)
	now := c.current

	// Find and wake expired waiters
	var remaining []*mockWaiter
	var expired []*mockWaiter
	for _, w := range c.waiters {
		if !w.deadline.After(now) {
			expired = append(expired, w)
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
	c.mu.Unlock()

	// Send to channels outside the lock to avoid deadlocks
	for _, w := range expired {
		select {
		case w.ch <- now:
		default:
		}
	}
}

// Set sets the clock to a specific time and wakes any expired waiters
func (c *MockClock) Set(t time.Time) {
	c.mu.Lock()
	c.current = t

	var remaining []*mockWaiter
	var expired []*mockWaiter
	for _, w := range c.waiters {
		if !w.deadline.After(t) {
			expired = append(expired, w)
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
	c.mu.Unlock()

	for _, w := range expired {
		select {
		case w.ch <- t:
		default:
		}
	}
}

// WaiterCount returns the number of goroutines waiting on After()
func (c *MockClock) WaiterCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}
