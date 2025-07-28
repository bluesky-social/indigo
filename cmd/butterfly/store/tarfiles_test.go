package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	err = store.ActiveSync(ctx, stream)
	require.NoError(t, err)

	// Close the store to finalize tar files
	err = store.Close()
	require.NoError(t, err)

	// Verify the tar file was created
	expectedFile := filepath.Join(tmpDir, "did_plc_testuser123.tar.gz")
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
	err = store.ActiveSync(ctx, stream)
	require.NoError(t, err)

	// Close the store
	err = store.Close()
	require.NoError(t, err)

	// Verify tar files were created for each DID
	for _, did := range testDIDs {
		filename := strings.ReplaceAll(did, ":", "_") + ".tar.gz"
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
	err = store.ActiveSync(ctx, stream)
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

		err = store.ActiveSync(ctx, stream)
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

		err = store.ActiveSync(ctx, stream)
		require.NoError(t, err)
		err = store.Close()
		require.NoError(t, err)
	}

	// Verify both records exist in the tar file
	expectedFile := filepath.Join(tmpDir, "did_plc_testuser.tar.gz")
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
	err = store.ActiveSync(ctx, stream)
	require.NoError(t, err)

	err = store.Close()
	require.NoError(t, err)

	// Verify only valid events were processed
	expectedFile := filepath.Join(tmpDir, "did_plc_testuser.tar.gz")
	contents, err := ReadTarFile(expectedFile)
	require.NoError(t, err)

	assert.Len(t, contents, 2)
	assert.Contains(t, contents, "app.bsky.feed.post/valid.json")
	assert.Contains(t, contents, "app.bsky.feed.post/valid2.json")
}

func TestTarfilesStore_KvStore_BasicOperations(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

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

	// Verify files exist on disk
	file1 := filepath.Join(tmpDir, "namespace1", "key1.json")
	assert.FileExists(t, file1)
	file2 := filepath.Join(tmpDir, "namespace2", "key1.json")
	assert.FileExists(t, file2)
}

func TestTarfilesStore_KvStore_GetNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

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

func TestTarfilesStore_KvStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

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

	// Verify file exists
	filePath := filepath.Join(tmpDir, "namespace1", "key1.json")
	assert.FileExists(t, filePath)

	// Delete it
	err = store.KvDel("namespace1", "key1")
	require.NoError(t, err)

	// Verify it's gone
	_, err = store.KvGet("namespace1", "key1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Verify file is gone
	assert.NoFileExists(t, filePath)

	// Delete non-existent key should not error
	err = store.KvDel("namespace1", "nonexistent")
	require.NoError(t, err)

	// Delete from non-existent namespace should not error
	err = store.KvDel("nonexistent_namespace", "key1")
	require.NoError(t, err)
}

func TestTarfilesStore_KvStore_Sanitization(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Test with problematic characters that should be sanitized
	testCases := []struct {
		namespace string
		key       string
		value     string
	}{
		{"namespace/with/slashes", "key/with/slashes", "value1"},
		{"namespace\\with\\backslashes", "key\\with\\backslashes", "value2"},
		{"namespace:with:colons", "key:with:colons", "value3"},
		{"namespace..with..dots", "key..with..dots", "value4"},
		{".hidden_namespace", ".hidden_key", "value5"},
		{"namespace*with?wildcards", "key*with?wildcards", "value6"},
		{"namespace\nwith\nnewlines", "key\twith\ttabs", "value7"},
		{"namespace with spaces", "key with spaces", "value8"},
	}

	for _, tc := range testCases {
		err := store.KvPut(tc.namespace, tc.key, tc.value)
		require.NoError(t, err, "Failed to put %s/%s", tc.namespace, tc.key)

		value, err := store.KvGet(tc.namespace, tc.key)
		require.NoError(t, err, "Failed to get %s/%s", tc.namespace, tc.key)
		assert.Equal(t, tc.value, value)
	}

	// Verify sanitized filenames exist
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	// Should have multiple namespace directories (sanitized)
	assert.Greater(t, len(entries), 0)

	// Check that no directory contains problematic characters
	for _, entry := range entries {
		name := entry.Name()
		if name == ".tmp" {
			continue // system file, skip
		}
		assert.NotContains(t, name, "/")
		assert.NotContains(t, name, "\\")
		assert.NotContains(t, name, "..")
		assert.NotContains(t, name, ":")
		assert.NotContains(t, name, "*")
		assert.NotContains(t, name, "?")
		assert.NotContains(t, name, "\n")
		assert.NotContains(t, name, "\t")
		assert.False(t, strings.HasPrefix(name, "."))
	}
}

func TestTarfilesStore_KvStore_EmptyValues(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Test with empty string value
	err = store.KvPut("namespace1", "emptykey", "")
	require.NoError(t, err)

	value, err := store.KvGet("namespace1", "emptykey")
	require.NoError(t, err)
	assert.Equal(t, "", value)

	// Test empty namespace should error
	err = store.KvPut("", "key1", "value1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace cannot be empty")

	// Test empty key should error
	err = store.KvPut("namespace1", "", "value1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key cannot be empty")
}

func TestTarfilesStore_KvStore_LargeValues(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

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

func TestTarfilesStore_KvStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create store and add data
	store1 := NewTarfilesStore(tmpDir)
	ctx := context.Background()
	err := store1.Setup(ctx)
	require.NoError(t, err)

	err = store1.KvPut("namespace1", "key1", "value1")
	require.NoError(t, err)
	err = store1.KvPut("namespace2", "key2", "value2")
	require.NoError(t, err)

	store1.Close()

	// Create new store instance with same directory
	store2 := NewTarfilesStore(tmpDir)
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

func TestTarfilesStore_KvStore_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewTarfilesStore(tmpDir)

	ctx := context.Background()
	err := store.Setup(ctx)
	require.NoError(t, err)
	defer store.Close()

	// Test concurrent writes to different keys
	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 50

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
