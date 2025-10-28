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

	"github.com/bluesky-social/indigo/nexus/models"
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
// Example sequence: H1, H2, L1, H3, H4, L2, H5
//   - H1 and H2 sent concurrently
//   - Wait for H1 and H2 to complete, then send L1 (alone)
//   - Wait for L1 to complete, then send H3 and H4 concurrently
//   - Wait for H3 and H4 to complete, then send L2 (alone)
//   - Wait for L2 to complete, then send H5

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

type Outbox struct {
	db         *gorm.DB
	logger     *slog.Logger
	events     chan *OutboxEvt
	mode       OutboxMode
	webhookURL string
	httpClient *http.Client

	eventCache   map[uint]*OutboxEvt // ID -> event
	pendingIDs   chan uint           // Buffered channel of event IDs
	cacheMu      sync.RWMutex
	lastLoadedID uint

	didWorkers map[string]*DIDWorker
	workersMu  sync.Mutex

	ctx context.Context
}

func NewOutbox(db *gorm.DB, mode OutboxMode, webhookURL string) *Outbox {
	return &Outbox{
		db:         db,
		logger:     slog.Default().With("system", "outbox"),
		events:     make(chan *OutboxEvt, 10000),
		mode:       mode,
		webhookURL: webhookURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		eventCache: make(map[uint]*OutboxEvt),
		pendingIDs: make(chan uint, 1000000),
		didWorkers: make(map[string]*DIDWorker),
	}
}

func (o *Outbox) Run(ctx context.Context) {
	o.ctx = ctx

	// Webhook mode doesn't need timeout checker since sendWebhook() retries indefinitely
	if o.mode == OutboxModeWebsocketAck {
		go o.checkTimeouts(ctx)
	}
	go o.runCacheLoader(ctx)
	go o.runDelivery(ctx)

	<-ctx.Done()
}

// continuously load events from DB into memory cache
func (o *Outbox) runCacheLoader(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.loadMoreEvents()
		}
	}
}

func (o *Outbox) loadMoreEvents() {
	o.cacheMu.RLock()
	lastID := o.lastLoadedID
	o.cacheMu.RUnlock()

	batchSize := 1000

	var events []models.OutboxBuffer
	if err := o.db.Where("id > ?", lastID).
		Order("id ASC").
		Limit(batchSize).
		Find(&events).Error; err != nil {
		o.logger.Error("failed to load events into cache", "error", err)
		return
	}

	if len(events) == 0 {
		return
	}

	o.cacheMu.Lock()
	for _, dbEvt := range events {
		var outboxEvt OutboxEvt
		if err := json.Unmarshal([]byte(dbEvt.Data), &outboxEvt); err != nil {
			o.logger.Error("failed to unmarshal cached event", "error", err, "id", dbEvt.ID)
			continue
		}

		outboxEvt.ID = dbEvt.ID
		o.eventCache[dbEvt.ID] = &outboxEvt

		select {
		case o.pendingIDs <- dbEvt.ID:
		default:
			// Channel full, will try again next time
		}

		if dbEvt.ID > o.lastLoadedID {
			o.lastLoadedID = dbEvt.ID
		}
	}
	o.cacheMu.Unlock()

	o.logger.Info("loaded events into cache", "count", len(events), "cacheSize", len(o.eventCache))
}

// runDelivery continuously pulls from pendingIDs and delivers events
func (o *Outbox) runDelivery(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case eventID := <-o.pendingIDs:
			o.deliverEvent(eventID)
		}
	}
}

func (o *Outbox) deliverEvent(eventID uint) {
	o.cacheMu.RLock()
	outboxEvt, exists := o.eventCache[eventID]
	o.cacheMu.RUnlock()

	if !exists {
		// Event was already acked/removed
		return
	}

	did := outboxEvt.DID()

	o.workersMu.Lock()
	worker, exists := o.didWorkers[did]
	if !exists {
		worker = &DIDWorker{
			did:            did,
			notifChan:      make(chan struct{}, 1),
			inFlightSentAt: make(map[uint]time.Time),
			outbox:         o,
			ctx:            o.ctx,
		}
		o.didWorkers[did] = worker
	}
	o.workersMu.Unlock()

	worker.addEvent(outboxEvt)
}

func (o *Outbox) sendEvent(evt *OutboxEvt) {
	switch o.mode {
	case OutboxModeFireAndForget:
		o.events <- evt
	case OutboxModeWebsocketAck:
		o.events <- evt
	case OutboxModeWebhook:
		go o.sendWebhook(evt)
	}
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
		w.mu.Lock()
		if w.blockedOnLive {
			return // can't proceed, break out of send loop and wait for acks
		}

		if len(w.pendingEvts) == 0 {
			w.mu.Unlock()
			return
		}
		eventID := w.pendingEvts[0]
		w.mu.Unlock()

		w.outbox.cacheMu.RLock()
		outboxEvt, exists := w.outbox.eventCache[eventID]
		w.outbox.cacheMu.RUnlock()

		if !exists {
			// Event was already acked/removed, skip it
			w.mu.Lock()
			w.pendingEvts = w.pendingEvts[1:]
			w.mu.Unlock()
			continue
		}

		isLive := outboxEvt.RecordEvt != nil && outboxEvt.RecordEvt.Live

		w.mu.Lock()
		if isLive {
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

		w.outbox.sendEvent(outboxEvt)
		if isLive {
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
	defer w.mu.Unlock()

	delete(w.inFlightSentAt, evtID)

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

func (o *Outbox) AckEvent(eventID uint) {
	o.cacheMu.RLock()
	outboxEvt, exists := o.eventCache[eventID]
	o.cacheMu.RUnlock()

	if !exists {
		// evt not in cache, may have already been acked - still try to delete from DB
		if err := o.db.Delete(&models.OutboxBuffer{}, eventID).Error; err != nil {
			o.logger.Error("failed to delete acked event", "error", err, "id", eventID)
		}
		return
	}

	did := outboxEvt.DID()

	o.workersMu.Lock()
	worker := o.didWorkers[did]
	o.workersMu.Unlock()

	if worker != nil {
		worker.ackEvent(eventID)
	}

	o.cacheMu.Lock()
	delete(o.eventCache, eventID)
	o.cacheMu.Unlock()

	if err := o.db.Delete(&models.OutboxBuffer{}, eventID).Error; err != nil {
		o.logger.Error("failed to delete acked event", "error", err, "id", eventID)
	}
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
	body, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	req, err := http.NewRequest("POST", o.webhookURL, bytes.NewReader(body))
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
	o.workersMu.Lock()
	workers := make([]*DIDWorker, 0, len(o.didWorkers))
	for _, w := range o.didWorkers {
		workers = append(workers, w)
	}
	o.workersMu.Unlock()

	for _, worker := range workers {
		timedOutIDs := worker.timedOutEvents()
		for _, id := range timedOutIDs {
			o.cacheMu.RLock()
			evt, exists := o.eventCache[id]
			o.cacheMu.RUnlock()
			if exists {
				o.logger.Info("retrying timed out event", "id", id)
				o.sendEvent(evt)
			}
		}
	}
}
