package main

import "gorm.io/gorm"

type Outbox struct {
	db    *gorm.DB
	outCh chan *Op
}

func NewOutbox(db *gorm.DB) *Outbox {
	return &Outbox{
		db:    db,
		outCh: make(chan *Op),
	}
}

func (o *Outbox) Send(op *Op) error {
	o.outCh <- op
	return nil
}

// func (nexus *Nexus) handleOp(op *Op) error {
// 	select {
// 	case nexus.evtCh <- op:
// 	default:
// 		err := nexus.db.Create(&BufferedEvt{Did: op.DID, Evt: *op}).Error
// 		if err != nil {
// 			return err
// 		}
// 	}
// 	return nil
// }
