package autoscaling

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers"
	"github.com/prometheus/client_golang/prometheus"
)

// Scheduler is a scheduler that will scale up and down the number of workers based on the throughput of the workers.
type Scheduler struct {
	concurrency    int
	maxConcurrency int

	do func(context.Context, *events.XRPCStreamEvent) error

	feeder      chan *consumerTask
	workerGroup sync.WaitGroup

	lk        sync.Mutex
	active    map[string][]*consumerTask
	maxActive int

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

	// autoscaling
	throughputManager  *ThroughputManager
	autoscaleFrequency time.Duration
	autoscalerIn       chan struct{}
	autoscalerOut      chan struct{}

	log *slog.Logger
}

type AutoscaleSettings struct {
	Concurrency                 int
	MaxConcurrency              int
	AutoscaleFrequency          time.Duration
	ThroughputBucketCount       int
	ThroughputBucketDuration    time.Duration
	MaximumBufferedItemsPerRepo int
}

// DefaultAutoscaleSettings returns the default autoscale settings.
// Concurrency is the number of workers to start with.
// MaxConcurrency is the maximum number of workers to scale up to.
// AutoscaleFrequency is the frequency to check the average throughput.
// ThroughputBucketCount is the number of buckets to use to calculate the average throughput.
// ThroughputBucketDuration is the duration of each bucket.
// By default we check the average throughput over the last 60 seconds with 1 second buckets
// We make an autoscaling decision every 5 seconds.
// We start with 1 worker and scale up to 32 workers.
func DefaultAutoscaleSettings() AutoscaleSettings {
	return AutoscaleSettings{
		Concurrency:                 1,
		MaxConcurrency:              32,
		AutoscaleFrequency:          5 * time.Second,
		ThroughputBucketCount:       60,
		ThroughputBucketDuration:    time.Second,
		MaximumBufferedItemsPerRepo: 100,
	}
}

func NewScheduler(autoscaleSettings AutoscaleSettings, ident string, do func(context.Context, *events.XRPCStreamEvent) error) *Scheduler {
	labels := prometheus.Labels{"pool": ident, "scheduler_type": "autoscaling"}
	p := &Scheduler{
		concurrency:    autoscaleSettings.Concurrency,
		maxConcurrency: autoscaleSettings.MaxConcurrency,

		do: do,

		feeder:      make(chan *consumerTask),
		active:      make(map[string][]*consumerTask),
		maxActive:   autoscaleSettings.MaximumBufferedItemsPerRepo,
		workerGroup: sync.WaitGroup{},

		ident: ident,

		itemsAdded:     schedulers.WorkItemsAdded.WithLabelValues(ident, "autoscaling"),
		itemsProcessed: schedulers.WorkItemsProcessed.WithLabelValues(ident, "autoscaling"),
		itemsActive:    schedulers.WorkItemsActive.WithLabelValues(ident, "autoscaling"),
		workersActive:  schedulers.WorkersActive.WithLabelValues(ident, "autoscaling"),
		repoQueueDepth: schedulers.RepoQueueDepth.WithLabelValues(ident, "autoscaling"),

		queuedItems:   schedulers.QueuedItems.MustCurryWith(labels),
		itemsInFlight: schedulers.ItemsInFlight.MustCurryWith(labels),
		queueWait:     schedulers.QueueWaitSeconds.MustCurryWith(labels),
		feederBlock:   schedulers.FeederBlockSeconds.MustCurryWith(labels),
		itemProcess:   schedulers.ItemProcessSeconds.MustCurryWith(labels),

		// autoscaling
		// By default, the ThroughputManager will calculate the average throughput over the last 60 seconds.
		throughputManager: NewThroughputManager(
			autoscaleSettings.ThroughputBucketCount,
			autoscaleSettings.ThroughputBucketDuration,
		),
		autoscaleFrequency: autoscaleSettings.AutoscaleFrequency,
		autoscalerIn:       make(chan struct{}),
		autoscalerOut:      make(chan struct{}),

		log: slog.Default().With("system", "autoscaling-scheduler"),
	}

	for i := 0; i < p.concurrency; i++ {
		go p.worker()
	}

	go p.autoscale()

	return p
}

func (p *Scheduler) Shutdown() {
	p.log.Debug("shutting down autoscaling scheduler", "ident", p.ident)

	// stop autoscaling
	p.autoscalerIn <- struct{}{}
	close(p.autoscalerIn)
	<-p.autoscalerOut

	p.log.Debug("stopping autoscaling scheduler workers")
	// stop workers
	for i := 0; i < p.concurrency; i++ {
		p.feeder <- &consumerTask{signal: "stop"}
	}
	close(p.feeder)

	p.log.Debug("waiting for autoscaling scheduler workers to stop")

	p.workerGroup.Wait()

	p.log.Debug("stopping autoscaling scheduler throughput manager")
	p.throughputManager.Stop()

	p.log.Debug("autoscaling scheduler shutdown complete")
}

// Add autoscaling function
func (p *Scheduler) autoscale() {
	p.throughputManager.Start()
	tick := time.NewTicker(p.autoscaleFrequency)
	defer tick.Stop()
	for {
		select {
		case <-p.autoscalerIn:
			p.autoscalerOut <- struct{}{}
			close(p.autoscalerOut)
			return
		case <-tick.C:
			avg := p.throughputManager.AvgThroughput()
			if avg > float64(p.concurrency) && p.concurrency < p.maxConcurrency {
				p.concurrency++
				go p.worker()
			} else if avg < float64(p.concurrency-1) && p.concurrency > 1 {
				p.concurrency--
				p.feeder <- &consumerTask{signal: "stop"}
			}
		}
	}
}

type consumerTask struct {
	repo   string
	val    *events.XRPCStreamEvent
	signal string
	wg     *sync.WaitGroup

	// kind is resolved once at enqueue and reused at dequeue, so that the
	// paired gauge Inc/Dec always land on the same label set.
	kind     string
	enqueued time.Time
}

func (p *Scheduler) AddWork(ctx context.Context, repo string, val *events.XRPCStreamEvent) error {
	p.itemsAdded.Inc()
	p.throughputManager.Add(1)
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
		if len(a) >= p.maxActive {
			// TODO: Need a pattern to prevent buffer stuffing
		}
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
	p.log.Debug("starting autoscaling worker", "ident", p.ident)
	p.workersActive.Inc()
	p.workerGroup.Add(1)
	defer p.workerGroup.Done()
	for work := range p.feeder {
		for work != nil {
			// Check if the work item contains a signal to stop the worker.
			if work.signal == "stop" {
				p.log.Debug("stopping autoscaling worker", "ident", p.ident)
				p.workersActive.Dec()
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
