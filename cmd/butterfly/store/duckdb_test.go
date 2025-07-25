package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDuckdbStore_Setup(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Check that database was created
	assert.FileExists(t, dbPath)

	// Verify tables exist by attempting to query
	stats, err := store.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), stats["total_records"])
	assert.Equal(t, int64(0), stats["unique_dids"])
}

func TestDuckdbStore_BasicOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Create a test stream
	stream := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 10),
	}

	testDID := "did:plc:testuser123"

	// Send some test events
	go func() {
		defer close(stream.Ch)

		// Create a post
		stream.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Rev:        "rev123",
				Operation:  remote.OpCreate,
				Collection: "app.bsky.feed.post",
				Rkey:       "3jui7kd54zh2y",
				Record: map[string]any{
					"text":      "Hello, world!",
					"createdAt": time.Now().Format(time.RFC3339),
				},
				Cid: "bafyreigvcqpnqk3dqg",
			},
		}

		// Create a follow
		stream.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Rev:        "rev124",
				Operation:  remote.OpCreate,
				Collection: "app.bsky.graph.follow",
				Rkey:       "3jui7kd54zh3z",
				Record: map[string]any{
					"subject":   "did:plc:alice",
					"createdAt": time.Now().Format(time.RFC3339),
				},
				Cid: "bafyreigvcqpnqk3dqh",
			},
		}

		// Update the post
		stream.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Rev:        "rev125",
				Operation:  remote.OpUpdate,
				Collection: "app.bsky.feed.post",
				Rkey:       "3jui7kd54zh2y",
				Record: map[string]any{
					"text":      "Hello, world! (edited)",
					"createdAt": time.Now().Format(time.RFC3339),
				},
				Cid: "bafyreigvcqpnqk3dqi",
			},
		}

		// Delete the follow
		stream.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Rev:        "rev126",
				Operation:  remote.OpDelete,
				Collection: "app.bsky.graph.follow",
				Rkey:       "3jui7kd54zh3z",
			},
		}
	}()

	// Process the stream
	err = store.Receive(ctx, stream)
	require.NoError(t, err)

	// Verify the post was updated
	post, err := store.GetRecord(ctx, testDID, "app.bsky.feed.post", "3jui7kd54zh2y")
	require.NoError(t, err)
	assert.NotNil(t, post)
	assert.Equal(t, "Hello, world! (edited)", post["text"])

	// Verify the follow was deleted (should return nil)
	follow, err := store.GetRecord(ctx, testDID, "app.bsky.graph.follow", "3jui7kd54zh3z")
	require.NoError(t, err)
	assert.Nil(t, follow)

	// Check stats
	stats, err := store.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats["total_records"])
	assert.Equal(t, int64(1), stats["unique_dids"])

	collections := stats["collections"].(map[string]int64)
	assert.Equal(t, int64(1), collections["app.bsky.feed.post"])
	assert.Equal(t, int64(0), collections["app.bsky.graph.follow"])
}

func TestDuckdbStore_ListRecords(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Create a test stream
	stream := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 10),
	}

	testDID := "did:plc:testuser123"

	// Send multiple posts
	go func() {
		defer close(stream.Ch)

		for i := 0; i < 5; i++ {
			stream.Ch <- remote.StreamEvent{
				Did:       testDID,
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Rev:        "rev" + string(rune(i)),
					Operation:  remote.OpCreate,
					Collection: "app.bsky.feed.post",
					Rkey:       "post" + string(rune(i)),
					Record: map[string]any{
						"text":      "Post number " + string(rune(i+1)),
						"createdAt": time.Now().Format(time.RFC3339),
					},
					Cid: "cid" + string(rune(i)),
				},
			}
		}
	}()

	// Process the stream
	err = store.Receive(ctx, stream)
	require.NoError(t, err)

	// List all posts
	posts, err := store.ListRecords(ctx, testDID, "app.bsky.feed.post", 0)
	require.NoError(t, err)
	assert.Len(t, posts, 5)

	// List with limit
	limitedPosts, err := store.ListRecords(ctx, testDID, "app.bsky.feed.post", 3)
	require.NoError(t, err)
	assert.Len(t, limitedPosts, 3)

	// Verify _rkey is added
	for _, post := range posts {
		assert.Contains(t, post, "_rkey")
		assert.Contains(t, post, "text")
	}
}

func TestDuckdbStore_MultipleRepos(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Create a test stream
	stream := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 10),
	}

	testDIDs := []string{"did:plc:user1", "did:plc:user2", "did:plc:user3"}

	// Send events for multiple DIDs
	go func() {
		defer close(stream.Ch)

		for i, did := range testDIDs {
			stream.Ch <- remote.StreamEvent{
				Did:       did,
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Rev:        "rev" + string(rune(i)),
					Operation:  remote.OpCreate,
					Collection: "app.bsky.actor.profile",
					Rkey:       "self",
					Record: map[string]any{
						"displayName": "User " + string(rune(i+1)),
						"createdAt":   time.Now().Format(time.RFC3339),
					},
				},
			}
		}
	}()

	// Process the stream
	err = store.Receive(ctx, stream)
	require.NoError(t, err)

	// Verify stats
	stats, err := store.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), stats["total_records"])
	assert.Equal(t, int64(3), stats["unique_dids"])

	// Verify each DID has a profile
	for _, did := range testDIDs {
		profile, err := store.GetRecord(ctx, did, "app.bsky.actor.profile", "self")
		require.NoError(t, err)
		assert.NotNil(t, profile)
		assert.Contains(t, profile, "displayName")
	}
}

func TestDuckdbStore_TransactionBatching(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Create a test stream with many events
	stream := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 2000),
	}

	testDID := "did:plc:testuser"

	// Send 1500 events (more than maxBatchSize of 1000)
	go func() {
		defer close(stream.Ch)

		for i := 0; i < 1500; i++ {
			stream.Ch <- remote.StreamEvent{
				Did:       testDID,
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Operation:  remote.OpCreate,
					Collection: "app.bsky.feed.post",
					Rkey:       "post" + string(rune(i)),
					Record: map[string]any{
						"text": "Test post " + string(rune(i)),
						"seq":  i,
					},
				},
			}
		}
	}()

	// Process should handle transaction batching automatically
	err = store.Receive(ctx, stream)
	require.NoError(t, err)

	// Verify all records were saved
	stats, err := store.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1500), stats["total_records"])
}

func TestDuckdbStore_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx, cancel := context.WithCancel(context.Background())
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Create a test stream
	stream := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 10),
	}

	// Send events indefinitely
	go func() {
		defer close(stream.Ch)
		for i := 0; i < 100; i++ {
			stream.Ch <- remote.StreamEvent{
				Did:       "did:plc:testuser",
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Operation:  remote.OpCreate,
					Collection: "app.bsky.feed.post",
					Rkey:       "post" + string(rune(i+1)),
					Record:     map[string]any{"text": "Test post"},
				},
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Cancel context after a short time
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// Process should stop when context is cancelled
	err = store.Receive(ctx, stream)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestDuckdbStore_ErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Create a test stream with invalid events
	stream := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 10),
	}

	go func() {
		defer close(stream.Ch)

		// Valid event
		stream.Ch <- remote.StreamEvent{
			Did:       "did:plc:testuser",
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.feed.post",
				Rkey:       "valid",
				Record:     map[string]any{"text": "Valid post"},
			},
		}

		// Event with nil commit (should be skipped)
		stream.Ch <- remote.StreamEvent{
			Did:       "did:plc:testuser",
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit:    nil,
		}

		// Non-commit event (should be skipped)
		stream.Ch <- remote.StreamEvent{
			Did:       "did:plc:testuser",
			Timestamp: time.Now(),
			Kind:      remote.EventKindIdentity,
			Identity: &remote.StreamEventIdentity{
				Did:    "did:plc:testuser",
				Handle: "testuser.bsky.social",
			},
		}

		// Another valid event
		stream.Ch <- remote.StreamEvent{
			Did:       "did:plc:testuser",
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.feed.post",
				Rkey:       "valid2",
				Record:     map[string]any{"text": "Another valid post"},
			},
		}
	}()

	// Should process without error, skipping invalid events
	err = store.Receive(ctx, stream)
	require.NoError(t, err)

	// Verify only valid events were processed
	stats, err := store.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats["total_records"])

	// Verify the valid records exist
	post1, err := store.GetRecord(ctx, "did:plc:testuser", "app.bsky.feed.post", "valid")
	require.NoError(t, err)
	assert.NotNil(t, post1)

	post2, err := store.GetRecord(ctx, "did:plc:testuser", "app.bsky.feed.post", "valid2")
	require.NoError(t, err)
	assert.NotNil(t, post2)
}