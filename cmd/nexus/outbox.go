package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"gorm.io/gorm"
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

type OutboxMessage struct {
	ID   uint
	JSON []byte
}

type Outbox struct {
	db          *gorm.DB
	logger      *slog.Logger
	mode        OutboxMode
	parallelism int
	webhookURL  string
	httpClient  *http.Client

	eventsMngr *EventManager

	didWorkers sync.Map // map[string]*DIDWorker

	ackQueue chan uint
	events   chan *OutboxMessage

	ctx context.Context
}

func NewOutbox(db *gorm.DB, eventsMngr *EventManager, mode OutboxMode, webhookURL string, parallelism int) *Outbox {
	// logger := slog.Default().With("system", "outbox")
	return &Outbox{
		db:          db,
		logger:      slog.Default().With("system", "outbox"),
		events:      make(chan *OutboxMessage, parallelism*5000),
		mode:        mode,
		parallelism: parallelism,
		webhookURL:  webhookURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		// cache:      NewEventCache(db, logger, parallelism*5, 1000),
		eventsMngr: eventsMngr,
		ackQueue:   make(chan uint, parallelism*5000),
	}
}

// Run starts the outbox workers for event delivery and cleanup.
func (o *Outbox) Run(ctx context.Context) {
	o.ctx = ctx

	if o.mode == OutboxModeWebsocketAck {
		go o.checkTimeouts(ctx)
	}

	// Run multiple delivery workers for parallelism across DIDs
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
		case eventID := <-o.eventsMngr.pendingIDs:
			o.deliverEvent(eventID)
		}
	}
}

func (o *Outbox) deliverEvent(eventID uint) {
	evt, exists := o.eventsMngr.GetEvent(eventID)
	if !exists {
		// Event was already acked/removed
		return
	}

	if val, ok := o.didWorkers.Load(evt.Did); ok {
		worker := val.(*DIDWorker)
		worker.addEvent(evt)
		return
	}

	worker := &DIDWorker{
		did:            evt.Did,
		notifChan:      make(chan struct{}, 1),
		inFlightSentAt: make(map[uint]time.Time),
		outbox:         o,
		ctx:            o.ctx,
	}
	actual, _ := o.didWorkers.LoadOrStore(evt.Did, worker)
	actual.(*DIDWorker).addEvent(evt)
}

func (o *Outbox) sendEvent(evt *OutboxEvt) {
	switch o.mode {
	case OutboxModeFireAndForget, OutboxModeWebsocketAck:
		// Marshal to JSON before outputting to avoid marshaling serially
		jsonBytes, err := json.Marshal(evt)
		if err != nil {
			o.logger.Error("failed to marshal event to JSON", "error", err, "id", evt.ID)
			return
		}
		o.events <- &OutboxMessage{
			ID:   evt.ID,
			JSON: jsonBytes,
		}
	case OutboxModeWebhook:
		go o.sendWebhook(evt)
	}
}

// AckEvent marks an event as delivered and queues it for deletion.
func (o *Outbox) AckEvent(eventID uint) {
	evt, exists := o.eventsMngr.GetEvent(eventID)

	if exists {
		if val, ok := o.didWorkers.Load(evt.Did); ok {
			worker := val.(*DIDWorker)
			worker.ackEvent(eventID)
		}
	}

	o.ackQueue <- eventID
}

func (o *Outbox) sendWebhook(evt *OutboxEvt) {
	retries := 0
	for {
		if err := o.postWebhook(evt); err != nil {
			o.logger.Warn("webhook failed, retrying", "error", err, "id", evt.ID, "retries", retries)
			time.Sleep(backoff(retries, 10))
			retries++
			continue
		}

		o.AckEvent(evt.ID)
		return
	}
}

func (o *Outbox) postWebhook(evt *OutboxEvt) error {
	req, err := http.NewRequest("POST", o.webhookURL, bytes.NewReader(evt.Event))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned non-2xx status: %d", resp.StatusCode)
	}

	return nil
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
		case id := <-o.ackQueue:
			batch = append(batch, id)
			if len(batch) >= 1000 {
				if err := o.eventsMngr.DeleteEvents(batch); err != nil {
					o.logger.Error("failed to delete batch of acked events", "error", err, "count", len(batch))
				}
				batch = nil
			}
		case <-ticker.C:
			if len(batch) > 0 {
				if err := o.eventsMngr.DeleteEvents(batch); err != nil {
					o.logger.Error("failed to delete batch of acked events", "error", err, "count", len(batch))
				}
				batch = nil
			}
		}
	}
}

func (o *Outbox) checkTimeouts(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.retryTimedOutEvents()
		}
	}
}

// retryTimedOutEvents iterates through all workers and re-queues timed out events
func (o *Outbox) retryTimedOutEvents() {
	// Get snapshot of all active workers
	workers := make([]*DIDWorker, 0)
	o.didWorkers.Range(func(key, value interface{}) bool {
		workers = append(workers, value.(*DIDWorker))
		return true
	})

	for _, worker := range workers {
		timedOutIDs := worker.timedOutEvents()
		for _, id := range timedOutIDs {
			evt, exists := o.eventsMngr.GetEvent(id)
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

		evt, exists := w.outbox.eventsMngr.GetEvent(eventID)
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
		if now.Sub(sentAt) > 10*time.Second {
			timedOut = append(timedOut, evtId)
		}
	}

	return timedOut
}
