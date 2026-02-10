package main

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/cmd/tap/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFireAndForget_BasicDelivery(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	// Small delay to ensure WS connection is fully established
	time.Sleep(50 * time.Millisecond)

	n := 5
	te.pushRecordEvents("did:plc:testuser1", n, false)

	msgs := consumer.waitForMessages(n, 2*time.Second)
	assert.Len(t, msgs, n, "expected %d messages, got %d", n, len(msgs))

	for _, msg := range msgs {
		assert.Equal(t, "record", msg.Type)
		assert.NotNil(t, msg.RecordEvt)
		assert.Equal(t, "did:plc:testuser1", msg.RecordEvt.Did)
	}
}

func TestFireAndForget_MultiDID(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	time.Sleep(50 * time.Millisecond)

	didA := "did:plc:alice"
	didB := "did:plc:bob"

	te.pushRecordEvents(didA, 3, false)
	te.pushRecordEvents(didB, 3, false)

	msgs := consumer.waitForMessages(6, 2*time.Second)
	assert.Len(t, msgs, 6, "expected 6 messages, got %d", len(msgs))

	// Count messages per DID
	countA, countB := 0, 0
	for _, msg := range msgs {
		if msg.RecordEvt.Did == didA {
			countA++
		} else if msg.RecordEvt.Did == didB {
			countB++
		}
	}
	assert.Equal(t, 3, countA, "expected 3 events for DID-A")
	assert.Equal(t, 3, countB, "expected 3 events for DID-B")
}

func TestWebsocketAck_BasicDelivery(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebsocketAck,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	time.Sleep(50 * time.Millisecond)

	n := 3
	te.pushRecordEvents("did:plc:acktest", n, false)

	msgs := consumer.waitForMessages(n, 2*time.Second)
	require.Len(t, msgs, n, "expected %d messages, got %d", n, len(msgs))

	// Ack all messages — verify acks are accepted without error
	for _, msg := range msgs {
		err := consumer.sendAck(msg.ID)
		require.NoError(t, err)
	}

	// Wait for acks to be processed and events to be deleted from cache.
	// The batched delete flushes every 10s, so we need a longer timeout.
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		allGone := true
		for _, msg := range msgs {
			if _, exists := te.events.GetEvent(msg.ID); exists {
				allGone = false
				break
			}
		}
		if allGone {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("events were not removed from cache after ack")
}

func TestWebsocketAck_OrderingHistorical(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebsocketAck,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	time.Sleep(50 * time.Millisecond)

	// Push 3 historical events for same DID — all should arrive
	did := "did:plc:historical"
	te.pushRecordEvents(did, 3, false)

	msgs := consumer.waitForMessages(3, 2*time.Second)
	assert.Len(t, msgs, 3, "expected all 3 historical events to arrive")

	for _, msg := range msgs {
		assert.Equal(t, did, msg.RecordEvt.Did)
	}
}

func TestWebsocketAck_OrderingLiveBarrier(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebsocketAck,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	time.Sleep(50 * time.Millisecond)

	did := "did:plc:ordering"

	// Push H1, H2 (historical)
	hIDs := te.pushRecordEvents(did, 2, false)

	// Wait for H1, H2 to arrive
	msgs := consumer.waitForMessages(2, 2*time.Second)
	require.Len(t, msgs, 2, "expected 2 historical events")

	// Push L1 (live) — should be blocked until H1 and H2 are acked
	lIDs := te.pushRecordEvents(did, 1, true)

	// Push H3, H4 (historical) — should be blocked until L1 is acked
	h2IDs := te.pushRecordEvents(did, 2, false)

	// Verify L1 has NOT arrived yet (it's blocked on H1, H2 acks)
	time.Sleep(100 * time.Millisecond)
	earlyMsgs := consumer.waitForMessages(3, 100*time.Millisecond)
	assert.Len(t, earlyMsgs, 2, "L1 should not arrive before H1/H2 are acked")

	// Ack H1 and H2
	for _, id := range hIDs {
		require.NoError(t, consumer.sendAck(id))
	}

	// L1 should now arrive
	msgs = consumer.waitForMessages(3, 2*time.Second)
	require.Len(t, msgs, 3, "expected L1 to arrive after H1/H2 acked")

	// Verify L1 is the 3rd message
	assert.Equal(t, lIDs[0], msgs[2].ID, "3rd message should be L1")

	// Verify H3, H4 have NOT arrived yet (blocked on L1 ack)
	time.Sleep(100 * time.Millisecond)
	msgs = consumer.waitForMessages(4, 100*time.Millisecond)
	assert.Len(t, msgs, 3, "H3/H4 should not arrive before L1 is acked")

	// Ack L1
	require.NoError(t, consumer.sendAck(lIDs[0]))

	// H3 and H4 should now arrive
	msgs = consumer.waitForMessages(5, 2*time.Second)
	require.Len(t, msgs, 5, "expected H3/H4 to arrive after L1 acked")

	// Verify H3 and H4 are in the final messages
	finalIDs := map[uint]bool{msgs[3].ID: true, msgs[4].ID: true}
	assert.True(t, finalIDs[h2IDs[0]], "H3 should be in final batch")
	assert.True(t, finalIDs[h2IDs[1]], "H4 should be in final batch")
}

func TestWebsocketAck_NoStallOnPreconnectEvents(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	// Push events BEFORE any WebSocket consumer connects
	n := 5
	te.pushRecordEvents("did:plc:preconnect", n, false)

	// Give the outbox time to try to deliver (they'll queue in the outgoing channel)
	time.Sleep(100 * time.Millisecond)

	// Now connect a consumer
	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	// Events should still arrive (they were buffered in the outgoing channel)
	msgs := consumer.waitForMessages(n, 2*time.Second)
	assert.Len(t, msgs, n, "pre-connect events should still be delivered, got %d", len(msgs))
}

func TestWebhook_BasicDelivery(t *testing.T) {
	receiver := newTestWebhookReceiver()
	defer receiver.close()

	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebhook,
		webhookURL: receiver.server.URL,
	})

	n := 3
	te.pushRecordEvents("did:plc:webhooktest", n, false)

	msgs := receiver.waitForMessages(n, 2*time.Second)
	assert.Len(t, msgs, n, "webhook receiver should get %d events, got %d", n, len(msgs))
}

func TestIdentityEvent_Delivery(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	time.Sleep(50 * time.Millisecond)

	te.pushIdentityEvent("did:plc:identity", "alice.bsky.social", models.AccountStatusActive)

	msgs := consumer.waitForMessages(1, 2*time.Second)
	require.Len(t, msgs, 1, "expected 1 identity event")

	msg := msgs[0]
	assert.Equal(t, "identity", msg.Type)
	assert.NotNil(t, msg.IdentityEvt)
	assert.Equal(t, "did:plc:identity", msg.IdentityEvt.Did)
	assert.Equal(t, "alice.bsky.social", msg.IdentityEvt.Handle)
	assert.True(t, msg.IdentityEvt.IsActive)
	assert.Equal(t, models.AccountStatusActive, msg.IdentityEvt.Status)
}
