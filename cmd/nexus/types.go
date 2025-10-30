package main

import "github.com/bluesky-social/indigo/nexus/models"

type OutboxMode string

const (
	OutboxModeFireAndForget OutboxMode = "fire-and-forget"
	OutboxModeWebsocketAck  OutboxMode = "websocket-ack"
	OutboxModeWebhook       OutboxMode = "webhook"
)

type Commit struct {
	Did     string     `json:"did"`
	Rev     string     `json:"rev"`
	DataCid string     `json:"data_cid"`
	Ops     []CommitOp `json:"ops"`
}

type CommitOp struct {
	Collection string                 `json:"collection"`
	Rkey       string                 `json:"rkey"`
	Action     string                 `json:"action"`
	Record     map[string]interface{} `json:"record,omitempty"`
	Cid        string                 `json:"cid,omitempty"`
}

func (c *Commit) ToEvts() []*RecordEvt {
	var evts []*RecordEvt
	for _, op := range c.Ops {
		evts = append(evts, &RecordEvt{
			Live:       true,
			Did:        c.Did,
			Rev:        c.Rev,
			Collection: op.Collection,
			Rkey:       op.Rkey,
			Action:     op.Action,
			Record:     op.Record,
			Cid:        op.Cid,
		})
	}
	return evts
}

type RecordEvt struct {
	Live       bool                   `json:"live"`
	Did        string                 `json:"did"`
	Rev        string                 `json:"rev"`
	Collection string                 `json:"collection"`
	Rkey       string                 `json:"rkey"`
	Action     string                 `json:"action"`
	Record     map[string]interface{} `json:"record,omitempty"`
	Cid        string                 `json:"cid,omitempty"`
}

type UserEvt struct {
	Did      string               `json:"did"`
	Handle   string               `json:"handle"`
	IsActive bool                 `json:"is_active"`
	Status   models.AccountStatus `json:"status"`
}

type OutboxEvt struct {
	ID        uint       `json:"id"`
	Type      string     `json:"type"`
	RecordEvt *RecordEvt `json:"record,omitempty"`
	UserEvt   *UserEvt   `json:"user,omitempty"`
}

func (evt *OutboxEvt) DID() string {
	if evt.RecordEvt != nil {
		return evt.RecordEvt.Did
	}
	if evt.UserEvt != nil {
		return evt.UserEvt.Did
	}
	return ""
}

type AckMessage struct {
	ID uint `json:"id"`
}
