package events

import (
	"context"
	"sync"
)

type ConsumerPool struct {
	maxConcurrency int
	maxQueue       int

	do func(*RepoAppend)

	feeder chan *consumerTask

	lk     sync.Mutex
	active map[string][]*consumerTask
}

func NewConsumerPool(maxC, maxQ int, do func(*RepoAppend)) *ConsumerPool {
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
	key string
	val *RepoAppend
}

func (p *ConsumerPool) Add(ctx context.Context, key string, val *RepoAppend) error {
	t := &consumerTask{
		key: key,
		val: val,
	}
	p.lk.Lock()

	a, ok := p.active[key]
	if ok {
		p.active[key] = append(a, t)
		p.lk.Unlock()
		return nil
	}

	p.active[key] = []*consumerTask{}
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
			p.do(work.val)

			p.lk.Lock()
			rem, ok := p.active[work.key]
			if !ok {
				log.Errorf("should always have an 'active' entry if a worker is processing a job")
			}

			if len(rem) == 0 {
				delete(p.active, work.key)
				work = nil
			} else {
				work = rem[0]
				p.active[work.key] = rem[1:]
			}
			p.lk.Unlock()
		}
	}
}
