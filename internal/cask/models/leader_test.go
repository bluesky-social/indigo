package models

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

const (
	testLeaseDuration       = 10 * time.Second
	testRenewalInterval     = 3 * time.Second
	testAcquisitionInterval = 2 * time.Second
)

func testModels(t *testing.T) *Models {
	t.Helper()
	db := testDB(t)
	m, err := NewWithPrefix(otel.Tracer("test"), db.Database, t.Name())
	require.NoError(t, err)
	return m
}

func testLeaderElection(t *testing.T, m *Models, identity string, clock Clock) *LeaderElection {
	t.Helper()
	db := testDB(t)
	return m.NewLeaderElection(db, LeaderElectionConfig{
		Identity:            identity,
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clock,
	})
}

// waitForWaiters waits until the mock clock has at least n waiters.
// Uses a short real-time sleep between checks for goroutine scheduling.
func waitForWaiters(t *testing.T, clock *MockClock, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return clock.WaiterCount() >= n
	}, 2*time.Second, 5*time.Millisecond, "timed out waiting for %d waiters, got %d", n, clock.WaiterCount())
}

// advanceAndWait advances the mock clock and waits briefly for goroutines to process
func advanceAndWait(clock *MockClock, d time.Duration) {
	clock.Advance(d)
	time.Sleep(10 * time.Millisecond) // Let goroutines process
}

func TestLeaderElection_SingleProcess(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clock := NewMockClock(time.Now())
	becameLeader := make(chan struct{}, 1)

	le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "single-process-test",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clock,
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
	leader, err := le.GetFirehoseLeader(ctx)
	require.NoError(t, err)
	require.NotNil(t, leader)
	require.Equal(t, "single-process-test", leader.Id)

	le.Stop()
}

func TestLeaderElection_MultipleProcesses_OneLeader(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clock := NewMockClock(time.Now())

	const numProcesses = 5
	var leaderCount atomic.Int32
	var elections []*LeaderElection

	for i := range numProcesses {
		le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
			Identity:            fmt.Sprintf("process-%d", i),
			Logger:              slog.Default(),
			LeaseDuration:       testLeaseDuration,
			RenewalInterval:     testRenewalInterval,
			AcquisitionInterval: testAcquisitionInterval,
			Clock:               clock,
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
	waitForWaiters(t, clock, numProcesses)

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

	m := testModels(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clock := NewMockClock(time.Now())

	var leader1BecameLeader, leader2BecameLeader atomic.Bool
	var leader1LostLeadership atomic.Bool

	le1 := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "graceful-handoff-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clock,
		OnBecameLeader: func(ctx context.Context) {
			leader1BecameLeader.Store(true)
		},
		OnLostLeadership: func(ctx context.Context) {
			leader1LostLeadership.Store(true)
		},
	})

	le2 := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "graceful-handoff-2",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clock,
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
	waitForWaiters(t, clock, 2)

	require.False(t, le2.IsLeader(), "le2 should not be leader while le1 is active")

	// Stop le1 gracefully (releases lease)
	le1.Stop()

	// Advance clock so le2's wait expires and it can try to acquire.
	// We may need multiple advances as le2 processes.
	for i := 0; i < 5 && !leader2BecameLeader.Load(); i++ {
		clock.Advance(testAcquisitionInterval)
		waitForWaiters(t, clock, 1)
	}

	require.True(t, leader2BecameLeader.Load(), "le2 should become leader after le1 stops")

	require.True(t, le2.IsLeader())
	require.True(t, leader1LostLeadership.Load(), "le1 should have lost leadership")

	le2.Stop()
}

func TestLeaderElection_CrashRecovery(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clock := NewMockClock(time.Now())

	var leader1BecameLeader, leader2BecameLeader atomic.Bool

	ctx1, cancel1 := context.WithCancel(ctx)

	le1 := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "crash-recovery-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clock,
		OnBecameLeader: func(ctx context.Context) {
			leader1BecameLeader.Store(true)
		},
	})

	le2 := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "crash-recovery-2",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clock,
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
	waitForWaiters(t, clock, 2)

	require.True(t, le1.IsLeader())
	require.False(t, le2.IsLeader())

	// Simulate crash - cancel context without graceful shutdown
	cancel1()
	time.Sleep(20 * time.Millisecond) // Let le1 exit

	// Advance clock past lease expiry so le2 can acquire
	for i := 0; i < 5 && !leader2BecameLeader.Load(); i++ {
		advanceAndWait(clock, testLeaseDuration)
	}

	require.True(t, leader2BecameLeader.Load(), "le2 should become leader after le1 crashes")

	require.True(t, le2.IsLeader())

	le2.Stop()
}

func TestLeaderElection_NoDuplicateLeaders(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clock := NewMockClock(time.Now())

	const numProcesses = 10
	var elections []*LeaderElection

	for i := range numProcesses {
		le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
			Identity:            fmt.Sprintf("no-dup-%d", i),
			Logger:              slog.Default(),
			LeaseDuration:       testLeaseDuration,
			RenewalInterval:     testRenewalInterval,
			AcquisitionInterval: testAcquisitionInterval,
			Clock:               clock,
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
	waitForWaiters(t, clock, numProcesses)

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
		advanceAndWait(clock, testRenewalInterval)
		waitForWaiters(t, clock, numProcesses)
	}

	// Stop all
	for _, le := range elections {
		le.Stop()
	}
}

func TestLeaderElection_LeaseRenewal(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clock := NewMockClock(time.Now())

	var becameLeader atomic.Bool
	var lostLeadership atomic.Bool

	le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "lease-renewal-test",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clock,
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
		waitForWaiters(t, clock, 1)
		require.True(t, le.IsLeader(), "should still be leader at check %d", i)
		require.False(t, lostLeadership.Load(), "should not have lost leadership at check %d", i)

		// Advance clock by renewal interval (not past lease expiry)
		clock.Advance(testRenewalInterval)
	}

	le.Stop()
}

func TestLeaderElection_RapidStartStop(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx := context.Background()

	clock := NewMockClock(time.Now())

	// Rapidly start and stop elections
	for i := range 10 {
		le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
			Identity:            fmt.Sprintf("rapid-%d", i),
			Logger:              slog.Default(),
			LeaseDuration:       testLeaseDuration,
			RenewalInterval:     testRenewalInterval,
			AcquisitionInterval: testAcquisitionInterval,
			Clock:               clock,
		})

		runCtx, runCancel := context.WithCancel(ctx)
		go func() {
			_ = le.Run(runCtx)
		}()

		waitForWaiters(t, clock, 1)
		le.Stop()
		runCancel()

		// Advance clock to clear any pending waiters
		clock.Advance(testLeaseDuration)
	}

	// Final election should be able to acquire leadership
	var becameLeader atomic.Bool
	finalCtx, finalCancel := context.WithCancel(ctx)
	defer finalCancel()

	le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "rapid-final",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clock,
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

func TestLeaderElection_GetFirehoseLeader_NoLeader(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx := context.Background()

	clock := NewMockClock(time.Now())
	le := testLeaderElection(t, m, "get-leader-test", clock)

	// Should be nil when no leader exists
	leader, err := le.GetFirehoseLeader(ctx)
	require.NoError(t, err)
	require.Nil(t, leader, "should return nil when no leader exists")
}

func TestLeaderElection_TryAcquireLease_AlreadyLeader(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx := context.Background()

	clock := NewMockClock(time.Now())
	le := testLeaderElection(t, m, "already-leader-test", clock)

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

	m := testModels(t)
	ctx := context.Background()

	clock := NewMockClock(time.Now())
	le := testLeaderElection(t, m, "renew-not-leader-test", clock)

	// Try to renew without being leader
	err := le.RenewLease(ctx)
	require.ErrorIs(t, err, ErrNotLeader)
}

func TestLeaderElection_ReleaseLease_NotLeader(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx := context.Background()

	clock := NewMockClock(time.Now())
	le := testLeaderElection(t, m, "release-not-leader-test", clock)

	// Try to release without being leader - should not error
	err := le.ReleaseLease(ctx)
	require.NoError(t, err)
}

func TestLeaderElection_DoubleStop(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clock := NewMockClock(time.Now())
	le := testLeaderElection(t, m, "double-stop-test", clock)

	go func() {
		_ = le.Run(ctx)
	}()

	waitForWaiters(t, clock, 1)

	// Double stop should not panic
	le.Stop()
	le.Stop()
}

func TestLeaderElection_LeaderStealing(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clock := NewMockClock(time.Now())

	var leader1Acquired, leader2Acquired atomic.Int32

	le1 := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "stealer-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clock,
		OnBecameLeader: func(ctx context.Context) {
			leader1Acquired.Add(1)
		},
	})

	le2 := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "stealer-2",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clock,
		OnBecameLeader: func(ctx context.Context) {
			leader2Acquired.Add(1)
		},
	})

	// Start both at nearly the same time
	go func() { _ = le1.Run(ctx) }()
	go func() { _ = le2.Run(ctx) }()

	// Wait for both to be waiting
	waitForWaiters(t, clock, 2)

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

	m := testModels(t)
	ctx := context.Background()

	clock := NewMockClock(time.Now())
	le := testLeaderElection(t, m, "lease-expiry-test", clock)

	// Acquire leadership
	acquired, err := le.TryAcquireLease(ctx)
	require.NoError(t, err)
	require.True(t, acquired)
	require.True(t, le.IsLeader())

	// Advance clock past lease expiry
	clock.Advance(testLeaseDuration + time.Second)

	// IsLeader should now return false
	require.False(t, le.IsLeader(), "should not be leader after lease expires")
}
