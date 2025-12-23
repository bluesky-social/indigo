package tap

import (
	"encoding/json"
	"fmt"
)

const (
	eventTypeACK      = "ack"
	eventTypeRecord   = "record"
	eventTypeIdentity = "identity"
)

// Event represents an atproto event from tap. Use a type switch on the Payload() method to access event data.
type Event struct {
	ID   uint64
	Type string

	record   *RecordEvent
	identity *IdentityEvent
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

func (e *Event) UnmarshalJSON(data []byte) error {
	event := struct {
		ID       uint64          `json:"id"`
		Type     string          `json:"type"`
		Record   json.RawMessage `json:"record,omitempty"`
		Identity json.RawMessage `json:"identity,omitempty"`
	}{}

	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("failed to unmarshal tap event: %w", err)
	}

	e.ID = event.ID
	e.Type = event.Type

	switch event.Type {
	case eventTypeRecord:
		e.record = &RecordEvent{}
		if err := json.Unmarshal(event.Record, e.record); err != nil {
			return fmt.Errorf("failed to unmarshal tap record event: %w", err)
		}
	case eventTypeIdentity:
		e.identity = &IdentityEvent{}
		if err := json.Unmarshal(event.Identity, e.identity); err != nil {
			return fmt.Errorf("failed to unmarshal tap identity event: %w", err)
		}
	default:
		return fmt.Errorf("unknown event type %q", event.Type)
	}

	return nil
}

func (e Event) MarshalJSON() ([]byte, error) {
	event := struct {
		ID       uint64         `json:"id"`
		Type     string         `json:"type"`
		Record   *RecordEvent   `json:"record,omitempty"`
		Identity *IdentityEvent `json:"identity,omitempty"`
	}{
		ID:       e.ID,
		Type:     e.Type,
		Record:   e.record,
		Identity: e.identity,
	}

	buf, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tap event: %w", err)
	}

	return buf, nil
}

// Payload returns the typed event data as either *RecordEvent or *IdentityEvent.
func (e *Event) Payload() any {
	switch e.Type {
	case eventTypeRecord:
		return e.record
	case eventTypeIdentity:
		return e.identity
	}

	return nil // unreachable
}

// Constructs a new ACK object to be serialized and sent back to tap
func NewACKPayload(id uint64) *Event {
	return &Event{
		Type: eventTypeACK,
		ID:   id,
	}
}
