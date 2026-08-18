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

	time.Sleep(20 * time.Millisecond)

	n := 5
	te.pushRecordEvents("did:example:user1", n, false)

	msgs := consumer.waitForMessages(n, 100*time.Millisecond)
	require.Len(t, msgs, n)

	for _, msg := range msgs {
		assert.Equal(t, "record", msg.Type)
		assert.NotNil(t, msg.RecordEvt)
		assert.Equal(t, "did:example:user1", msg.RecordEvt.Did)
	}
}

func TestFireAndForget_MultiDID(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	time.Sleep(20 * time.Millisecond)

	didA := "did:example:alice"
	didB := "did:example:bob"

	te.pushRecordEvents(didA, 3, false)
	te.pushRecordEvents(didB, 3, false)

	msgs := consumer.waitForMessages(6, 100*time.Millisecond)
	require.Len(t, msgs, 6)

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

	time.Sleep(20 * time.Millisecond)

	n := 3
	te.pushRecordEvents("did:example:ackuser", n, false)

	msgs := consumer.waitForMessages(n, 100*time.Millisecond)
	require.Len(t, msgs, n)

	for _, msg := range msgs {
		err := consumer.sendAck(msg.ID)
		require.NoError(t, err)
	}
}

func TestWebsocketAck_OrderingHistorical(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebsocketAck,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	time.Sleep(20 * time.Millisecond)

	did := "did:example:historical"
	te.pushRecordEvents(did, 3, false)

	msgs := consumer.waitForMessages(3, 100*time.Millisecond)
	require.Len(t, msgs, 3)

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

	time.Sleep(20 * time.Millisecond)

	did := "did:example:ordering"

	// Push H1, H2 (historical)
	hIDs := te.pushRecordEvents(did, 2, false)

	msgs := consumer.waitForMessages(2, 100*time.Millisecond)
	require.Len(t, msgs, 2, "expected 2 historical events")

	// Push L1 (live) — blocked until H1 and H2 are acked
	lIDs := te.pushRecordEvents(did, 1, true)

	// Push H3, H4 (historical) — blocked until L1 is acked
	h2IDs := te.pushRecordEvents(did, 2, false)

	// Verify L1 has NOT arrived yet
	earlyMsgs := consumer.waitForMessages(3, 50*time.Millisecond)
	assert.Len(t, earlyMsgs, 2, "L1 should not arrive before H1/H2 are acked")

	// Ack H1 and H2
	for _, id := range hIDs {
		require.NoError(t, consumer.sendAck(id))
	}

	// L1 should now arrive
	msgs = consumer.waitForMessages(3, 100*time.Millisecond)
	require.Len(t, msgs, 3, "expected L1 to arrive after H1/H2 acked")
	assert.Equal(t, lIDs[0], msgs[2].ID, "3rd message should be L1")

	// Verify H3, H4 have NOT arrived yet
	msgs = consumer.waitForMessages(4, 50*time.Millisecond)
	assert.Len(t, msgs, 3, "H3/H4 should not arrive before L1 is acked")

	// Ack L1
	require.NoError(t, consumer.sendAck(lIDs[0]))

	// H3 and H4 should now arrive
	msgs = consumer.waitForMessages(5, 100*time.Millisecond)
	require.Len(t, msgs, 5, "expected H3/H4 to arrive after L1 acked")

	finalIDs := map[uint]bool{msgs[3].ID: true, msgs[4].ID: true}
	assert.True(t, finalIDs[h2IDs[0]], "H3 should be in final batch")
	assert.True(t, finalIDs[h2IDs[1]], "H4 should be in final batch")
}

func TestWebsocketAck_ConsecutiveLiveEvents(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebsocketAck,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	time.Sleep(20 * time.Millisecond)

	did := "did:example:consecutive-live"

	// Push 3 live events for the same DID.
	// Without the blockedOnLive fix, the worker stalls after L1's ack:
	// it clears blockedOnLive but has no pending notification to process L2.
	ids := te.pushRecordEvents(did, 3, true)

	// L1 should arrive immediately
	msgs := consumer.waitForMessages(1, 100*time.Millisecond)
	require.Len(t, msgs, 1)
	assert.Equal(t, ids[0], msgs[0].ID)

	// L2 should NOT arrive yet (blocked on L1 ack)
	msgs = consumer.waitForMessages(2, 50*time.Millisecond)
	assert.Len(t, msgs, 1, "L2 should not arrive before L1 is acked")

	// Ack L1 → L2 should arrive
	require.NoError(t, consumer.sendAck(ids[0]))
	msgs = consumer.waitForMessages(2, 100*time.Millisecond)
	require.Len(t, msgs, 2, "L2 should arrive after L1 acked")
	assert.Equal(t, ids[1], msgs[1].ID)

	// Ack L2 → L3 should arrive
	require.NoError(t, consumer.sendAck(ids[1]))
	msgs = consumer.waitForMessages(3, 100*time.Millisecond)
	require.Len(t, msgs, 3, "L3 should arrive after L2 acked")
	assert.Equal(t, ids[2], msgs[2].ID)
}

// TestWebsocketAck_FastPathLiveBlocksHistorical verifies that when a live event
// is sent via the DIDWorker's fast path (first event for a DID), subsequent
// historical events are blocked until the live event is acked.
// Regression test for: bugfix(cmd/tap): prevent DID worker live-barrier stall
func TestWebsocketAck_FastPathLiveBlocksHistorical(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebsocketAck,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	time.Sleep(20 * time.Millisecond)

	did := "did:example:fast-path-live"

	// L1 is the first event for this DID, so it takes the fast path in addEvent.
	// The bugfix ensures addEvent sets blockedOnLive=true on the fast path.
	lIDs := te.pushRecordEvents(did, 1, true)

	// Wait for L1 to arrive at the consumer
	msgs := consumer.waitForMessages(1, 100*time.Millisecond)
	require.Len(t, msgs, 1)
	assert.Equal(t, lIDs[0], msgs[0].ID)

	// Push H1 (historical) for the same DID.
	// H1 enters addEvent while L1 is in-flight, so it takes the slow path.
	// processPendingEvts should see blockedOnLive=true and hold H1 back.
	te.pushRecordEvents(did, 1, false)

	// H1 should NOT arrive while L1 is unacked
	msgs = consumer.waitForMessages(2, 50*time.Millisecond)
	assert.Len(t, msgs, 1, "H1 should not arrive before L1 is acked")

	// Ack L1 — the live barrier is lifted, H1 should now arrive
	require.NoError(t, consumer.sendAck(lIDs[0]))
	msgs = consumer.waitForMessages(2, 100*time.Millisecond)
	require.Len(t, msgs, 2, "H1 should arrive after L1 acked")
}

func TestWebsocketAck_NoStallOnPreconnectEvents(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	// Push events BEFORE any WebSocket consumer connects
	n := 5
	te.pushRecordEvents("did:example:preconnect", n, false)

	// Give the outbox time to buffer events in the outgoing channel
	time.Sleep(50 * time.Millisecond)

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	msgs := consumer.waitForMessages(n, 100*time.Millisecond)
	require.Len(t, msgs, n, "pre-connect events should still be delivered")
}

func TestWebhook_BasicDelivery(t *testing.T) {
	receiver := newTestWebhookReceiver()
	defer receiver.close()

	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeWebhook,
		webhookURL: receiver.server.URL,
	})

	n := 3
	te.pushRecordEvents("did:example:webhook", n, false)

	msgs := receiver.waitForMessages(n, 100*time.Millisecond)
	require.Len(t, msgs, n)
}

func TestIdentityEvent_Delivery(t *testing.T) {
	te := newTestEnv(t, testEnvOpts{
		outboxMode: OutboxModeFireAndForget,
	})

	consumer, err := newTestConsumer(te.wsURL())
	require.NoError(t, err)
	defer consumer.close()

	time.Sleep(20 * time.Millisecond)

	te.pushIdentityEvent("did:example:identity", "alice.bsky.social", models.AccountStatusActive)

	msgs := consumer.waitForMessages(1, 100*time.Millisecond)
	require.Len(t, msgs, 1)

	msg := msgs[0]
	assert.Equal(t, "identity", msg.Type)
	assert.NotNil(t, msg.IdentityEvt)
	assert.Equal(t, "did:example:identity", msg.IdentityEvt.Did)
	assert.Equal(t, "alice.bsky.social", msg.IdentityEvt.Handle)
	assert.True(t, msg.IdentityEvt.IsActive)
	assert.Equal(t, models.AccountStatusActive, msg.IdentityEvt.Status)
}
