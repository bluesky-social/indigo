package main

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/bluesky-social/indigo/nexus/models"
	"gorm.io/gorm"
)

var ErrAlreadyConnected = errors.New("websocket already connected")

type Outbox struct {
	db          *gorm.DB
	outCh       chan *Op
	mu          sync.RWMutex
	connected   bool
	drainingBuf bool
}

func NewOutbox(db *gorm.DB) *Outbox {
	return &Outbox{
		db:          db,
		outCh:       make(chan *Op, 100),
		connected:   false,
		drainingBuf: false,
	}
}

func (o *Outbox) Connect() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.connected {
		return ErrAlreadyConnected
	}

	o.connected = true
	o.drainingBuf = true
	return nil
}

func (o *Outbox) StartLive() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.drainingBuf = false
}

func (o *Outbox) Disconnect() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.connected = false
	o.drainingBuf = false
}

func (o *Outbox) IsConnected() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.connected
}

func (o *Outbox) Send(op *Op) error {
	o.mu.RLock()
	connected := o.connected
	drainingBuf := o.drainingBuf
	o.mu.RUnlock()

	// If connected but still draining buffered events, buffer to DB
	if drainingBuf {
		return o.bufferToDB(op)
	}

	if connected {
		select {
		case o.outCh <- op:
			return nil
		default:
			// Channel full, buffer to DB
			return o.bufferToDB(op)
		}
	} else {
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
