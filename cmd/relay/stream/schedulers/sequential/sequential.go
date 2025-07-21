package sequential

import (
	"context"

	"github.com/bluesky-social/indigo/cmd/relay/stream"
	"github.com/bluesky-social/indigo/cmd/relay/stream/schedulers"

	"github.com/prometheus/client_golang/prometheus"
)

// var log = slog.Default().With("system", "sequential-scheduler")

// Scheduler is a sequential scheduler that will run work on a single worker
type Scheduler struct {
	Do func(context.Context, *stream.XRPCStreamEvent) error

	ident string

	// metrics
	itemsAdded     prometheus.Counter
	itemsProcessed prometheus.Counter
	itemsActive    prometheus.Counter
	workersActive  prometheus.Gauge
}

func NewScheduler(ident string, do func(context.Context, *stream.XRPCStreamEvent) error) *Scheduler {
	p := &Scheduler{
		Do: do,

		ident: ident,

		itemsAdded:     schedulers.WorkItemsAdded.WithLabelValues(ident, "sequential"),
		itemsProcessed: schedulers.WorkItemsProcessed.WithLabelValues(ident, "sequential"),
		itemsActive:    schedulers.WorkItemsActive.WithLabelValues(ident, "sequential"),
		workersActive:  schedulers.WorkersActive.WithLabelValues(ident, "sequential"),
	}

	p.workersActive.Set(1)

	return p
}

func (p *Scheduler) Shutdown() {
	p.workersActive.Set(0)
}

func (s *Scheduler) AddWork(ctx context.Context, repo string, val *stream.XRPCStreamEvent) error {
	s.itemsAdded.Inc()
	s.itemsActive.Inc()
	err := s.Do(ctx, val)
	s.itemsProcessed.Inc()
	return err
}
