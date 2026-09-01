package identity

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

// blockSingleCallDirectory wraps a Directory and makes the first LookupDID call
// block until its context is cancelled. Subsequent calls delegate to inner.
//
// This is useful for forcing a call to LookupDID to return context.Cancelled,
type blockSingleCallDirectory struct {
	inner   Directory
	called  atomic.Bool
	blocked chan struct{} // closed once the first call is blocking
}

func (d *blockSingleCallDirectory) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	// Only block on the first call for the purpose of tests
	if d.called.CompareAndSwap(false, true) {
		// Signal to the test that this directory is blocked on the context cancellation
		close(d.blocked)
		// Wait for the context to be cancelled, return the error (mocks out first caller returning context cancelled for inner DID lookup)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return d.inner.LookupDID(ctx, did)
}

func (d *blockSingleCallDirectory) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	return d.inner.LookupHandle(ctx, h)
}

func (d *blockSingleCallDirectory) Lookup(ctx context.Context, a syntax.AtIdentifier) (*Identity, error) {
	return d.inner.Lookup(ctx, a)
}

func (d *blockSingleCallDirectory) Purge(ctx context.Context, a syntax.AtIdentifier) error {
	return d.inner.Purge(ctx, a)
}

// TestCancelledContextErrorNotCached verifies that when a LookupDID call has its
// context cancelled while the inner lookup is in flight, the resulting
// context.Canceled error is not stored in the cache. A subsequent call with a
// fresh context must retry the inner directory and succeed.
func TestCancelledContextErrorNotCached(t *testing.T) {
	testDID := syntax.DID("did:plc:abc123")
	testHandle := syntax.Handle("user.example.com")
	testIdent := Identity{
		DID:    testDID,
		Handle: testHandle,
	}

	mock := NewMockDirectory()
	mock.Insert(testIdent)

	blockingDir := &blockSingleCallDirectory{
		inner:   mock,
		blocked: make(chan struct{}),
	}

	// Use long TTLs for test
	cd := NewCacheDirectory(blockingDir, 100, time.Hour, time.Hour, time.Hour)

	// First call: cancel the context while the inner lookup is in flight.
	ctx1, cancel := context.WithCancel(context.Background())

	type result struct {
		ident *Identity
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		ident, err := cd.LookupDID(ctx1, testDID)
		ch <- result{ident, err}
	}()

	// Wait until blocked on the context cancellation
	<-blockingDir.blocked
	cancel()

	res := <-ch
	require.ErrorIs(t, res.err, context.Canceled, "first call should fail with context.Canceled")

	// Second call with a fresh context must not return the cached context error.
	ident, err := cd.LookupDID(context.Background(), testDID)
	require.NoError(t, err)
	require.Equal(t, &testIdent, ident)
}

// TestCoalescedCallNotAffectedByFirstCallerCancellation verifies that when two lookups for the same DID coalesce,
// cancelling the first caller's context does not propagate context.Canceled to the second caller whose context is still valid.
//
// Test that the following bug was fixed: the primary caller's context.Canceled gets written to the identity
// cache and the coalesce channel is closed. The waiting (second) caller wakes, reads context.Canceled out of the cache, and returns it to its own caller —
// even though the second caller's context was never cancelled.
func TestCoalescedCallNotAffectedByFirstCallerCancellation(t *testing.T) {
	testDID := syntax.DID("did:plc:abc456")
	testIdent := Identity{
		DID:    testDID,
		Handle: syntax.Handle("user2.example.com"),
	}

	mock := NewMockDirectory()
	mock.Insert(testIdent)

	blockingDir := &blockSingleCallDirectory{
		inner:   mock,
		blocked: make(chan struct{}),
	}

	// NewCacheDirectory is called before synctest.Run so the LRU's internal
	// ticker goroutine is not part of the bubble and won't block synctest.Wait().
	cd := NewCacheDirectory(blockingDir, 100, time.Hour, time.Hour, time.Hour)

	type result struct {
		ident *Identity
		err   error
	}
	ch1 := make(chan result, 1)
	ch2 := make(chan result, 1)

	synctest.Test(t, func(t *testing.T) {
		ctx1, cancel := context.WithCancel(context.Background())

		// caller 1: blocks inside slow.LookupDID until ctx1 is cancelled.
		go func() {
			ident, err := cd.LookupDID(ctx1, testDID)
			ch1 <- result{ident, err}
		}()

		// Wait until caller 1 is blocking inside slow.LookupDID.
		// caller 2 will now coalesce
		<-blockingDir.blocked

		// caller 2: uses a fresh context that is never cancelled.
		go func() {
			ident, err := cd.LookupDID(context.Background(), testDID)
			ch2 <- result{ident, err}
		}()

		// Wait until all goroutines in the bubble are blocked:
		// - caller 1 is blocked in slow.LookupDID on <-ctx.Done()
		// - caller 2 is blocked in the coalesce select on the result channel
		synctest.Wait()

		// Cancel caller 1's context
		cancel()

		res1 := <-ch1
		res2 := <-ch2

		require.ErrorIs(t, res1.err, context.Canceled, "call 1 must fail with its cancelled context")

		// caller 2's context was never cancelled; it must not receive context.Canceled.
		// Should this return "identity not found error" or retry or return the context.Canceled error but not cache it?
		require.ErrorContains(t, res2.err, "identity not found in cache after coalesce returned")
	})
}
