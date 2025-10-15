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
	Event  *OutboxEvt
	SentAt time.Time
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
	inFlightDIDs   map[string]uint
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
		inFlightDIDs:   make(map[string]uint),
	}
}

func (o *Outbox) Run(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
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

		// Set the ID from the database record
		outboxEvt.ID = evt.ID
		did := outboxEvt.DID()

		// Check if this DID already has an in-flight event
		o.inFlightMu.RLock()
		_, hasInFlight := o.inFlightDIDs[did]
		o.inFlightMu.RUnlock()

		if hasInFlight {
			// Skip this event for now, will be retried later
			continue
		}

		o.trackInFlight(&evt, &outboxEvt, did)

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

func (o *Outbox) trackInFlight(dbEvt *models.OutboxBuffer, outboxEvt *OutboxEvt, did string) {
	o.inFlightMu.Lock()
	o.inFlightEvents[dbEvt.ID] = &InFlightEvent{
		ID:     dbEvt.ID,
		DID:    did,
		Event:  outboxEvt,
		SentAt: time.Now(),
	}
	o.inFlightDIDs[did] = dbEvt.ID
	o.inFlightMu.Unlock()
}

func (o *Outbox) AckEvent(eventID uint) {
	o.inFlightMu.Lock()
	defer o.inFlightMu.Unlock()

	// Check if this was a tracked event (websocket-ack or webhook mode)
	inFlight, exists := o.inFlightEvents[eventID]
	if exists {
		// Remove from in-flight tracking
		delete(o.inFlightDIDs, inFlight.DID)
		delete(o.inFlightEvents, eventID)
		o.logger.Info("event acked", "id", eventID, "did", inFlight.DID)
	}

	// Delete from DB (for both tracked and untracked events)
	if err := o.db.Delete(&models.OutboxBuffer{}, eventID).Error; err != nil {
		o.logger.Error("failed to delete acked event", "error", err, "id", eventID)
	}
}

func (o *Outbox) sendWebhook(eventID uint, evt *OutboxEvt) {
	backoff := 0
	for {
		if err := o.postWebhook(evt); err != nil {
			o.logger.Warn("webhook failed, retrying", "error", err, "id", eventID, "backoff", backoff)
			time.Sleep(webhookBackoff(backoff))
			backoff++
			continue
		}

		// Success - ack the event
		o.AckEvent(eventID)
		return
	}
}

func webhookBackoff(attempt int) time.Duration {
	if attempt == 0 {
		return time.Second
	}
	// Exponential: 1s, 2s, 4s, 8s, cap at 10s
	duration := time.Second * (1 << attempt)
	if duration > 10*time.Second {
		duration = 10 * time.Second
	}
	return duration
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

func (o *Outbox) retryTimedOutEvents() {
	o.inFlightMu.Lock()
	defer o.inFlightMu.Unlock()

	now := time.Now()
	for id, inFlight := range o.inFlightEvents {
		if now.Sub(inFlight.SentAt) > 10*time.Second {
			o.logger.Info("event timed out, resending", "id", id, "did", inFlight.DID)

			// Remove from in-flight tracking so it can be retried
			delete(o.inFlightDIDs, inFlight.DID)
			delete(o.inFlightEvents, id)
		}
	}
}
