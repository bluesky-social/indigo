package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
)

func TestPebbleStore(t *testing.T) {
	// Create temp directory for test database
	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")

	// Create store
	store := NewPebbleStore(dbPath)

	// Setup
	ctx := context.Background()
	if err := store.Setup(ctx); err != nil {
		t.Fatalf("Failed to setup store: %v", err)
	}
	defer store.Close()

	// Test KV operations
	t.Run("KV Operations", func(t *testing.T) {
		// Test Put
		if err := store.KvPut("test-namespace", "key1", "value1"); err != nil {
			t.Errorf("KvPut failed: %v", err)
		}

		// Test Get
		value, err := store.KvGet("test-namespace", "key1")
		if err != nil {
			t.Errorf("KvGet failed: %v", err)
		}
		if value != "value1" {
			t.Errorf("Expected value1, got %s", value)
		}

		// Test Get non-existent key
		_, err = store.KvGet("test-namespace", "nonexistent")
		if err == nil {
			t.Error("Expected error for non-existent key")
		}

		// Test Delete
		if err := store.KvDel("test-namespace", "key1"); err != nil {
			t.Errorf("KvDel failed: %v", err)
		}

		// Verify deletion
		_, err = store.KvGet("test-namespace", "key1")
		if err == nil {
			t.Error("Expected error after deletion")
		}
	})

	// Test event processing
	t.Run("Event Processing", func(t *testing.T) {
		// Create a channel for events
		eventCh := make(chan remote.StreamEvent, 10)

		// Create a test stream
		stream := &remote.RemoteStream{
			Ch: eventCh,
		}

		// Send test events
		go func() {
			// Send a commit event
			eventCh <- remote.StreamEvent{
				Did:       "did:plc:test123",
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Rev:        "rev1",
					Operation:  remote.OpCreate,
					Collection: "app.bsky.feed.post",
					Rkey:       "rkey1",
					Record: map[string]any{
						"text":      "Hello, world!",
						"createdAt": time.Now().Format(time.RFC3339),
					},
					Cid: "cid1",
				},
			}

			// Send a commit event
			eventCh <- remote.StreamEvent{
				Did:       "did:plc:test123",
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Rev:        "rev2",
					Operation:  remote.OpCreate,
					Collection: "app.bsky.feed.post",
					Rkey:       "rkey2",
					Record: map[string]any{
						"text":      "Hello, world!",
						"createdAt": time.Now().Format(time.RFC3339),
					},
					Cid: "cid1",
				},
			}

			// Send a commit event
			eventCh <- remote.StreamEvent{
				Did:       "did:plc:test123",
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Rev:        "rev3",
					Operation:  remote.OpCreate,
					Collection: "app.bsky.feed.post",
					Rkey:       "rkey3",
					Record: map[string]any{
						"text":      "Hello, world!",
						"createdAt": time.Now().Format(time.RFC3339),
					},
					Cid: "cid1",
				},
			}

			// Close the channel after a short delay
			time.Sleep(100 * time.Millisecond)
			close(eventCh)
		}()

		// Process the stream
		if err := store.ActiveSync(ctx, stream); err != nil {
			t.Errorf("ActiveSync failed: %v", err)
		}

		// Verify data was stored
		records, err := store.ListRecords(ctx, "did:plc:test123", "app.bsky.feed.post", 0)
		if err != nil {
			t.Errorf("ListRecords failed: %v", err)
		}
		if len(records) != 3 {
			t.Errorf("Expected 3 records, got %d", len(records))
		}
	})

	// Test batch processing
	t.Run("Batch Processing", func(t *testing.T) {
		// Create a channel for events
		eventCh := make(chan remote.StreamEvent, 100)

		// Create a test stream
		stream := &remote.RemoteStream{
			Ch: eventCh,
		}

		// Send many events to test batching
		go func() {
			for i := 0; i < 50; i++ {
				eventCh <- remote.StreamEvent{
					Did:       "did:plc:batch",
					Timestamp: time.Now(),
					Kind:      remote.EventKindCommit,
					Commit: &remote.StreamEventCommit{
						Rev:        "rev" + string(rune(i)),
						Operation:  remote.OpCreate,
						Collection: "app.bsky.feed.post",
						Rkey:       "rkey" + string(rune(i)),
						Record: map[string]any{
							"text":      "Post " + string(rune(i)),
							"createdAt": time.Now().Format(time.RFC3339),
						},
						Cid: "cid" + string(rune(i)),
					},
				}
			}
			close(eventCh)
		}()

		// Process the stream
		if err := store.ActiveSync(ctx, stream); err != nil {
			t.Errorf("Batch ActiveSync failed: %v", err)
		}

		// Verify data was stored
		records, err := store.ListRecords(ctx, "did:plc:batch", "app.bsky.feed.post", 0)
		if err != nil {
			t.Errorf("ListRecords failed: %v", err)
		}
		if len(records) != 50 {
			t.Errorf("Expected 50 records, got %d", len(records))
		}
	})

	// Test GetRecord and ListRecords
	t.Run("GetRecord and ListRecords", func(t *testing.T) {
		// Create test data
		testDid := "did:plc:getlist"
		testCollection := "app.bsky.feed.post"

		// Create a channel for events
		eventCh := make(chan remote.StreamEvent, 10)
		stream := &remote.RemoteStream{Ch: eventCh}

		// Send test events with different records
		go func() {
			for i := 1; i <= 5; i++ {
				eventCh <- remote.StreamEvent{
					Did:       testDid,
					Timestamp: time.Now(),
					Kind:      remote.EventKindCommit,
					Commit: &remote.StreamEventCommit{
						Rev:        fmt.Sprintf("rev%d", i),
						Operation:  remote.OpCreate,
						Collection: testCollection,
						Rkey:       fmt.Sprintf("post%d", i),
						Record: map[string]any{
							"text":      fmt.Sprintf("Post number %d", i),
							"createdAt": time.Now().Format(time.RFC3339),
							"index":     i,
						},
						Cid: fmt.Sprintf("cid%d", i),
					},
				}
			}

			// Add a record in a different collection
			eventCh <- remote.StreamEvent{
				Did:       testDid,
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Rev:        "rev-like",
					Operation:  remote.OpCreate,
					Collection: "app.bsky.feed.like",
					Rkey:       "like1",
					Record: map[string]any{
						"subject":   "at://did:plc:other/app.bsky.feed.post/123",
						"createdAt": time.Now().Format(time.RFC3339),
					},
					Cid: "cid-like",
				},
			}

			close(eventCh)
		}()

		// Process the stream
		if err := store.ActiveSync(ctx, stream); err != nil {
			t.Errorf("Failed to sync test data: %v", err)
		}

		// Test GetRecord
		record, err := store.GetRecord(ctx, testDid, testCollection, "post3")
		if err != nil {
			t.Errorf("GetRecord failed: %v", err)
		}
		if record == nil {
			t.Error("GetRecord returned nil record")
		} else {
			if text, ok := record["text"].(string); !ok || text != "Post number 3" {
				t.Errorf("Expected 'Post number 3', got %v", record["text"])
			}
			if index, ok := record["index"].(float64); !ok || int(index) != 3 {
				t.Errorf("Expected index 3, got %v", record["index"])
			}
		}

		// Test GetRecord with non-existent record
		_, err = store.GetRecord(ctx, testDid, testCollection, "nonexistent")
		if err == nil {
			t.Error("Expected error for non-existent record")
		}

		// Test ListRecords
		records, err := store.ListRecords(ctx, testDid, testCollection, 0)
		if err != nil {
			t.Errorf("ListRecords failed: %v", err)
		}
		if len(records) != 5 {
			t.Errorf("Expected 5 records, got %d", len(records))
		}

		// Verify records have rkey metadata
		for _, rec := range records {
			if _, ok := rec["_rkey"].(string); !ok {
				t.Error("Record missing _rkey metadata")
			}
		}

		// Test ListRecords with limit
		limitedRecords, err := store.ListRecords(ctx, testDid, testCollection, 3)
		if err != nil {
			t.Errorf("ListRecords with limit failed: %v", err)
		}
		if len(limitedRecords) != 3 {
			t.Errorf("Expected 3 records with limit, got %d", len(limitedRecords))
		}

		// Test ListAllRecords
		allRecords, err := store.ListAllRecords(ctx, testDid, 0)
		if err != nil {
			t.Errorf("ListAllRecords failed: %v", err)
		}
		if len(allRecords) != 2 {
			t.Errorf("Expected 2 collections, got %d", len(allRecords))
		}
		if posts, ok := allRecords["app.bsky.feed.post"]; !ok || len(posts) != 5 {
			t.Errorf("Expected 5 posts, got %v", len(posts))
		}
		if likes, ok := allRecords["app.bsky.feed.like"]; !ok || len(likes) != 1 {
			t.Errorf("Expected 1 like, got %v", len(likes))
		}

		// Test context cancellation
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately
		_, err = store.ListRecords(cancelCtx, testDid, testCollection, 0)
		if err == nil || err != context.Canceled {
			t.Errorf("Expected context.Canceled error, got %v", err)
		}
	})
}
