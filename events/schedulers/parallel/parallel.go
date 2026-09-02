package parallel

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers"

	"github.com/prometheus/client_golang/prometheus"
)

// Scheduler is a parallel scheduler that will run work on a fixed number of workers.
//
// Notably, this scheduler uses a per-DID task tracker to ensure that events are not processed concurrently for the same account. This does *not* mean that all events for the same DID are consistently processed by the same worker.
type Scheduler struct {
	maxConcurrency int
	maxQueue       int

	do func(context.Context, *events.XRPCStreamEvent) error

	feeder chan *consumerTask
	out    chan struct{}

	lk     sync.Mutex
	active map[string][]*consumerTask

	ident string

	// metrics
	itemsAdded     prometheus.Counter
	itemsProcessed prometheus.Counter
	itemsActive    prometheus.Counter
	workersActive  prometheus.Gauge
	repoQueueDepth prometheus.Observer

	// curried on (pool, scheduler_type); the remaining label is event_kind
	queuedItems   *prometheus.GaugeVec
	itemsInFlight *prometheus.GaugeVec
	queueWait     prometheus.ObserverVec
	feederBlock   prometheus.ObserverVec
	itemProcess   prometheus.ObserverVec

	log *slog.Logger
}

func NewScheduler(maxC, maxQ int, ident string, do func(context.Context, *events.XRPCStreamEvent) error) *Scheduler {
	labels := prometheus.Labels{"pool": ident, "scheduler_type": "parallel"}
	p := &Scheduler{
		maxConcurrency: maxC,
		maxQueue:       maxQ,

		do: do,

		feeder: make(chan *consumerTask),
		active: make(map[string][]*consumerTask),
		out:    make(chan struct{}),

		ident: ident,

		itemsAdded:     schedulers.WorkItemsAdded.WithLabelValues(ident, "parallel"),
		itemsProcessed: schedulers.WorkItemsProcessed.WithLabelValues(ident, "parallel"),
		itemsActive:    schedulers.WorkItemsActive.WithLabelValues(ident, "parallel"),
		workersActive:  schedulers.WorkersActive.WithLabelValues(ident, "parallel"),
		repoQueueDepth: schedulers.RepoQueueDepth.WithLabelValues(ident, "parallel"),

		queuedItems:   schedulers.QueuedItems.MustCurryWith(labels),
		itemsInFlight: schedulers.ItemsInFlight.MustCurryWith(labels),
		queueWait:     schedulers.QueueWaitSeconds.MustCurryWith(labels),
		feederBlock:   schedulers.FeederBlockSeconds.MustCurryWith(labels),
		itemProcess:   schedulers.ItemProcessSeconds.MustCurryWith(labels),

		log: slog.Default().With("system", "parallel-scheduler"),
	}

	for range maxC {
		go p.worker()
	}

	p.workersActive.Set(float64(maxC))

	return p
}

func (p *Scheduler) Shutdown() {
	p.log.Info("shutting down parallel scheduler", "ident", p.ident)

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

	// kind is resolved once at enqueue and reused at dequeue, so that the
	// paired gauge Inc/Dec always land on the same label set.
	kind     string
	enqueued time.Time
}

func (p *Scheduler) AddWork(ctx context.Context, repo string, val *events.XRPCStreamEvent) error {
	p.itemsAdded.Inc()
	kind := val.Kind()
	t := &consumerTask{
		repo:     repo,
		val:      val,
		kind:     kind,
		enqueued: time.Now(),
	}
	// Counted from enqueue rather than from the append below, so that the item
	// blocked on the feeder is included: during a backup that item is queued
	// too, and it is the one holding up the caller's read loop.
	p.queuedItems.WithLabelValues(kind).Inc()

	p.lk.Lock()

	a, ok := p.active[repo]
	if ok {
		p.repoQueueDepth.Observe(float64(len(a)))
		p.active[repo] = append(a, t)
		p.lk.Unlock()
		return nil
	}

	p.repoQueueDepth.Observe(0)
	p.active[repo] = []*consumerTask{}
	p.lk.Unlock()

	blockedAt := time.Now()
	select {
	case p.feeder <- t:
		p.feederBlock.WithLabelValues(kind).Observe(time.Since(blockedAt).Seconds())
		return nil
	case <-ctx.Done():
		p.feederBlock.WithLabelValues(kind).Observe(time.Since(blockedAt).Seconds())
		// This item never reaches a worker, so nothing downstream will
		// decrement for it.
		//
		// NOTE: the empty p.active[repo] placeholder reserved above is also
		// left behind with no worker to clear it. That is pre-existing
		// behaviour and only reachable while the stream is shutting down; see
		// the queueing notes in the PR description.
		p.queuedItems.WithLabelValues(kind).Dec()
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
			p.queuedItems.WithLabelValues(work.kind).Dec()
			p.queueWait.WithLabelValues(work.kind).Observe(time.Since(work.enqueued).Seconds())
			p.itemsInFlight.WithLabelValues(work.kind).Inc()

			startedAt := time.Now()
			if err := p.do(context.TODO(), work.val); err != nil {
				p.log.Error("event handler failed", "err", err)
			}
			p.itemProcess.WithLabelValues(work.kind).Observe(time.Since(startedAt).Seconds())
			p.itemsInFlight.WithLabelValues(work.kind).Dec()
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
