package sequential

import (
	"context"

	"github.com/bluesky-social/indigo/events"
	"github.com/bluesky-social/indigo/events/schedulers"
	"github.com/prometheus/client_golang/prometheus"
)

// Scheduler is a sequential scheduler that will run work on a single worker
type Scheduler struct {
	Do func(context.Context, *events.XRPCStreamEvent) error

	ident string

	// metrics
	itemsAdded     prometheus.Counter
	itemsProcessed prometheus.Counter
	itemsActive    prometheus.Counter
	workersActive  prometheus.Gauge
}

func NewScheduler(ident string, do func(context.Context, *events.XRPCStreamEvent) error) *Scheduler {
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

func (s *Scheduler) AddWork(ctx context.Context, repo string, val *events.XRPCStreamEvent) error {
	s.itemsAdded.Inc()
	s.itemsActive.Inc()
	err := s.Do(ctx, val)
	s.itemsProcessed.Inc()
	return err
}
