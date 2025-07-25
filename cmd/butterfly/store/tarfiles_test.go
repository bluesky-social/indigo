package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTarfilesStore_Setup(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)

	// Check that directories were created
	assert.DirExists(t, tmpDir)
	assert.DirExists(t, filepath.Join(tmpDir, ".tmp"))
}

func TestTarfilesStore_BasicOperations(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)

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

	// Close the store to finalize tar files
	err = store.Close()
	require.NoError(t, err)

	// Verify the tar file was created
	expectedFile := filepath.Join(tmpDir, "did_plc_testuser123.tar")
	assert.FileExists(t, expectedFile)

	// Read and verify the tar contents
	contents, err := ReadTarFile(expectedFile)
	require.NoError(t, err)

	// Check that the post exists and was updated
	postData, exists := contents["app.bsky.feed.post/3jui7kd54zh2y.json"]
	assert.True(t, exists)

	var post map[string]any
	err = json.Unmarshal(postData, &post)
	require.NoError(t, err)
	assert.Equal(t, "Hello, world! (edited)", post["text"])

	// Check that the follow was deleted (contains _deleted: true)
	followData, hasFollow := contents["app.bsky.graph.follow/3jui7kd54zh3z.json"]
	assert.True(t, hasFollow, "Follow record should exist with deletion marker")

	var follow map[string]any
	err = json.Unmarshal(followData, &follow)
	require.NoError(t, err)
	assert.Equal(t, true, follow["_deleted"], "Follow should be marked as deleted")
}

func TestTarfilesStore_MultipleRepos(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)

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

	// Close the store
	err = store.Close()
	require.NoError(t, err)

	// Verify tar files were created for each DID
	for _, did := range testDIDs {
		filename := strings.ReplaceAll(did, ":", "_") + ".tar"
		expectedFile := filepath.Join(tmpDir, filename)
		assert.FileExists(t, expectedFile)
	}
}

func TestTarfilesStore_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

	ctx, cancel := context.WithCancel(context.Background())
	err := store.Setup(ctx)
	require.NoError(t, err)

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

func TestTarfilesStore_AppendToExisting(t *testing.T) {
	tmpDir := t.TempDir()
	testDID := "did:plc:testuser"

	// First run - create initial records
	{
		store := NewTarfilesStore(tmpDir)
		ctx := context.Background()
		err := store.Setup(ctx)
		require.NoError(t, err)

		stream := &remote.RemoteStream{
			Ch: make(chan remote.StreamEvent, 2),
		}

		stream.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.feed.post",
				Rkey:       "post1",
				Record:     map[string]any{"text": "First post"},
			},
		}
		close(stream.Ch)

		err = store.Receive(ctx, stream)
		require.NoError(t, err)
		err = store.Close()
		require.NoError(t, err)
	}

	// Second run - append more records
	{
		store := NewTarfilesStore(tmpDir)
		ctx := context.Background()
		err := store.Setup(ctx)
		require.NoError(t, err)

		stream := &remote.RemoteStream{
			Ch: make(chan remote.StreamEvent, 2),
		}

		stream.Ch <- remote.StreamEvent{
			Did:       testDID,
			Timestamp: time.Now(),
			Kind:      remote.EventKindCommit,
			Commit: &remote.StreamEventCommit{
				Operation:  remote.OpCreate,
				Collection: "app.bsky.feed.post",
				Rkey:       "post2",
				Record:     map[string]any{"text": "Second post"},
			},
		}
		close(stream.Ch)

		err = store.Receive(ctx, stream)
		require.NoError(t, err)
		err = store.Close()
		require.NoError(t, err)
	}

	// Verify both records exist in the tar file
	expectedFile := filepath.Join(tmpDir, "did_plc_testuser.tar")
	contents, err := ReadTarFile(expectedFile)
	require.NoError(t, err)

	assert.Contains(t, contents, "app.bsky.feed.post/post1.json")
	assert.Contains(t, contents, "app.bsky.feed.post/post2.json")
}

func TestTarfilesStore_ErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)

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

	err = store.Close()
	require.NoError(t, err)

	// Verify only valid events were processed
	expectedFile := filepath.Join(tmpDir, "did_plc_testuser.tar")
	contents, err := ReadTarFile(expectedFile)
	require.NoError(t, err)

	assert.Len(t, contents, 2)
	assert.Contains(t, contents, "app.bsky.feed.post/valid.json")
	assert.Contains(t, contents, "app.bsky.feed.post/valid2.json")
}
