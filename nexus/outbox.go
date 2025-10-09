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
	outCh  chan *Op
	logger *slog.Logger
}

func NewOutbox(db *gorm.DB) *Outbox {
	return &Outbox{
		db:     db,
		outCh:  make(chan *Op, 100),
		logger: slog.Default().With("system", "outbox"),
	}
}

func (o *Outbox) Subscribe(ctx context.Context, send func(op *Op) error) error {
	var bufferedEvts []models.OutboxBuffer
	if err := o.db.Order("id ASC").Find(&bufferedEvts).Error; err != nil {
		o.logger.Error("failed to load buffered events", "error", err)
		return err
	}

	if len(bufferedEvts) > 0 {
		o.logger.Info("draining buffered events", "count", len(bufferedEvts))
		for _, evt := range bufferedEvts {
			// Unmarshal buffered JSON back to Commit
			var op Op
			if err := json.Unmarshal([]byte(evt.Data), &op); err != nil {
				o.logger.Error("failed to unmarshal buffered op", "error", err, "id", evt.ID)
				continue
			}

			if err := send(&op); err != nil {
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
		case op := <-o.outCh:
			if err := send(op); err != nil {
				o.logger.Info("send error during live stream", "error", err)
				return err
			}
		}
	}
}

func (o *Outbox) Send(op *Op) error {
	select {
	case o.outCh <- op:
		return nil
	default:
		return o.bufferToDB(op)
	}
}

func (o *Outbox) bufferToDB(op *Op) error {
	jsonData, err := json.Marshal(op)
	if err != nil {
		return err
	}

	return o.db.Create(&models.OutboxBuffer{
		Data: string(jsonData),
	}).Error
}
