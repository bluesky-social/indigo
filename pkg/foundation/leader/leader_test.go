package leader

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/pkg/clock"
	"github.com/stretchr/testify/require"
)

func TestLeaderElection_SingleProcess(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClock := clock.NewMockClock(time.Now())
	becameLeader := make(chan struct{}, 1)

	db := testDB(t)
	dir := testDir(t)
	le := New(db, dir, LeaderElectionConfig{
		Identity:            "single-process-test",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			select {
			case becameLeader <- struct{}{}:
			default:
			}
		},
	})

	go func() {
		_ = le.Run(ctx)
	}()

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
	require.Equal(t, "single-process-test", leader.ID)

	le.Stop()
}

func TestLeaderElection_MultipleProcesses_OneLeader(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dir := testDir(t)

	const numProcesses = 5
	var leaderCount atomic.Int32
	var elections []*LeaderElection

	for i := range numProcesses {
		le := New(db, dir, LeaderElectionConfig{
			Identity:            fmt.Sprintf("process-%d", i),
			Logger:              slog.Default(),
			LeaseDuration:       testLeaseDuration,
			RenewalInterval:     testRenewalInterval,
			AcquisitionInterval: testAcquisitionInterval,
			Clock:               mockClock,
			OnBecameLeader: func(ctx context.Context) {
				leaderCount.Add(1)
			},
			OnLostLeadership: func(ctx context.Context) {
				leaderCount.Add(-1)
			},
		})
		elections = append(elections, le)
	}

	// Start all elections
	for _, le := range elections {
		go func() {
			_ = le.Run(ctx)
		}()
	}

	// Wait for all processes to be waiting on the clock
	waitForWaiters(t, mockClock, numProcesses)

	// Count how many think they're the leader
	actualLeaders := 0
	for _, le := range elections {
		if le.IsLeader() {
			actualLeaders++
		}
	}

	require.Equal(t, 1, actualLeaders, "expected exactly one leader")
	require.Equal(t, int32(1), leaderCount.Load(), "leader count should be 1")

	// Stop all
	for _, le := range elections {
		le.Stop()
	}
}

func TestLeaderElection_GracefulHandoff(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dir := testDir(t)

	var leader1BecameLeader, leader2BecameLeader atomic.Bool
	var leader1LostLeadership atomic.Bool

	le1 := New(db, dir, LeaderElectionConfig{
		Identity:            "graceful-handoff-1",
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

	le2 := New(db, dir, LeaderElectionConfig{
		Identity:            "graceful-handoff-2",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader2BecameLeader.Store(true)
		},
	})

	// Start first leader
	go func() {
		_ = le1.Run(ctx)
	}()

	// Wait for le1 to become leader and start waiting
	require.Eventually(t, func() bool {
		return leader1BecameLeader.Load()
	}, time.Second, time.Millisecond, "le1 should become leader")

	require.True(t, le1.IsLeader())
	require.False(t, le2.IsLeader())

	// Start second election
	go func() {
		_ = le2.Run(ctx)
	}()

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
}

func TestLeaderElection_CrashRecovery(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dir := testDir(t)

	var leader1BecameLeader, leader2BecameLeader atomic.Bool

	ctx1, cancel1 := context.WithCancel(ctx)

	le1 := New(db, dir, LeaderElectionConfig{
		Identity:            "crash-recovery-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader1BecameLeader.Store(true)
		},
	})

	le2 := New(db, dir, LeaderElectionConfig{
		Identity:            "crash-recovery-2",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader2BecameLeader.Store(true)
		},
	})

	// Start le1
	go func() {
		_ = le1.Run(ctx1)
	}()

	// Wait for le1 to become leader
	require.Eventually(t, func() bool {
		return leader1BecameLeader.Load()
	}, time.Second, time.Millisecond, "le1 should become leader")

	// Start le2
	go func() {
		_ = le2.Run(ctx)
	}()

	// Wait for both to be waiting
	waitForWaiters(t, mockClock, 2)

	require.True(t, le1.IsLeader())
	require.False(t, le2.IsLeader())

	// Simulate crash - cancel context without graceful shutdown
	cancel1()
	time.Sleep(20 * time.Millisecond) // Let le1 exit

	// Advance clock past lease expiry so le2 can acquire
	for i := 0; i < 5 && !leader2BecameLeader.Load(); i++ {
		advanceAndWait(mockClock, testLeaseDuration)
	}

	require.True(t, leader2BecameLeader.Load(), "le2 should become leader after le1 crashes")

	require.True(t, le2.IsLeader())

	le2.Stop()
}

func TestLeaderElection_NoDuplicateLeaders(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dir := testDir(t)

	const numProcesses = 10
	var elections []*LeaderElection

	for i := range numProcesses {
		le := New(db, dir, LeaderElectionConfig{
			Identity:            fmt.Sprintf("no-dup-%d", i),
			Logger:              slog.Default(),
			LeaseDuration:       testLeaseDuration,
			RenewalInterval:     testRenewalInterval,
			AcquisitionInterval: testAcquisitionInterval,
			Clock:               mockClock,
		})
		elections = append(elections, le)
	}

	// Start all elections
	for _, le := range elections {
		go func() {
			_ = le.Run(ctx)
		}()
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
}

func TestLeaderElection_LeaseRenewal(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dir := testDir(t)

	var becameLeader atomic.Bool
	var lostLeadership atomic.Bool

	le := New(db, dir, LeaderElectionConfig{
		Identity:            "lease-renewal-test",
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

	go func() {
		_ = le.Run(ctx)
	}()

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
}

func TestLeaderElection_RapidStartStop(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := testDB(t)
	dir := testDir(t)

	mockClock := clock.NewMockClock(time.Now())

	// Rapidly start and stop elections
	for i := range 10 {
		le := New(db, dir, LeaderElectionConfig{
			Identity:            fmt.Sprintf("rapid-%d", i),
			Logger:              slog.Default(),
			LeaseDuration:       testLeaseDuration,
			RenewalInterval:     testRenewalInterval,
			AcquisitionInterval: testAcquisitionInterval,
			Clock:               mockClock,
		})

		runCtx, runCancel := context.WithCancel(ctx)
		go func() {
			_ = le.Run(runCtx)
		}()

		waitForWaiters(t, mockClock, 1)
		le.Stop()
		runCancel()

		// Advance clock to clear any pending waiters
		mockClock.Advance(testLeaseDuration)
	}

	// Final election should be able to acquire leadership
	var becameLeader atomic.Bool
	finalCtx, finalCancel := context.WithCancel(ctx)
	defer finalCancel()

	le := New(db, dir, LeaderElectionConfig{
		Identity:            "rapid-final",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			becameLeader.Store(true)
		},
	})

	go func() {
		_ = le.Run(finalCtx)
	}()

	require.Eventually(t, func() bool {
		return becameLeader.Load()
	}, time.Second, time.Millisecond, "final election should become leader")

	le.Stop()
}

func TestLeaderElection_GetLeader_NoLeader(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClock := clock.NewMockClock(time.Now())
	le := testLeaderElection(t, "get-leader-test", mockClock)

	// Should be nil when no leader exists
	leader, err := le.GetLeader(ctx)
	require.NoError(t, err)
	require.Nil(t, leader, "should return nil when no leader exists")
}

func TestLeaderElection_TryAcquireLease_AlreadyLeader(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
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

	_ = le.ReleaseLease(ctx)
}

func TestLeaderElection_RenewLease_NotLeader(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClock := clock.NewMockClock(time.Now())
	le := testLeaderElection(t, "renew-not-leader-test", mockClock)

	// Try to renew without being leader
	err := le.RenewLease(ctx)
	require.ErrorIs(t, err, ErrNotLeader)
}

func TestLeaderElection_ReleaseLease_NotLeader(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClock := clock.NewMockClock(time.Now())
	le := testLeaderElection(t, "release-not-leader-test", mockClock)

	// Try to release without being leader - should not error
	err := le.ReleaseLease(ctx)
	require.NoError(t, err)
}

func TestLeaderElection_DoubleStop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClock := clock.NewMockClock(time.Now())
	le := testLeaderElection(t, "double-stop-test", mockClock)

	go func() {
		_ = le.Run(ctx)
	}()

	waitForWaiters(t, mockClock, 1)

	// Double stop should not panic
	le.Stop()
	le.Stop()
}

func TestLeaderElection_LeaderStealing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockClock := clock.NewMockClock(time.Now())
	db := testDB(t)
	dir := testDir(t)

	var leader1Acquired, leader2Acquired atomic.Int32

	le1 := New(db, dir, LeaderElectionConfig{
		Identity:            "stealer-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader1Acquired.Add(1)
		},
	})

	le2 := New(db, dir, LeaderElectionConfig{
		Identity:            "stealer-2",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               mockClock,
		OnBecameLeader: func(ctx context.Context) {
			leader2Acquired.Add(1)
		},
	})

	// Start both at nearly the same time
	go func() { _ = le1.Run(ctx) }()
	go func() { _ = le2.Run(ctx) }()

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
}

func TestLeaderElection_LeaseExpiry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockClock := clock.NewMockClock(time.Now())
	le := testLeaderElection(t, "lease-expiry-test", mockClock)

	// Acquire leadership
	acquired, err := le.TryAcquireLease(ctx)
	require.NoError(t, err)
	require.True(t, acquired)
	require.True(t, le.IsLeader())

	// Advance clock past lease expiry
	mockClock.Advance(testLeaseDuration + time.Second)

	// IsLeader should now return false
	require.False(t, le.IsLeader(), "should not be leader after lease expires")
}
