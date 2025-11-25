package main

import (
	"encoding/json"

	"github.com/bluesky-social/indigo/cmd/nexus/models"
)

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

func (e *RecordEvt) MarshalJSON(id uint) ([]byte, error) {
	return json.Marshal(MarshallableEvt{
		ID:        id,
		Type:      "record",
		RecordEvt: e,
	})
}

type UserEvt struct {
	Did      string               `json:"did"`
	Handle   string               `json:"handle"`
	IsActive bool                 `json:"is_active"`
	Status   models.AccountStatus `json:"status"`
}

func (e *UserEvt) MarshalJSON(id uint) ([]byte, error) {
	return json.Marshal(MarshallableEvt{
		ID:      id,
		Type:    "user",
		UserEvt: e,
	})
}

type MarshallableEvt struct {
	ID        uint       `json:"id"`
	Type      string     `json:"type"`
	RecordEvt *RecordEvt `json:"record,omitempty"`
	UserEvt   *UserEvt   `json:"user,omitempty"`
}

type OutboxEvt struct {
	ID    uint
	Did   string
	Live  bool
	Event []byte
}

type WsReponseType string

const (
	WsResponseAck WsReponseType = "ack"
)

type WsResponse struct {
	Type WsReponseType `json:"type"`
	ID   uint          `json:"id"`
}
