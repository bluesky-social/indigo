package main

import "encoding/json"

const (
	eventTypeACK      = "ack"
	eventTypeRecord   = "record"
	eventTypeIdentity = "identity"
)

// Event represents an atproto event from tap. Use a type switch on the Payload() method to access event data.
type Event struct {
	ID   uint64 `json:"id"`
	Type string `json:"type"`

	Record   *RecordEvent   `json:"record"`
	Identity *IdentityEvent `json:"identity"`
}

// RecordEvent represents a record creation, update, or deletion in a repository
type RecordEvent struct {
	DID        string          `json:"did"`
	Collection string          `json:"collection"`
	Rkey       string          `json:"rkey"`
	Action     string          `json:"action"`
	CID        string          `json:"cid"`
	Record     json.RawMessage `json:"record"`
	Live       bool            `json:"live"`
}

// IdentityEvent represents an account status change
type IdentityEvent struct {
	DID      string `json:"did"`
	Handle   string `json:"handle"`
	IsActive bool   `json:"isActive"`
	Status   string `json:"status"`
}
