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

type InFlightEvent struct {
	ID     uint
	DID    string
	Live   bool
	Event  *OutboxEvt
	SentAt time.Time
}

type DIDInFlight struct {
	LiveCount       int
	HistoricalCount int
}

type Outbox struct {
	db             *gorm.DB
	logger         *slog.Logger
	notify         chan struct{}
	events         chan *OutboxEvt
	mode           OutboxMode
	webhookURL     string
	httpClient     *http.Client
	inFlightEvents map[uint]*InFlightEvent
	inFlightDIDs   map[string]*DIDInFlight
	inFlightMu     sync.RWMutex
}

func NewOutbox(db *gorm.DB, mode OutboxMode, webhookURL string) *Outbox {
	return &Outbox{
		db:         db,
		logger:     slog.Default().With("system", "outbox"),
		notify:     make(chan struct{}),
		events:     make(chan *OutboxEvt, 1000),
		mode:       mode,
		webhookURL: webhookURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		inFlightEvents: make(map[uint]*InFlightEvent),
		inFlightDIDs:   make(map[string]*DIDInFlight),
	}
}

func (o *Outbox) Run(ctx context.Context) {
	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	// Start timeout checker for websocket-ack mode
	// Webhook mode doesn't need this since sendWebhook() retries indefinitely
	if o.mode == OutboxModeWebsocketAck {
		go o.checkTimeouts(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-o.notify:
			o.deliverPending()
		case <-ticker.C:
			o.deliverPending()
		}
	}
}

// notify outbox of new event in buffer
// for active outboxes, channel will basically always be firing
func (o *Outbox) Notify() {
	select {
	case o.notify <- struct{}{}:
	default:
	}
}

func (o *Outbox) deliverPending() {
	var events []models.OutboxBuffer
	if err := o.db.Order("id ASC").Limit(100).Find(&events).Error; err != nil {
		o.logger.Error("failed to query outbox buffer", "error", err)
		return
	}

	if len(events) == 0 {
		return
	}

	for _, evt := range events {
		var outboxEvt OutboxEvt
		if err := json.Unmarshal([]byte(evt.Data), &outboxEvt); err != nil {
			o.logger.Error("failed to unmarshal outbox event", "error", err, "id", evt.ID)
			continue
		}

		outboxEvt.ID = evt.ID
		did := outboxEvt.DID()

		// check if events for the same DID are in flight and we should skip the event for now
		// it will remain in the DB & be tried again later
		o.inFlightMu.RLock()
		counts, didExists := o.inFlightDIDs[did]
		_, evtExists := o.inFlightEvents[evt.ID]
		o.inFlightMu.RUnlock()

		if evtExists {
			continue
		} else if didExists {
			// Live events: wait for ALL in-flight events to clear (historical and live)
			if evt.Live && (counts.LiveCount > 0 || counts.HistoricalCount > 0) {
				continue
			}
			// Historical events: only wait for in-flight live events
			if !evt.Live && counts.LiveCount > 0 {
				continue
			}
		}

		o.trackInFlight(&evt, &outboxEvt, did, evt.Live)

		// Deliver based on consumer mode
		switch o.mode {
		case OutboxModeFireAndForget:
			o.events <- &outboxEvt
		case OutboxModeWebsocketAck:
			o.events <- &outboxEvt
		case OutboxModeWebhook:
			go o.sendWebhook(evt.ID, &outboxEvt)
		}
	}
}

func (o *Outbox) trackInFlight(dbEvt *models.OutboxBuffer, outboxEvt *OutboxEvt, did string, isLive bool) {
	o.inFlightMu.Lock()
	o.inFlightEvents[dbEvt.ID] = &InFlightEvent{
		ID:     dbEvt.ID,
		DID:    did,
		Live:   isLive,
		Event:  outboxEvt,
		SentAt: time.Now(),
	}

	// Increment the appropriate counter
	counts, exists := o.inFlightDIDs[did]
	if !exists {
		counts = &DIDInFlight{}
		o.inFlightDIDs[did] = counts
	}
	if isLive {
		counts.LiveCount++
	} else {
		counts.HistoricalCount++
	}
	o.inFlightMu.Unlock()
}

func (o *Outbox) AckEvent(eventID uint) {
	o.inFlightMu.Lock()
	defer o.inFlightMu.Unlock()

	// Check if this was a tracked event (websocket-ack or webhook mode)
	inFlight, exists := o.inFlightEvents[eventID]
	if exists {
		// Decrement the appropriate counter
		did := inFlight.DID
		counts := o.inFlightDIDs[did]
		if inFlight.Live {
			counts.LiveCount--
		} else {
			counts.HistoricalCount--
		}

		// If no more events for this DID, remove the key
		if counts.LiveCount == 0 && counts.HistoricalCount == 0 {
			delete(o.inFlightDIDs, did)
		}

		delete(o.inFlightEvents, eventID)
		o.logger.Info("event acked", "id", eventID, "did", did, "live", inFlight.Live)
	}

	// Delete from DB (for both tracked and untracked events)
	if err := o.db.Delete(&models.OutboxBuffer{}, eventID).Error; err != nil {
		o.logger.Error("failed to delete acked event", "error", err, "id", eventID)
	}
}

func (o *Outbox) sendWebhook(eventID uint, evt *OutboxEvt) {
	retries := 0
	for {
		if err := o.postWebhook(evt); err != nil {
			o.logger.Warn("webhook failed, retrying", "error", err, "id", eventID, "retries", retries)
			time.Sleep(backoff(retries, 10))
			retries++
			continue
		}

		// Success - ack the event
		o.AckEvent(eventID)
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
	ticker := time.NewTicker(1 * time.Second)
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

// this just clears them out of tracking which will allow them to be retried on the next deliverPend
func (o *Outbox) retryTimedOutEvents() {
	o.inFlightMu.Lock()
	defer o.inFlightMu.Unlock()

	now := time.Now()
	for id, inFlight := range o.inFlightEvents {
		if now.Sub(inFlight.SentAt) > 10*time.Second {
			o.logger.Info("event timed out, resending", "id", id, "did", inFlight.DID, "live", inFlight.Live)

			// Decrement the appropriate counter
			did := inFlight.DID
			counts := o.inFlightDIDs[did]
			if inFlight.Live {
				counts.LiveCount--
			} else {
				counts.HistoricalCount--
			}

			// If no more events for this DID, remove the key
			if counts.LiveCount == 0 && counts.HistoricalCount == 0 {
				delete(o.inFlightDIDs, did)
			}

			delete(o.inFlightEvents, id)
		}
	}
}
