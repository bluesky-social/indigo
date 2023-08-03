package autoscaling

import (
	"sync"
	"time"
)

// ThroughputManager keeps track of the number of tasks processed per second over a specified interval.
type ThroughputManager struct {
	mu       sync.Mutex
	circular []int
	pos      int
	sum      int
	interval int
}

// NewThroughputManager creates a new ThroughputManager with the specified interval.
func NewThroughputManager(interval int) *ThroughputManager {
	return &ThroughputManager{
		circular: make([]int, interval),
		interval: interval,
	}
}

// Add increments the count of tasks processed in the current second.
func (m *ThroughputManager) Add(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// increment the current position's value
	m.circular[m.pos] += n
	m.sum += n
}

// AvgThroughput returns the average number of tasks processed per second over the past interval.
func (m *ThroughputManager) AvgThroughput() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	return float64(m.sum) / float64(m.interval)
}

// shift shifts the position in the circular buffer every second, resetting the old value.
func (m *ThroughputManager) shift() {
	tick := time.NewTicker(time.Second)
	for range tick.C {
		m.mu.Lock()

		m.pos = (m.pos + 1) % m.interval
		m.sum -= m.circular[m.pos]
		m.circular[m.pos] = 0

		m.mu.Unlock()
	}
}

// Start starts the ThroughputManager
// It ticks every second, shifting the position in the circular buffer.
func (m *ThroughputManager) Start() {
	go m.shift()
}
