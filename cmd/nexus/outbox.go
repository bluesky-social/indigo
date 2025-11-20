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

	"github.com/bluesky-social/indigo/cmd/nexus/models"
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

	cache *EventCache

	didWorkers sync.Map // map[string]*DIDWorker

	ackQueue chan uint
	events   chan *OutboxMessage

	ctx context.Context
}

func NewOutbox(db *gorm.DB, mode OutboxMode, webhookURL string, parallelism int) *Outbox {
	logger := slog.Default().With("system", "outbox")
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
		cache:    NewEventCache(db, logger, parallelism*5, 1000),
		ackQueue: make(chan uint, parallelism*5000),
	}
}

// Run starts the outbox workers for event delivery and cleanup.
func (o *Outbox) Run(ctx context.Context) {
	o.ctx = ctx

	if o.mode == OutboxModeWebsocketAck {
		go o.checkTimeouts(ctx)
	}
	go o.cache.run(ctx)

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
		case eventID := <-o.cache.pendingIDs:
			o.deliverEvent(eventID)
		}
	}
}

func (o *Outbox) deliverEvent(eventID uint) {
	outboxEvt, exists := o.cache.GetEvent(eventID)
	if !exists {
		// Event was already acked/removed
		return
	}

	did := outboxEvt.DID()

	if val, ok := o.didWorkers.Load(did); ok {
		worker := val.(*DIDWorker)
		worker.addEvent(outboxEvt)
		return
	}

	worker := &DIDWorker{
		did:            did,
		notifChan:      make(chan struct{}, 1),
		inFlightSentAt: make(map[uint]time.Time),
		outbox:         o,
		ctx:            o.ctx,
	}
	actual, _ := o.didWorkers.LoadOrStore(did, worker)
	actual.(*DIDWorker).addEvent(outboxEvt)
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
	outboxEvt, exists := o.cache.GetEvent(eventID)

	if exists {
		did := outboxEvt.DID()

		if val, ok := o.didWorkers.Load(did); ok {
			worker := val.(*DIDWorker)
			worker.ackEvent(eventID)
		}

		o.cache.DeleteEvent(eventID)
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
				o.flushDeleteBatch(batch)
				batch = nil
			}
		case <-ticker.C:
			if len(batch) > 0 {
				o.flushDeleteBatch(batch)
				batch = nil
			}
		}
	}
}

func (o *Outbox) flushDeleteBatch(ids []uint) {
	if len(ids) == 0 {
		return
	}

	if err := o.db.Delete(&models.OutboxBuffer{}, ids).Error; err != nil {
		o.logger.Error("failed to delete batch of acked events", "error", err, "count", len(ids))
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
			evt, exists := o.cache.GetEvent(id)
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

		outboxEvt, exists := w.outbox.cache.GetEvent(eventID)

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

type EventCache struct {
	db     *gorm.DB
	logger *slog.Logger

	batchSize  int
	numLoaders int

	eventCache map[uint]*OutboxEvt
	cacheMu    sync.RWMutex

	pendingIDs chan uint
}

func NewEventCache(db *gorm.DB, logger *slog.Logger, numLoaders int, batchSize int) *EventCache {
	pendingSize := batchSize * numLoaders * 2
	return &EventCache{
		db:         db,
		logger:     logger,
		batchSize:  batchSize,
		numLoaders: numLoaders,
		eventCache: make(map[uint]*OutboxEvt),
		pendingIDs: make(chan uint, pendingSize),
	}
}

func (ec *EventCache) run(ctx context.Context) {
	lastID := 0
	loadParallel := false
	var lastPageID int
	var resultSize int
	var err error
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// if we're in a densely populated section of the table, we can take advantage of that and load chunks of IDs in parallel
			// if it's sparsely populated, then we just need to load one page at a time
			// switch between the two by comparing the result size to the expected batch size
			if loadParallel {
				lastPageID, resultSize, err = ec.loadEventsParallel(lastID)
			} else {
				lastPageID, resultSize, err = ec.loadEventsSerial(lastID)
			}
			if err != nil {
				ec.logger.Error("failed to load events into cache", "error", err, "lastID", lastID)
			} else {
				lastID = lastPageID
				loadParallel = resultSize > (ec.batchSize)/2
			}

			if resultSize == 0 {
				ec.logger.Info("no events back, sleeping for a second")
				time.Sleep(time.Second)
			}
		}
	}
}

func (ec *EventCache) loadEventsSerial(startID int) (int, int, error) {
	dbEvts := make([]models.OutboxBuffer, 0, ec.batchSize)
	if err := ec.db.Raw("SELECT * FROM outbox_buffers WHERE id > ? ORDER BY id ASC LIMIT ?", startID, ec.batchSize).Scan(&dbEvts).Error; err != nil {
		return 0, 0, err
	}

	outboxEvts, err := parseOutboxEvents(dbEvts)
	if err != nil {
		return 0, 0, err
	}
	resultSize := len(outboxEvts)
	if resultSize == 0 {
		return startID, 0, nil
	}

	// Populate cache and queue for delivery
	ec.cacheMu.Lock()
	for i := range outboxEvts {
		evt := outboxEvts[i] // Create heap copy
		ec.eventCache[evt.ID] = &evt
	}
	ec.cacheMu.Unlock()

	for i := range outboxEvts {
		ec.pendingIDs <- outboxEvts[i].ID
	}

	return int(outboxEvts[resultSize-1].ID), resultSize, nil
}

func (ec *EventCache) loadEventsParallel(startID int) (int, int, error) {
	results := make([]batchResult, ec.numLoaders)
	var wg sync.WaitGroup
	wg.Add(ec.numLoaders)
	for i := 0; i < ec.numLoaders; i++ {
		loaderID := i
		go func() {
			defer wg.Done()
			batchStartID := startID + loaderID*ec.batchSize
			results[loaderID] = ec.loadEventBatch(batchStartID, batchStartID+ec.batchSize)
		}()
	}
	wg.Wait()

	for _, result := range results {
		if result.err != nil {
			return 0, 0, result.err
		}
	}

	resultsSize := batchesSize(results)
	// didn't load any events
	if resultsSize == 0 {
		time.Sleep(500 * time.Millisecond)
		return startID, 0, nil
	}

	ec.cacheMu.Lock()
	for _, result := range results {
		for _, evt := range result.events {
			evtCopy := evt // Create heap copy to avoid storing address of loop variable
			ec.eventCache[evtCopy.ID] = &evtCopy
		}
	}
	ec.cacheMu.Unlock()

	for _, result := range results {
		for _, evt := range result.events {
			ec.pendingIDs <- evt.ID
		}
	}
	return lastIDForBatches(results), resultsSize, nil
}

func (ec *EventCache) loadEventBatch(startID int, endID int) batchResult {
	dbEvts := make([]models.OutboxBuffer, 0, (endID-startID)+1)
	err := ec.db.Raw("SELECT * FROM outbox_buffers WHERE id > ? AND id <= ? ORDER BY id ASC", startID, endID).Scan(&dbEvts).Error
	if err != nil {
		return batchResult{err: err}
	}
	outboxEvts, err := parseOutboxEvents(dbEvts)
	return batchResult{events: outboxEvts, err: err}
}

func parseOutboxEvents(dbEvts []models.OutboxBuffer) ([]OutboxEvt, error) {
	outboxEvts := make([]OutboxEvt, 0, len(dbEvts))

	for _, evt := range dbEvts {
		var outboxEvt OutboxEvt
		if err := json.Unmarshal([]byte(evt.Data), &outboxEvt); err != nil {
			return nil, err
		}
		outboxEvt.ID = evt.ID
		outboxEvts = append(outboxEvts, outboxEvt)
	}

	return outboxEvts, nil
}

type batchResult struct {
	events []OutboxEvt
	err    error
}

func batchesSize(batches []batchResult) int {
	size := 0
	for _, batch := range batches {
		size += len(batch.events)
	}
	return size
}

func lastIDForBatches(batches []batchResult) int {
	// Iterate backwards to find the last non-empty batch
	for i := len(batches) - 1; i >= 0; i-- {
		if len(batches[i].events) > 0 {
			lastEvent := batches[i].events[len(batches[i].events)-1]
			return int(lastEvent.ID)
		}
	}
	return 0
}

func (ec *EventCache) GetEvent(id uint) (*OutboxEvt, bool) {
	ec.cacheMu.RLock()
	defer ec.cacheMu.RUnlock()
	evt, exists := ec.eventCache[id]
	return evt, exists
}

func (ec *EventCache) DeleteEvent(id uint) {
	ec.cacheMu.Lock()
	defer ec.cacheMu.Unlock()
	delete(ec.eventCache, id)
}
