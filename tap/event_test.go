package tap

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEventJSON(t *testing.T) {
	t.Parallel()

	t.Run("marshal/unmarshal record", func(t *testing.T) {
		t.Parallel()
		require := require.New(t)

		original := Event{
			ID:   123,
			Type: eventTypeRecord,
			record: &RecordEvent{
				DID:        "did:plc:test",
				Collection: "app.bsky.feed.post",
				Rkey:       "abc123",
				Action:     "create",
				CID:        "bafytest",
				Record:     json.RawMessage(`{"text":"hello"}`),
				Live:       true,
			},
		}

		buf, err := json.Marshal(original)
		require.NoError(err)

		var decoded Event
		require.NoError(json.Unmarshal(buf, &decoded))
		require.Equal(original.ID, decoded.ID)
		require.Equal(original.Type, decoded.Type)

		payload, ok := decoded.Payload().(*RecordEvent)
		require.True(ok)
		require.Equal(original.record.DID, payload.DID)
		require.Equal(original.record.Collection, payload.Collection)
		require.Equal(original.record.Rkey, payload.Rkey)
		require.Equal(original.record.Action, payload.Action)
		require.Equal(original.record.CID, payload.CID)
		require.Equal(original.record.Live, payload.Live)
		require.JSONEq(string(original.record.Record), string(payload.Record))
	})

	t.Run("marshal/unmarshal user", func(t *testing.T) {
		t.Parallel()
		require := require.New(t)

		original := Event{
			ID:   456,
			Type: eventTypeUser,
			user: &UserEvent{
				DID:      "did:plc:user",
				Handle:   "test.bsky.social",
				IsActive: true,
				Status:   "active",
			},
		}

		buf, err := json.Marshal(original)
		require.NoError(err)

		var decoded Event
		require.NoError(json.Unmarshal(buf, &decoded))
		require.Equal(original.ID, decoded.ID)
		require.Equal(original.Type, decoded.Type)

		payload, ok := decoded.Payload().(*UserEvent)
		require.True(ok)
		require.Equal(original.user.DID, payload.DID)
		require.Equal(original.user.Handle, payload.Handle)
		require.Equal(original.user.IsActive, payload.IsActive)
		require.Equal(original.user.Status, payload.Status)
	})

	t.Run("unmarshal from raw json", func(t *testing.T) {
		t.Parallel()
		require := require.New(t)

		recordJSON := `{"id":1,"type":"record","record":{"did":"did:plc:abc","collection":"app.bsky.feed.like","rkey":"xyz","action":"create","cid":"mycid","record":{"subject":"at://did:plc:foo/app.bsky.feed.post/xyz"},"live":false}}`
		var recordEvent Event
		require.NoError(json.Unmarshal([]byte(recordJSON), &recordEvent))
		require.Equal(uint64(1), recordEvent.ID)
		require.Equal(eventTypeRecord, recordEvent.Type)

		userJSON := `{"id":2,"type":"user","user":{"did":"did:plc:def","handle":"foo.test","isActive":true,"status":"active"}}`
		var userEvent Event
		require.NoError(json.Unmarshal([]byte(userJSON), &userEvent))
		require.Equal(uint64(2), userEvent.ID)
		require.Equal(eventTypeUser, userEvent.Type)
	})

	t.Run("unmarshal unknown type", func(t *testing.T) {
		t.Parallel()
		require := require.New(t)

		badJSON := `{"id":1,"type":"unknown"}`
		var ev Event
		err := json.Unmarshal([]byte(badJSON), &ev)
		require.Error(err)
		require.Contains(err.Error(), "unknown event type")
	})
}
