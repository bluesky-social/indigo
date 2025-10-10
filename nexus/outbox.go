package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/nexus/models"
	"gorm.io/gorm"
)

type Outbox struct {
	db     *gorm.DB
	logger *slog.Logger
	notify chan struct{}
	events chan *OutboxEvt
}

func NewOutbox(db *gorm.DB) *Outbox {
	return &Outbox{
		db:     db,
		logger: slog.Default().With("system", "outbox"),
		notify: make(chan struct{}),
		events: make(chan *OutboxEvt, 1000),
	}
}

func (o *Outbox) Run(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

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

		outboxEvt.AckCh = make(chan struct{})

		o.events <- &outboxEvt

		<-outboxEvt.AckCh

		if err := o.db.Delete(&evt).Error; err != nil {
			o.logger.Error("failed to delete outbox event", "error", err, "id", evt.ID)
		}
	}
}

func (o *Outbox) Notify() {
	select {
	case o.notify <- struct{}{}:
	default:
	}
}

func (o *Outbox) Subscribe(ctx context.Context) <-chan *OutboxEvt {
	return o.events
}
