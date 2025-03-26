package parallel

import (
	"context"
	"log/slog"
	"sync"

	"github.com/bluesky-social/indigo/cmd/relay/events"
	"github.com/bluesky-social/indigo/events/schedulers"

	"github.com/prometheus/client_golang/prometheus"
)

// Scheduler is a parallel scheduler that will run work on a fixed number of workers
type Scheduler struct {
	maxConcurrency int

	do func(context.Context, *events.XRPCStreamEvent) error

	feeder chan *consumerTask
	out    chan struct{}

	lk     sync.Mutex
	active map[string][]*consumerTask

	// metrics
	itemsAdded     prometheus.Counter
	itemsProcessed prometheus.Counter
	itemsActive    prometheus.Counter
	workesActive   prometheus.Gauge

	log *slog.Logger
}

// NewScheduler builds a worker pool of maxC threads calling the do() function pointer
// info and error logs include {"system":"parallel-scheduler", hostLogKey: host}
func NewScheduler(maxC int, hostLogKey, host string, do func(context.Context, *events.XRPCStreamEvent) error) *Scheduler {
	p := &Scheduler{
		maxConcurrency: maxC,

		do: do,

		feeder: make(chan *consumerTask),
		active: make(map[string][]*consumerTask),
		out:    make(chan struct{}),

		itemsAdded:     schedulers.WorkItemsAdded.WithLabelValues(host, "parallel"),
		itemsProcessed: schedulers.WorkItemsProcessed.WithLabelValues(host, "parallel"),
		itemsActive:    schedulers.WorkItemsActive.WithLabelValues(host, "parallel"),
		workesActive:   schedulers.WorkersActive.WithLabelValues(host, "parallel"),

		log: slog.Default().With("system", "parallel-scheduler", hostLogKey, host),
	}

	for i := 0; i < maxC; i++ {
		go p.worker()
	}

	p.workesActive.Set(float64(maxC))

	return p
}

func (p *Scheduler) Shutdown() {
	p.log.Info("shutting down parallel scheduler")

	for i := 0; i < p.maxConcurrency; i++ {
		p.feeder <- &consumerTask{
			control: "stop",
		}
	}

	close(p.feeder)

	for i := 0; i < p.maxConcurrency; i++ {
		<-p.out
	}

	p.log.Info("parallel scheduler shutdown complete")
}

type consumerTask struct {
	repo    string
	val     *events.XRPCStreamEvent
	control string
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
			if work.control == "stop" {
				p.out <- struct{}{}
				return
			}

			p.itemsActive.Inc()
			if err := p.do(context.TODO(), work.val); err != nil {
				p.log.Error("event handler failed", "err", err)
			}
			p.itemsProcessed.Inc()

			p.lk.Lock()
			rem, ok := p.active[work.repo]
			if !ok {
				p.log.Error("should always have an 'active' entry if a worker is processing a job")
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
