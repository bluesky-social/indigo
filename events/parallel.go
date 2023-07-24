package events

import (
	"context"
	"sync"
)

type Scheduler interface {
	AddWork(ctx context.Context, repo string, val *XRPCStreamEvent) error
}

type SequentialScheduler struct {
	Do func(context.Context, *XRPCStreamEvent) error
}

func (s *SequentialScheduler) AddWork(ctx context.Context, repo string, val *XRPCStreamEvent) error {
	return s.Do(ctx, val)
}

type ParallelConsumerPool struct {
	maxConcurrency int
	maxQueue       int

	do func(context.Context, *XRPCStreamEvent) error

	feeder chan *consumerTask

	lk     sync.Mutex
	active map[string][]*consumerTask

	ident string
}

func NewConsumerPool(maxC, maxQ int, ident string, do func(context.Context, *XRPCStreamEvent) error) *ParallelConsumerPool {
	p := &ParallelConsumerPool{
		maxConcurrency: maxC,
		maxQueue:       maxQ,

		do: do,

		feeder: make(chan *consumerTask),
		active: make(map[string][]*consumerTask),

		ident: ident,
	}

	for i := 0; i < maxC; i++ {
		go p.worker()
	}

	return p
}

type consumerTask struct {
	repo string
	val  *XRPCStreamEvent
}

func (p *ParallelConsumerPool) AddWork(ctx context.Context, repo string, val *XRPCStreamEvent) error {
	workItemsAdded.WithLabelValues(p.ident).Inc()
	t := &consumerTask{
		repo: repo,
		val:  val,
	}
	p.lk.Lock()

	a, ok := p.active[repo]
	if ok {
		p.active[repo] = append(a, t)
		p.lk.Unlock()
		return nil
	}

	p.active[repo] = []*consumerTask{}
	p.lk.Unlock()

	select {
	case p.feeder <- t:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *ParallelConsumerPool) worker() {
	for work := range p.feeder {
		for work != nil {
			workItemsInFlight.WithLabelValues(p.ident).Inc()
			if err := p.do(context.TODO(), work.val); err != nil {
				log.Errorf("event handler failed: %s", err)
			}
			workItemsInFlight.WithLabelValues(p.ident).Dec()
			workItemsProcessed.WithLabelValues(p.ident).Inc()

			p.lk.Lock()
			rem, ok := p.active[work.repo]
			if !ok {
				log.Errorf("should always have an 'active' entry if a worker is processing a job")
			}

			if len(rem) == 0 {
				delete(p.active, work.repo)
				work = nil
			} else {
				work = rem[0]
				p.active[work.repo] = rem[1:]
			}
			p.lk.Unlock()
		}
	}
}
