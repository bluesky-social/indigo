package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/puzpuzpuz/xsync/v4"
)

// Ordering guarantees for events belonging to the same DID:
//
// Live events are synchronization barriers - all prior events must complete
// before a live event can be sent, and the live event must complete before
// any subsequent events can be sent.
//
// Historical events can be sent concurrently with each other (no ordering
// between them), but cannot be sent while a live event is in-flight.
//
// Example sequence: H1, H2, L1, L2, H3, H4, L2, H5
//   - H1 and H2 sent concurrently
//   - Wait for H1 and H2 to complete, then send L1 (alone)
//   - Wait for L1 to complete, then send L2 (alone)
//   - Wait for L2 to complete, then send H3 and H4 concurrently
//   - Wait for H3 and H4 to complete, then send L3 (alone)
//   - Wait for L3 to complete, then send H5

type Outbox struct {
	logger       *slog.Logger
	mode         OutboxMode
	parallelism  int
	retryTimeout time.Duration
	webhook      *WebhookClient

	events *EventManager

	didWorkers *xsync.Map[string, *DIDWorker]

	acks     chan uint
	outgoing chan *OutboxEvt

	ctx context.Context
}

func NewOutbox(logger *slog.Logger, events *EventManager, config *TapConfig) *Outbox {
	return &Outbox{
		logger:       logger.With("component", "outbox"),
		mode:         parseOutboxMode(config.WebhookURL, config.DisableAcks),
		parallelism:  config.OutboxParallelism,
		retryTimeout: config.RetryTimeout,
		webhook: &WebhookClient{
			logger:        logger.With("component", "webhook_client"),
			webhookURL:    config.WebhookURL,
			adminPassword: config.AdminPassword,
			httpClient: &http.Client{
				Timeout: 30 * time.Second,
			},
		},
		events:     events,
		didWorkers: xsync.NewMap[string, *DIDWorker](),
		acks:       make(chan uint, config.OutboxParallelism*10000),
		outgoing:   make(chan *OutboxEvt, config.OutboxParallelism*10000),
	}
}

// Run starts the outbox workers for event delivery and cleanup.
func (o *Outbox) Run(ctx context.Context) {
	o.ctx = ctx

	if o.mode == OutboxModeWebsocketAck {
		go o.checkTimeouts(ctx)
	}

	for i := 0; i < o.parallelism; i++ {
		go o.runDelivery(ctx)
	}

	for i := 0; i < o.parallelism; i++ {
		go o.runBatchedDeletes(ctx)
	}

	<-ctx.Done()
}

// runDelivery continuously pulls from pendingIDs and delivers events
func (o *Outbox) runDelivery(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case eventID := <-o.events.pendingIDs:
			o.deliverEvent(eventID)
		}
	}
}

func (o *Outbox) deliverEvent(eventID uint) {
	evt, exists := o.events.GetEvent(eventID)
	if !exists {
		// Event was already acked/removed
		return
	}
	o.workerFor(evt.Did).addEvent(evt)
}

// workerFor gets or creates the DIDWorker for the given DID.
func (o *Outbox) workerFor(did string) *DIDWorker {
	w, _ := o.didWorkers.LoadOrCompute(did, func() (*DIDWorker, bool) {
		return &DIDWorker{
			did:            did,
			notifChan:      make(chan struct{}, 1),
			inFlightSentAt: make(map[uint]time.Time),
			outbox:         o,
			ctx:            o.ctx,
		}, false
	})
	return w
}

func (o *Outbox) sendEvent(evt *OutboxEvt) {
	eventsDelivered.Inc()
	switch o.mode {
	case OutboxModeFireAndForget, OutboxModeWebsocketAck:
		o.outgoing <- evt
	case OutboxModeWebhook:
		go o.webhook.Send(evt, o.AckEvent)
	}
}

// AckEvent marks an event as delivered and queues it for deletion.
func (o *Outbox) AckEvent(eventID uint) {
	eventsAcked.Inc()
	evt, exists := o.events.GetEvent(eventID)

	if exists {
		if worker, ok := o.didWorkers.Load(evt.Did); ok {
			worker.ackEvent(eventID)
		}
	}

	o.acks <- eventID
}

func (o *Outbox) runBatchedDeletes(ctx context.Context) {
	// drain every 10s as a fallback in the case of low-throughput ack queue
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var batch []uint

	for {
		select {
		case <-ctx.Done():
			return
		case id := <-o.acks:
			batch = append(batch, id)
			if len(batch) >= 1000 {
				if err := o.events.DeleteEvents(ctx, batch); err != nil {
					o.logger.Error("failed to delete batch of acked events", "error", err, "count", len(batch))
				}
				batch = nil
			}
		case <-ticker.C:
			if len(batch) > 0 {
				if err := o.events.DeleteEvents(ctx, batch); err != nil {
					o.logger.Error("failed to delete batch of acked events", "error", err, "count", len(batch))
				}
				batch = nil
			}
		}
	}
}

func (o *Outbox) checkTimeouts(ctx context.Context) {
	runPeriodically(ctx, o.retryTimeout, func(ctx context.Context) error {
		o.retryTimedOutEvents()
		return nil
	})
}

// retryTimedOutEvents iterates through all workers and re-queues timed out events
func (o *Outbox) retryTimedOutEvents() {
	// Get snapshot of all active workers
	workers := make([]*DIDWorker, 0)
	o.didWorkers.Range(func(key string, value *DIDWorker) bool {
		workers = append(workers, value)
		return true
	})

	for _, worker := range workers {
		timedOutIDs := worker.timedOutEvents()
		for _, id := range timedOutIDs {
			evt, exists := o.events.GetEvent(id)
			if exists {
				o.logger.Info("retrying timed out event", "id", id)
				o.sendEvent(evt)
			}
		}
	}
}

type DIDWorker struct {
	outbox         *Outbox
	ctx            context.Context
	did            string
	notifChan      chan struct{}
	pendingEvts    []uint
	inFlightSentAt map[uint]time.Time
	blockedOnLive  bool
	running        bool
	mu             sync.Mutex
}

func (w *DIDWorker) run() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.notifChan:
		}

		w.processPendingEvts()

		// Check if goroutine should exit
		w.mu.Lock()
		queueEmpty := len(w.pendingEvts) == 0
		noInFlight := len(w.inFlightSentAt) == 0

		if noInFlight {
			w.blockedOnLive = false
		}

		if queueEmpty && noInFlight {
			w.running = false
			w.mu.Unlock()
			return
		}
		w.mu.Unlock()
	}
}

// get as many pending events in flight as we can
// returns when it hits a blocking event
func (w *DIDWorker) processPendingEvts() {
	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		w.mu.Lock()
		if w.blockedOnLive {
			w.mu.Unlock()
			return
		}

		if len(w.pendingEvts) == 0 {
			w.mu.Unlock()
			return
		}
		eventID := w.pendingEvts[0]
		w.mu.Unlock()

		evt, exists := w.outbox.events.GetEvent(eventID)
		if !exists {
			// Event was already acked/removed, skip it
			w.mu.Lock()
			w.pendingEvts = w.pendingEvts[1:]
			w.mu.Unlock()
			continue
		}

		w.mu.Lock()
		if evt.Live {
			hasInFlight := len(w.inFlightSentAt) > 0
			// live event - must wait for all in-flight to clear
			if hasInFlight {
				w.mu.Unlock()
				return
			}

			w.blockedOnLive = true
		}
		w.pendingEvts = w.pendingEvts[1:]
		w.inFlightSentAt[eventID] = time.Now()
		w.mu.Unlock()

		w.outbox.sendEvent(evt)
		if evt.Live {
			return // not going to be able to send anymore in this loop so return for now
		}
	}
}

func (w *DIDWorker) addEvent(evt *OutboxEvt) {
	w.mu.Lock()

	hasInFlight := len(w.inFlightSentAt) > 0

	// Fast path: no contention, send immediately without goroutine
	if !hasInFlight {
		w.inFlightSentAt[evt.ID] = time.Now()
		w.mu.Unlock()
		w.outbox.sendEvent(evt)
		return
	}

	// Slow path: contention exists, need goroutine for ordering
	w.pendingEvts = append(w.pendingEvts, evt.ID)
	if !w.running {
		w.running = true
		go w.run()
	}
	w.mu.Unlock()

	select {
	case w.notifChan <- struct{}{}:
	default:
	}
}

func (w *DIDWorker) ackEvent(evtID uint) {
	w.mu.Lock()
	delete(w.inFlightSentAt, evtID)
	w.mu.Unlock()

	select {
	case w.notifChan <- struct{}{}:
	default:
	}
}

// checkAndRetryTimeouts checks for timed out events and returns their IDs
// Must be called without holding w.mu
func (w *DIDWorker) timedOutEvents() []uint {
	w.mu.Lock()
	defer w.mu.Unlock()

	var timedOut []uint
	now := time.Now()

	for evtId, sentAt := range w.inFlightSentAt {
		if now.Sub(sentAt) > w.outbox.retryTimeout {
			timedOut = append(timedOut, evtId)
		}
	}

	return timedOut
}
