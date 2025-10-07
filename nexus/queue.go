package main

import "sync"

type BackfillQueue struct {
	queue []string
	head  int
	mu    sync.Mutex
	cond  *sync.Cond
}

func NewBackfillQueue() *BackfillQueue {
	q := &BackfillQueue{
		queue: make([]string, 0),
		head:  0,
	}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *BackfillQueue) Enqueue(did string) int {
	q.mu.Lock()
	q.queue = append(q.queue, did)
	depth := len(q.queue) - q.head
	q.mu.Unlock()
	q.cond.Signal()
	return depth
}

func (q *BackfillQueue) Dequeue() string {
	q.mu.Lock()
	for q.head >= len(q.queue) {
		q.cond.Wait()
	}
	did := q.queue[q.head]
	q.head++

	// trim queue every 10000 elements
	if q.head > 10000 {
		q.queue = append([]string(nil), q.queue[q.head:]...)
		q.head = 0
	}
	q.mu.Unlock()

	return did
}
