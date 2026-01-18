package leader

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/internal/testutil"
	"github.com/bluesky-social/indigo/pkg/clock"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

const (
	testLeaseDuration       = 10 * time.Second
	testRenewalInterval     = 3 * time.Second
	testAcquisitionInterval = 2 * time.Second
)

func testDB(t *testing.T) *foundation.DB {
	t.Helper()
	return testutil.TestDB(t)
}

func testDirPath(t *testing.T) []string {
	t.Helper()
	// Include a UUID for complete isolation between test runs and parallel tests
	return []string{t.Name(), uuid.NewString(), "leader"}
}

func testLeaderElection(t *testing.T, identity string, clk clock.Clock) *LeaderElection {
	t.Helper()
	db := testDB(t)
	le, err := New(db, testDirPath(t), LeaderElectionConfig{
		ID:                  identity,
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clk,
	})
	require.NoError(t, err)
	return le
}

// waitForWaiters waits until the mock clock has at least n waiters.
// Uses a short real-time sleep between checks for goroutine scheduling.
func waitForWaiters(t *testing.T, clk *clock.MockClock, n int) {
	t.Helper()
	require.Eventually(t,
		func() bool {
			return clk.WaiterCount() >= n
		},
		2*time.Second,
		time.Millisecond,
		"timed out waiting for %d waiters, got %d",
		n,
		clk.WaiterCount(),
	)
}

// advanceAndWait advances the mock clock and waits briefly for goroutines to process
func advanceAndWait(clk *clock.MockClock, d time.Duration) {
	clk.Advance(d)
	time.Sleep(10 * time.Millisecond) // Let goroutines process
}

func TestLeaderElection_SingleProcess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	becameLeader := make(chan any)

	db := testDB(t)
	dirPath := testDirPath(t)
	le, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "single-process-test",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			becameLeader <- struct{}{}
		},
	})
	require.NoError(t, err)

	errs := errgroup.Group{}
	errs.Go(func() error { return le.Run(ctx) })

	// Should become leader almost immediately (after initial acquisition)
	select {
	case <-becameLeader:
		// Success
	case <-time.After(time.Second):
		t.Fatal("timed out waiting to become leader")
	}

	require.True(t, le.IsLeader())

	// Verify we can read the leader info
	leader, err := le.GetLeader(ctx)
	require.NoError(t, err)
	require.NotNil(t, leader)
	require.Equal(t, "single-process-test", leader.Id)

	le.Stop()
	require.NoError(t, errs.Wait())
}

func TestLeaderElection_MultipleProcesses(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dirPath := testDirPath(t)

	const numProcesses = 10
	var elections []*LeaderElection

	for i := range numProcesses {
		le, err := New(db, dirPath, LeaderElectionConfig{
			ID:                  fmt.Sprintf("multiple-%d", i),
			Logger:              slog.Default(),
			LeaseDuration:       testLeaseDuration,
			RenewalInterval:     testRenewalInterval,
			AcquisitionInterval: testAcquisitionInterval,
			Clock:               mockClock,
		})
		require.NoError(t, err)
		elections = append(elections, le)
	}

	// Start all elections
	errs := errgroup.Group{}
	for _, le := range elections {
		errs.Go(func() error { return le.Run(ctx) })
	}

	// Wait for all to be waiting
	waitForWaiters(t, mockClock, numProcesses)

	// Check leader count multiple times while advancing clock
	for i := range 20 {
		leaders := 0
		for _, le := range elections {
			if le.IsLeader() {
				leaders++
			}
		}
		require.LessOrEqual(t, leaders, 1, "should never have more than 1 leader (iteration %d)", i)

		// Advance clock to trigger renewal/acquisition cycles
		advanceAndWait(mockClock, testRenewalInterval)
		waitForWaiters(t, mockClock, numProcesses)
	}

	// Stop all
	for _, le := range elections {
		le.Stop()
	}
	require.NoError(t, errs.Wait())
}

func TestLeaderElection_GracefulHandoff(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dirPath := testDirPath(t)

	var leader1BecameLeader, leader2BecameLeader atomic.Bool
	var leader1LostLeadership atomic.Bool

	le1, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "graceful-handoff-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader1BecameLeader.Store(true)
		},
		OnLostLeadership: func(ctx context.Context) {
			leader1LostLeadership.Store(true)
		},
	})
	require.NoError(t, err)

	le2, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "graceful-handoff-2",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader2BecameLeader.Store(true)
		},
	})
	require.NoError(t, err)

	// Start first leader
	errs := errgroup.Group{}
	errs.Go(func() error { return le1.Run(ctx) })

	// Wait for le1 to become leader and start waiting
	require.Eventually(t, func() bool {
		return leader1BecameLeader.Load()
	}, time.Second, time.Millisecond, "le1 should become leader")

	require.True(t, le1.IsLeader())
	require.False(t, le2.IsLeader())

	// Start second election
	errs.Go(func() error { return le2.Run(ctx) })

	// Wait for both to be waiting
	waitForWaiters(t, mockClock, 2)

	require.False(t, le2.IsLeader(), "le2 should not be leader while le1 is active")

	// Stop le1 gracefully (releases lease)
	le1.Stop()

	// Advance clock so le2's wait expires and it can try to acquire.
	// We may need multiple advances as le2 processes.
	for i := 0; i < 5 && !leader2BecameLeader.Load(); i++ {
		mockClock.Advance(testAcquisitionInterval)
		waitForWaiters(t, mockClock, 1)
	}

	require.True(t, leader2BecameLeader.Load(), "le2 should become leader after le1 stops")

	require.True(t, le2.IsLeader())
	require.True(t, leader1LostLeadership.Load(), "le1 should have lost leadership")

	le2.Stop()
	require.NoError(t, errs.Wait())
}

func TestLeaderElection_CrashRecovery(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dirPath := testDirPath(t)

	var leader1BecameLeader, leader2BecameLeader atomic.Bool

	ctx1, cancel1 := context.WithCancel(ctx)

	le1, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "crash-recovery-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader1BecameLeader.Store(true)
		},
	})
	require.NoError(t, err)

	le2, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "crash-recovery-2",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader2BecameLeader.Store(true)
		},
	})
	require.NoError(t, err)

	// Start le1 with its own context (will be cancelled to simulate crash)
	errs1 := errgroup.Group{}
	errs1.Go(func() error { return le1.Run(ctx1) })

	// Wait for le1 to become leader
	require.Eventually(t, func() bool {
		return leader1BecameLeader.Load()
	}, time.Second, time.Millisecond, "le1 should become leader")

	// Start le2
	errs2 := errgroup.Group{}
	errs2.Go(func() error { return le2.Run(ctx) })

	// Wait for both to be waiting
	waitForWaiters(t, mockClock, 2)

	require.True(t, le1.IsLeader())
	require.False(t, le2.IsLeader())

	// Simulate crash - cancel context without graceful shutdown
	cancel1()
	require.NoError(t, errs1.Wait())

	// Advance clock past lease expiry so le2 can acquire
	for i := 0; i < 5 && !leader2BecameLeader.Load(); i++ {
		advanceAndWait(mockClock, testLeaseDuration)
	}

	require.True(t, leader2BecameLeader.Load(), "le2 should become leader after le1 crashes")
	require.True(t, le2.IsLeader())

	le2.Stop()
	require.NoError(t, errs2.Wait())
}

func TestLeaderElection_LeaseRenewal(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dirPath := testDirPath(t)

	var becameLeader atomic.Bool
	var lostLeadership atomic.Bool

	le, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "lease-renewal-test",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			becameLeader.Store(true)
		},
		OnLostLeadership: func(ctx context.Context) {
			lostLeadership.Store(true)
		},
	})
	require.NoError(t, err)

	errs := errgroup.Group{}
	errs.Go(func() error { return le.Run(ctx) })

	// Wait for leader
	require.Eventually(t, func() bool {
		return becameLeader.Load()
	}, time.Second, time.Millisecond)

	// Simulate multiple renewal cycles - should maintain leadership
	for i := range 10 {
		waitForWaiters(t, mockClock, 1)
		require.True(t, le.IsLeader(), "should still be leader at check %d", i)
		require.False(t, lostLeadership.Load(), "should not have lost leadership at check %d", i)

		// Advance clock by renewal interval (not past lease expiry)
		mockClock.Advance(testRenewalInterval)
	}

	le.Stop()
	require.NoError(t, errs.Wait())
}

func TestLeaderElection_RapidStartStop(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	db := testDB(t)
	dirPath := testDirPath(t)

	mockClock := clock.NewMockClock(time.Now())

	// Rapidly start and stop elections
	for i := range 10 {
		le, err := New(db, dirPath, LeaderElectionConfig{
			ID:                  fmt.Sprintf("rapid-%d", i),
			Logger:              slog.Default(),
			LeaseDuration:       testLeaseDuration,
			RenewalInterval:     testRenewalInterval,
			AcquisitionInterval: testAcquisitionInterval,
			Clock:               mockClock,
		})
		require.NoError(t, err)

		errs := errgroup.Group{}
		errs.Go(func() error { return le.Run(ctx) })

		waitForWaiters(t, mockClock, 1)
		le.Stop()
		require.NoError(t, errs.Wait())

		// Advance clock to clear any pending waiters
		mockClock.Advance(testLeaseDuration)
	}

	// Final election should be able to acquire leadership
	var becameLeader atomic.Bool

	le, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "rapid-final",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			becameLeader.Store(true)
		},
	})
	require.NoError(t, err)

	errs := errgroup.Group{}
	errs.Go(func() error { return le.Run(ctx) })

	require.Eventually(t, func() bool {
		return becameLeader.Load()
	}, time.Second, time.Millisecond, "final election should become leader")

	le.Stop()
	require.NoError(t, errs.Wait())
}

func TestLeaderElection_GetLeader_NoLeader(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	le := testLeaderElection(t, "get-leader-test", mockClock)

	// Should be nil when no leader exists
	leader, err := le.GetLeader(ctx)
	require.NoError(t, err)
	require.Nil(t, leader, "should return nil when no leader exists")
}

func TestLeaderElection_TryAcquireLease_AlreadyLeader(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	le := testLeaderElection(t, "already-leader-test", mockClock)

	// First acquisition
	acquired, err := le.TryAcquireLease(ctx)
	require.NoError(t, err)
	require.True(t, acquired)
	require.True(t, le.IsLeader())

	// Second acquisition by same identity should succeed (renewal)
	acquired, err = le.TryAcquireLease(ctx)
	require.NoError(t, err)
	require.True(t, acquired)
	require.True(t, le.IsLeader())

	require.NoError(t, le.ReleaseLease(ctx))
}

func TestLeaderElection_RenewLease_NotLeader(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	le := testLeaderElection(t, "renew-not-leader-test", mockClock)

	// Try to renew without being leader
	err := le.RenewLease(ctx)
	require.ErrorIs(t, err, ErrNotLeader)
}

func TestLeaderElection_ReleaseLease_NotLeader(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	le := testLeaderElection(t, "release-not-leader-test", mockClock)

	// Try to release without being leader - should not error
	err := le.ReleaseLease(ctx)
	require.NoError(t, err)
}

func TestLeaderElection_LeaderRacing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dirPath := testDirPath(t)

	var leader1Acquired, leader2Acquired atomic.Int32

	le1, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "racer-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader1Acquired.Add(1)
		},
	})
	require.NoError(t, err)

	le2, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "racer-2",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader2Acquired.Add(1)
		},
	})
	require.NoError(t, err)

	// Start both at nearly the same time
	errs := errgroup.Group{}
	errs.Go(func() error { return le1.Run(ctx) })
	errs.Go(func() error { return le2.Run(ctx) })

	// Wait for both to be waiting
	waitForWaiters(t, mockClock, 2)

	// Exactly one should be leader
	require.True(t, (le1.IsLeader() || le2.IsLeader()), "one should be leader")
	require.False(t, (le1.IsLeader() && le2.IsLeader()), "both should not be leader")

	// Total acquisitions should be 1
	totalAcquisitions := leader1Acquired.Load() + leader2Acquired.Load()
	require.Equal(t, int32(1), totalAcquisitions, "exactly one acquisition should have happened")

	le1.Stop()
	le2.Stop()
	require.NoError(t, errs.Wait())
}

func TestLeaderElection_LeaseExpiry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dirPath := testDirPath(t)

	le1, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "expiry-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
	})
	require.NoError(t, err)

	le2, err := New(db, dirPath, LeaderElectionConfig{
		ID:                  "expiry-2",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
	})
	require.NoError(t, err)

	// le1 acquires leadership
	acquired, err := le1.TryAcquireLease(ctx)
	require.NoError(t, err)
	require.True(t, acquired, "le1 should acquire leadership")
	require.True(t, le1.IsLeader())

	// le2 should not be able to acquire while le1's lease is valid
	acquired, err = le2.TryAcquireLease(ctx)
	require.NoError(t, err)
	require.False(t, acquired, "le2 should not acquire while le1's lease is valid")
	require.False(t, le2.IsLeader())

	// Advance clock past lease expiry
	mockClock.Advance(testLeaseDuration + time.Second)

	// le1 should no longer be considered leader (lease expired)
	require.False(t, le1.IsLeader(), "le1 should not be leader after lease expires")

	// Now le2 should be able to acquire the expired lease
	acquired, err = le2.TryAcquireLease(ctx)
	require.NoError(t, err)
	require.True(t, acquired, "le2 should acquire after le1's lease expires")
	require.True(t, le2.IsLeader())

	// Verify the leader info shows le2 as the current leader
	leader, err := le2.GetLeader(ctx)
	require.NoError(t, err)
	require.Equal(t, "expiry-2", leader.Id)
}
