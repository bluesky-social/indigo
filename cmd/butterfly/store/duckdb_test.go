package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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
	err = store.ActiveSync(ctx, stream)
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
	err = store.ActiveSync(ctx, stream)
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
	err = store.ActiveSync(ctx, stream)
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
	err = store.ActiveSync(ctx, stream)
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
	err = store.ActiveSync(ctx, stream)
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
	err = store.ActiveSync(ctx, stream)
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

func TestDuckdbStore_BackfillRepo(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	testDID := "did:plc:backfilltest"

	// First, create some initial records using ActiveSync
	stream1 := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 10),
	}

	go func() {
		defer close(stream1.Ch)

		// Create initial posts
		for i := 0; i < 3; i++ {
			stream1.Ch <- remote.StreamEvent{
				Did:       testDID,
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Rev:        "rev" + string(rune(i)),
					Operation:  remote.OpCreate,
					Collection: "app.bsky.feed.post",
					Rkey:       "post" + string(rune(i+1)),
					Record: map[string]any{
						"text":      "Initial post " + string(rune(i)),
						"createdAt": time.Now().Format(time.RFC3339),
					},
					Cid: "cid" + string(rune(i)),
				},
			}
		}
	}()

	err = store.ActiveSync(ctx, stream1)
	require.NoError(t, err)

	// Verify initial records
	stats, err := store.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), stats["total_records"])

	posts, err := store.ListRecords(ctx, testDID, "app.bsky.feed.post", 0)
	require.NoError(t, err)
	assert.Len(t, posts, 3)

	// Now backfill with new data
	stream2 := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 10),
	}

	go func() {
		defer close(stream2.Ch)

		// Send different posts for backfill
		for i := 0; i < 5; i++ {
			stream2.Ch <- remote.StreamEvent{
				Did:       testDID,
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Rev:        "backfillrev" + string(rune(i)),
					Operation:  remote.OpCreate,
					Collection: "app.bsky.feed.post",
					Rkey:       "backfillpost" + string(rune(i+1)),
					Record: map[string]any{
						"text":      "Backfilled post " + string(rune(i)),
						"createdAt": time.Now().Format(time.RFC3339),
					},
					Cid: "backfillcid" + string(rune(i)),
				},
			}
		}
	}()

	// Backfill should replace all existing records for this DID
	err = store.BackfillRepo(ctx, testDID, stream2)
	require.NoError(t, err)

	// Verify old records are gone and new ones are present
	stats, err = store.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(5), stats["total_records"])

	posts, err = store.ListRecords(ctx, testDID, "app.bsky.feed.post", 0)
	require.NoError(t, err)
	assert.Len(t, posts, 5)

	// Verify old posts are gone
	for i := 0; i < 3; i++ {
		post, err := store.GetRecord(ctx, testDID, "app.bsky.feed.post", "post"+string(rune(i+1)))
		require.NoError(t, err)
		assert.Nil(t, post)
	}

	// Verify new posts exist
	for i := 0; i < 5; i++ {
		post, err := store.GetRecord(ctx, testDID, "app.bsky.feed.post", "backfillpost"+string(rune(i+1)))
		fmt.Printf("%d %v\n", i, post)
		require.NoError(t, err)
		assert.NotNil(t, post)
		assert.Contains(t, post["text"], "Backfilled post")
	}
}

func TestDuckdbStore_BackfillRepo_MultipleCollections(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	testDID := "did:plc:multicollection"

	// Create initial records across multiple collections
	stream1 := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 10),
	}

	go func() {
		defer close(stream1.Ch)

		stream1.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.feed.post",
				Rkey:       "oldpost",
				Record:     map[string]any{"text": "Old post"},
			},
		}

		stream1.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.graph.follow",
				Rkey:       "oldfollow",
				Record:     map[string]any{"subject": "did:plc:olduser"},
			},
		}

		stream1.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.actor.profile",
				Rkey:       "self",
				Record:     map[string]any{"displayName": "Old Name"},
			},
		}
	}()

	err = store.ActiveSync(ctx, stream1)
	require.NoError(t, err)

	// Backfill with new data
	stream2 := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 10),
	}

	go func() {
		defer close(stream2.Ch)

		stream2.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.feed.post",
				Rkey:       "newpost1",
				Record:     map[string]any{"text": "New post 1"},
			},
		}

		stream2.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.feed.post",
				Rkey:       "newpost2",
				Record:     map[string]any{"text": "New post 2"},
			},
		}

		stream2.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.actor.profile",
				Rkey:       "self",
				Record:     map[string]any{"displayName": "New Name"},
			},
		}
	}()

	err = store.BackfillRepo(ctx, testDID, stream2)
	require.NoError(t, err)

	// Verify backfill replaced ALL collections for this DID
	posts, err := store.ListRecords(ctx, testDID, "app.bsky.feed.post", 0)
	require.NoError(t, err)
	assert.Len(t, posts, 2)

	// Old follow should be gone
	follow, err := store.GetRecord(ctx, testDID, "app.bsky.graph.follow", "oldfollow")
	require.NoError(t, err)
	assert.Nil(t, follow)

	// Profile should be updated
	profile, err := store.GetRecord(ctx, testDID, "app.bsky.actor.profile", "self")
	require.NoError(t, err)
	assert.NotNil(t, profile)
	assert.Equal(t, "New Name", profile["displayName"])
}

func TestDuckdbStore_BackfillRepo_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx, cancel := context.WithCancel(context.Background())
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	testDID := "did:plc:canceltest"

	// Create a stream that sends many events
	stream := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 100),
	}

	go func() {
		defer close(stream.Ch)
		for i := 0; i < 100; i++ {
			stream.Ch <- remote.StreamEvent{
				Did:       testDID,
				Timestamp: time.Now(),
				Kind:      remote.EventKindCommit,
				Commit: &remote.StreamEventCommit{
					Operation:  remote.OpCreate,
					Collection: "app.bsky.feed.post",
					Rkey:       "post" + string(rune(i)),
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

	// Backfill should stop when context is cancelled
	err = store.BackfillRepo(ctx, testDID, stream)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestDuckdbStore_BackfillRepo_EmptyStream(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	testDID := "did:plc:emptytest"

	// Create some initial records
	stream1 := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent, 2),
	}

	go func() {
		defer close(stream1.Ch)
		stream1.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.feed.post",
				Rkey:       "post1",
				Record:     map[string]any{"text": "Post to be deleted"},
			},
		}
	}()

	err = store.ActiveSync(ctx, stream1)
	require.NoError(t, err)

	// Backfill with empty stream
	stream2 := &remote.RemoteStream{
		Ch: make(chan remote.StreamEvent),
	}
	close(stream2.Ch)

	err = store.BackfillRepo(ctx, testDID, stream2)
	require.NoError(t, err)

	// Verify all records for this DID were deleted
	posts, err := store.ListRecords(ctx, testDID, "app.bsky.feed.post", 0)
	require.NoError(t, err)
	assert.Len(t, posts, 0)
}

func TestDuckdbStore_KvStore_BasicOperations(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Test Put and Get
	err = store.KvPut("namespace1", "key1", "value1")
	require.NoError(t, err)

	value, err := store.KvGet("namespace1", "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	// Test updating existing key
	err = store.KvPut("namespace1", "key1", "updated_value1")
	require.NoError(t, err)

	value, err = store.KvGet("namespace1", "key1")
	require.NoError(t, err)
	assert.Equal(t, "updated_value1", value)

	// Test different namespace
	err = store.KvPut("namespace2", "key1", "value2")
	require.NoError(t, err)

	value, err = store.KvGet("namespace2", "key1")
	require.NoError(t, err)
	assert.Equal(t, "value2", value)

	// Verify namespace isolation
	value, err = store.KvGet("namespace1", "key1")
	require.NoError(t, err)
	assert.Equal(t, "updated_value1", value)
}

func TestDuckdbStore_KvStore_GetNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Test get non-existent key
	_, err = store.KvGet("namespace1", "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Test get from non-existent namespace
	_, err = store.KvGet("nonexistent_namespace", "key1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDuckdbStore_KvStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Put a value
	err = store.KvPut("namespace1", "key1", "value1")
	require.NoError(t, err)

	// Verify it exists
	value, err := store.KvGet("namespace1", "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	// Delete it
	err = store.KvDel("namespace1", "key1")
	require.NoError(t, err)

	// Verify it's gone
	_, err = store.KvGet("namespace1", "key1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Delete non-existent key should not error
	err = store.KvDel("namespace1", "nonexistent")
	require.NoError(t, err)

	// Delete from non-existent namespace should not error
	err = store.KvDel("nonexistent_namespace", "key1")
	require.NoError(t, err)
}

func TestDuckdbStore_KvStore_SpecialCharacters(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Test with special characters in namespace, key, and value
	specialNamespace := "namespace:with:colons"
	specialKey := "key/with/slashes"
	specialValue := `{"json": "value", "with": "quotes 'and' \"escapes\"", "newline": "test\nvalue"}`

	err = store.KvPut(specialNamespace, specialKey, specialValue)
	require.NoError(t, err)

	value, err := store.KvGet(specialNamespace, specialKey)
	require.NoError(t, err)
	assert.Equal(t, specialValue, value)

	// Test with empty string value
	err = store.KvPut("namespace1", "emptykey", "")
	require.NoError(t, err)

	value, err = store.KvGet("namespace1", "emptykey")
	require.NoError(t, err)
	assert.Equal(t, "", value)
}

func TestDuckdbStore_KvStore_LargeValues(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Test with large value (1MB)
	largeValue := strings.Repeat("a", 1024*1024)
	
	err = store.KvPut("namespace1", "largekey", largeValue)
	require.NoError(t, err)

	value, err := store.KvGet("namespace1", "largekey")
	require.NoError(t, err)
	assert.Equal(t, largeValue, value)
}

func TestDuckdbStore_KvStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create store and add data
	store1 := NewDuckdbStore(dbPath)
	ctx := context.Background()
	err := store1.Setup(ctx)
	require.NoError(t, err)

	err = store1.KvPut("namespace1", "key1", "value1")
	require.NoError(t, err)
	err = store1.KvPut("namespace2", "key2", "value2")
	require.NoError(t, err)

	store1.Close()

	// Create new store instance with same DB path
	store2 := NewDuckdbStore(dbPath)
	err = store2.Setup(ctx)
	require.NoError(t, err)
	defer store2.Close()

	// Verify data persisted
	value, err := store2.KvGet("namespace1", "key1")
	require.NoError(t, err)
	assert.Equal(t, "value1", value)

	value, err = store2.KvGet("namespace2", "key2")
	require.NoError(t, err)
	assert.Equal(t, "value2", value)
}

func TestDuckdbStore_KvStore_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store := NewDuckdbStore(dbPath)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Test concurrent writes to different keys
	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key_%d_%d", goroutineID, j)
				value := fmt.Sprintf("value_%d_%d", goroutineID, j)
				err := store.KvPut("concurrent_namespace", key, value)
				assert.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify all values were written correctly
	for i := 0; i < numGoroutines; i++ {
		for j := 0; j < numOperations; j++ {
			key := fmt.Sprintf("key_%d_%d", i, j)
			expectedValue := fmt.Sprintf("value_%d_%d", i, j)
			
			value, err := store.KvGet("concurrent_namespace", key)
			require.NoError(t, err)
			assert.Equal(t, expectedValue, value)
		}
	}
}
