package autoscaling

import (
	"sync"
	"time"
)

// ThroughputManager keeps track of the number of tasks processed per bucketDuration over a specified bucketCount.
type ThroughputManager struct {
	mu             sync.Mutex
	circular       []int
	pos            int
	sum            int
	bucketCount    int
	bucketDuration time.Duration
}

// NewThroughputManager creates a new ThroughputManager with the specified interval.
func NewThroughputManager(bucketCount int, bucketDuration time.Duration) *ThroughputManager {
	return &ThroughputManager{
		circular:       make([]int, bucketCount),
		bucketCount:    bucketCount,
		bucketDuration: bucketDuration,
	}
}

// Add increments the count of tasks processed in the current bucket
func (m *ThroughputManager) Add(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// increment the current position's value
	m.circular[m.pos] += n
	m.sum += n
}

// AvgThroughput returns the average number of tasks processed per
// bucketDuration over the past bucketCount buckets.
func (m *ThroughputManager) AvgThroughput() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	return float64(m.sum) / float64(m.bucketCount)
}

// shift shifts the position in the circular buffer every bucketDuration, resetting the old value.
func (m *ThroughputManager) shift() {
	tick := time.NewTicker(m.bucketDuration)
	for range tick.C {
		m.mu.Lock()

		m.pos = (m.pos + 1) % m.bucketCount
		m.sum -= m.circular[m.pos]
		m.circular[m.pos] = 0

		m.mu.Unlock()
	}
}

// Start starts the ThroughputManager
// It ticks every bucketDuration, shifting the position in the circular buffer.
func (m *ThroughputManager) Start() {
	go m.shift()
}
