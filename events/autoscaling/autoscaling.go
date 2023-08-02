package autoscaling

import (
	"context"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/events"
	"github.com/labstack/gommon/log"
	"github.com/prometheus/client_golang/prometheus"
)

type ConsumerPool struct {
	concurrency    int
	maxConcurrency int

	do func(context.Context, *events.XRPCStreamEvent) error

	feeder chan *consumerTask

	lk     sync.Mutex
	active map[string][]*consumerTask

	ident string

	// metrics
	itemsAdded     prometheus.Counter
	itemsProcessed prometheus.Counter
	itemsActive    prometheus.Counter
	workersAcrive  prometheus.Gauge

	// autoscaling
	throughputManager *ThroughputManager
}

func NewConsumerPool(concurrency, maxC int, ident string, do func(context.Context, *events.XRPCStreamEvent) error) *ConsumerPool {
	p := &ConsumerPool{
		concurrency:    concurrency,
		maxConcurrency: maxC,

		do: do,

		feeder: make(chan *consumerTask),
		active: make(map[string][]*consumerTask),

		ident: ident,

		itemsAdded:     workItemsAdded.WithLabelValues(ident, "autoscaling"),
		itemsProcessed: workItemsProcessed.WithLabelValues(ident, "autoscaling"),
		itemsActive:    workItemsActive.WithLabelValues(ident, "autoscaling"),
		workersAcrive:  workersActive.WithLabelValues(ident, "autoscaling"),

		// autoscaling
		// By default, the ThroughputManager will calculate the average throughput over the last 60 seconds.
		throughputManager: NewThroughputManager(60),
	}

	for i := 0; i < concurrency; i++ {
		go p.worker()
	}

	go p.autoscale()

	return p
}

// Add autoscaling function
func (p *ConsumerPool) autoscale() {
	p.throughputManager.Start()
	tick := time.NewTicker(time.Second * 5) // adjust as needed
	for range tick.C {
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

type consumerTask struct {
	repo   string
	val    *events.XRPCStreamEvent
	signal string
}

func (p *ConsumerPool) AddWork(ctx context.Context, repo string, val *events.XRPCStreamEvent) error {
	p.itemsAdded.Inc()
	p.throughputManager.Add(1)
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
	log.Infof("starting autoscaling worker for %s", p.ident)
	p.workersAcrive.Inc()
	for work := range p.feeder {
		for work != nil {
			// Check if the work item contains a signal to stop the worker.
			if work.signal == "stop" {
				log.Infof("stopping autoscaling worker for %s", p.ident)
				p.workersAcrive.Dec()
				return
			}

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
