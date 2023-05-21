package events

import (
	"context"
	"sync"
)

type ConsumerPool struct {
	maxConcurrency int
	maxQueue       int

	do func(*XRPCStreamEvent) error

	feeder chan *consumerTask

	lk     sync.Mutex
	active map[string][]*consumerTask
}

func NewConsumerPool(maxC, maxQ int, do func(*XRPCStreamEvent) error) *ConsumerPool {
	p := &ConsumerPool{
		maxConcurrency: maxC,
		maxQueue:       maxQ,

		do: do,

		feeder: make(chan *consumerTask),
		active: make(map[string][]*consumerTask),
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

func (p *ConsumerPool) Add(ctx context.Context, repo string, val *XRPCStreamEvent) error {
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

func (p *ConsumerPool) worker() {
	for work := range p.feeder {
		for work != nil {
			if err := p.do(work.val); err != nil {
				log.Errorf("event handler failed: %s", err)
			}

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
