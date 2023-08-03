package parallel

import (
	"context"
	"sync"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers"
	"github.com/labstack/gommon/log"
	"github.com/prometheus/client_golang/prometheus"
)

// Scheduler is a parallel scheduler that will run work on a fixed number of workers
type Scheduler struct {
	maxConcurrency int
	maxQueue       int

	do func(context.Context, *events.XRPCStreamEvent) error

	feeder chan *consumerTask

	lk     sync.Mutex
	active map[string][]*consumerTask

	ident string

	// metrics
	itemsAdded     prometheus.Counter
	itemsProcessed prometheus.Counter
	itemsActive    prometheus.Counter
	workesActive   prometheus.Gauge
}

func NewScheduler(maxC, maxQ int, ident string, do func(context.Context, *events.XRPCStreamEvent) error) *Scheduler {
	p := &Scheduler{
		maxConcurrency: maxC,
		maxQueue:       maxQ,

		do: do,

		feeder: make(chan *consumerTask),
		active: make(map[string][]*consumerTask),

		ident: ident,

		itemsAdded:     schedulers.WorkItemsAdded.WithLabelValues(ident, "parallel"),
		itemsProcessed: schedulers.WorkItemsProcessed.WithLabelValues(ident, "parallel"),
		itemsActive:    schedulers.WorkItemsActive.WithLabelValues(ident, "parallel"),
		workesActive:   schedulers.WorkersActive.WithLabelValues(ident, "parallel"),
	}

	for i := 0; i < maxC; i++ {
		go p.worker()
	}

	p.workesActive.Set(float64(maxC))

	return p
}

type consumerTask struct {
	repo string
	val  *events.XRPCStreamEvent
}

func (p *Scheduler) AddWork(ctx context.Context, repo string, val *events.XRPCStreamEvent) error {
	p.itemsAdded.Inc()
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

func (p *Scheduler) worker() {
	for work := range p.feeder {
		for work != nil {
			p.itemsActive.Inc()
			if err := p.do(context.TODO(), work.val); err != nil {
				log.Errorf("event handler failed: %s", err)
			}
			p.itemsProcessed.Inc()

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
