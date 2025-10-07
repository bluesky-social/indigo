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

func (o *Outbox) Subscribe(ctx context.Context, send func(*Op) error) error {
	var bufferedEvts []models.BufferedEvt
	if err := o.db.Order("id ASC").Find(&bufferedEvts).Error; err != nil {
		o.logger.Error("failed to load buffered events", "error", err)
		return err
	}

	if len(bufferedEvts) > 0 {
		o.logger.Info("draining buffered events", "count", len(bufferedEvts))
		for _, evt := range bufferedEvts {
			op := &Op{
				Did:        evt.Did,
				Collection: evt.Collection,
				Rkey:       evt.Rkey,
				Action:     evt.Action,
				Cid:        evt.Cid,
			}

			if evt.Record != "" {
				var record map[string]interface{}
				if err := json.Unmarshal([]byte(evt.Record), &record); err != nil {
					o.logger.Error("failed to unmarshal record", "error", err, "id", evt.ID)
					continue
				}
				op.Record = record
			}

			if err := send(op); err != nil {
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
	var recordJSON string
	if op.Record != nil {
		bytes, err := json.Marshal(op.Record)
		if err != nil {
			return err
		}
		recordJSON = string(bytes)
	}

	err := o.db.Create(&models.BufferedEvt{
		Did:        op.Did,
		Collection: op.Collection,
		Rkey:       op.Rkey,
		Action:     op.Action,
		Cid:        op.Cid,
		Record:     recordJSON,
	}).Error

	return err
}
