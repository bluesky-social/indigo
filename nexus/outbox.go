package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/bluesky-social/indigo/nexus/models"
	"gorm.io/gorm"
)

type Outbox struct {
	db     *gorm.DB
	outCh  chan *OutboxEvt
	logger *slog.Logger
}

func NewOutbox(db *gorm.DB) *Outbox {
	return &Outbox{
		db:     db,
		outCh:  make(chan *OutboxEvt, 100),
		logger: slog.Default().With("system", "outbox"),
	}
}

func (o *Outbox) Subscribe(ctx context.Context, send func(evt *OutboxEvt) error) error {
	var bufferedEvts []models.OutboxBuffer
	if err := o.db.Order("id ASC").Find(&bufferedEvts).Error; err != nil {
		o.logger.Error("failed to load buffered events", "error", err)
		return err
	}

	if len(bufferedEvts) > 0 {
		o.logger.Info("draining buffered events", "count", len(bufferedEvts))
		for _, evt := range bufferedEvts {
			var outboxEvt OutboxEvt
			if err := json.Unmarshal([]byte(evt.Data), &outboxEvt); err != nil {
				o.logger.Error("failed to unmarshal buffered event", "error", err, "id", evt.ID)
				continue
			}

			if err := send(&outboxEvt); err != nil {
				o.logger.Info("send error during drain", "error", err)
				return err
			}
		}

		if err := o.db.Delete(&bufferedEvts).Error; err != nil {
			o.logger.Error("failed to delete buffered events", "error", err)
		} else {
			o.logger.Info("cleared buffered events", "count", len(bufferedEvts))
		}
	}

	o.logger.Info("starting live event stream")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt := <-o.outCh:
			if err := send(evt); err != nil {
				o.logger.Info("send error during live stream", "error", err)
				return err
			}
		}
	}
}

func (o *Outbox) SendRecordEvt(evt *RecordEvt) error {
	return o.SendOutboxEvt(&OutboxEvt{
		Type:      "record",
		RecordEvt: evt,
	})
}

func (o *Outbox) SendUserEvt(evt *UserEvt) error {
	return o.SendOutboxEvt(&OutboxEvt{
		Type:    "user",
		UserEvt: evt,
	})
}

func (o *Outbox) SendOutboxEvt(evt *OutboxEvt) error {
	select {
	case o.outCh <- evt:
		return nil
	default:
		return o.bufferEvent(evt)
	}
}

func (o *Outbox) bufferEvent(evt *OutboxEvt) error {
	jsonData, err := json.Marshal(evt)
	if err != nil {
		return err
	}

	return o.db.Create(&models.OutboxBuffer{
		Data: string(jsonData),
	}).Error
}
