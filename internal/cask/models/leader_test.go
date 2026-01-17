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
	testLeaseDuration       = 200 * time.Millisecond
	testRenewalInterval     = 30 * time.Millisecond
	testAcquisitionInterval = 25 * time.Millisecond
)

func testModels(t *testing.T) *Models {
	t.Helper()
	db := testDB(t)
	// Use test name as prefix to isolate tests from each other
	m, err := NewWithPrefix(otel.Tracer("test"), db.Database, t.Name())
	require.NoError(t, err)
	return m
}

func testLeaderElection(t *testing.T, m *Models, identity string) *LeaderElection {
	t.Helper()
	db := testDB(t)
	return m.NewLeaderElection(db, LeaderElectionConfig{
		Identity:            identity,
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
	})
}

func TestLeaderElection_SingleProcess(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	becameLeader := make(chan struct{}, 1)
	le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "single-process-test",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
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

	select {
	case <-becameLeader:
		// Success - became leader
	case <-time.After(2 * time.Second):
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

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
		le := le
		go func() {
			_ = le.Run(ctx)
		}()
	}

	// Wait for leader election to stabilize
	time.Sleep(3 * testLeaseDuration)

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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var leader1BecameLeader, leader2BecameLeader atomic.Bool
	var leader1LostLeadership atomic.Bool

	le1 := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "graceful-handoff-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
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
		OnBecameLeader: func(ctx context.Context) {
			leader2BecameLeader.Store(true)
		},
	})

	// Start first leader
	go func() {
		_ = le1.Run(ctx)
	}()

	// Wait for le1 to become leader
	require.Eventually(t, func() bool {
		return leader1BecameLeader.Load()
	}, 2*time.Second, 10*time.Millisecond, "le1 should become leader")

	require.True(t, le1.IsLeader())
	require.False(t, le2.IsLeader())

	// Start second election (should not become leader yet)
	go func() {
		_ = le2.Run(ctx)
	}()

	time.Sleep(2 * testLeaseDuration)
	require.False(t, le2.IsLeader(), "le2 should not be leader while le1 is active")

	// Stop le1 gracefully (releases lease)
	le1.Stop()
	time.Sleep(testAcquisitionInterval * 2) // Give time for stop to complete

	// le2 should take over
	require.Eventually(t, func() bool {
		return leader2BecameLeader.Load()
	}, 2*time.Second, 10*time.Millisecond, "le2 should become leader after le1 stops")

	require.True(t, le2.IsLeader())
	require.True(t, leader1LostLeadership.Load(), "le1 should have lost leadership")

	le2.Stop()
}

func TestLeaderElection_CrashRecovery(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var leader1BecameLeader, leader2BecameLeader atomic.Bool

	ctx1, cancel1 := context.WithCancel(ctx)

	le1 := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "crash-recovery-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
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
		OnBecameLeader: func(ctx context.Context) {
			leader2BecameLeader.Store(true)
		},
	})

	// Start both
	go func() {
		_ = le1.Run(ctx1)
	}()

	// Wait for le1 to become leader
	require.Eventually(t, func() bool {
		return leader1BecameLeader.Load()
	}, 2*time.Second, 10*time.Millisecond, "le1 should become leader")

	// Start le2
	go func() {
		_ = le2.Run(ctx)
	}()

	time.Sleep(2 * testLeaseDuration)
	require.True(t, le1.IsLeader())
	require.False(t, le2.IsLeader())

	// Simulate crash - cancel context without graceful shutdown
	// This means le1 won't release its lease
	cancel1()

	// le2 should take over after lease expires
	require.Eventually(t, func() bool {
		return leader2BecameLeader.Load()
	}, 2*time.Second, 10*time.Millisecond, "le2 should become leader after le1 crashes")

	require.True(t, le2.IsLeader())

	le2.Stop()
}

func TestLeaderElection_NoDuplicateLeaders(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const numProcesses = 10
	var elections []*LeaderElection

	for i := range numProcesses {
		le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
			Identity:            fmt.Sprintf("no-dup-%d", i),
			Logger:              slog.Default(),
			LeaseDuration:       testLeaseDuration,
			RenewalInterval:     testRenewalInterval,
			AcquisitionInterval: testAcquisitionInterval,
		})
		elections = append(elections, le)
	}

	// Start all elections with slight stagger
	for i, le := range elections {
		le := le
		go func() {
			time.Sleep(time.Duration(i) * 5 * time.Millisecond)
			_ = le.Run(ctx)
		}()
	}

	// Wait for election to stabilize
	time.Sleep(2 * testLeaseDuration)

	// Run for a while, periodically checking leader count via IsLeader().
	// This tests the core invariant: at any sampled point in time, at most one
	// process should report being the leader. Brief overlaps during transitions
	// are possible with lease-based election but should not be visible in
	// periodic sampling.
	for i := range 20 {
		time.Sleep(testLeaseDuration / 2)

		leaders := 0
		for _, le := range elections {
			if le.IsLeader() {
				leaders++
			}
		}
		require.LessOrEqual(t, leaders, 1, "should never have more than 1 leader (iteration %d)", i)
	}

	// Stop all
	for _, le := range elections {
		le.Stop()
	}
}

func TestLeaderElection_LeaseRenewal(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var becameLeader atomic.Bool
	var lostLeadership atomic.Bool

	le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "lease-renewal-test",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
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
	}, 2*time.Second, 10*time.Millisecond)

	// Run for several lease durations - should maintain leadership.
	// Check periodically that we're still leader and haven't lost leadership.
	for i := range 10 {
		time.Sleep(testLeaseDuration / 2)
		require.True(t, le.IsLeader(), "should still be leader at check %d", i)
		require.False(t, lostLeadership.Load(), "should not have lost leadership at check %d", i)
	}

	le.Stop()
}

func TestLeaderElection_RapidStartStop(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx := context.Background()

	// Rapidly start and stop elections to stress test
	for i := range 10 {
		le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
			Identity:            fmt.Sprintf("rapid-%d", i),
			Logger:              slog.Default(),
			LeaseDuration:       testLeaseDuration,
			RenewalInterval:     testRenewalInterval,
			AcquisitionInterval: testAcquisitionInterval,
		})

		runCtx, runCancel := context.WithCancel(ctx)
		go func() {
			_ = le.Run(runCtx)
		}()

		time.Sleep(testLeaseDuration / 2)
		le.Stop()
		runCancel()
		time.Sleep(testAcquisitionInterval)
	}

	// Final election should be able to acquire leadership
	var becameLeader atomic.Bool
	finalCtx, finalCancel := context.WithTimeout(ctx, 5*time.Second)
	defer finalCancel()

	le := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "rapid-final",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		OnBecameLeader: func(ctx context.Context) {
			becameLeader.Store(true)
		},
	})

	go func() {
		_ = le.Run(finalCtx)
	}()

	require.Eventually(t, func() bool {
		return becameLeader.Load()
	}, 2*time.Second, 10*time.Millisecond, "final election should become leader")

	le.Stop()
}

func TestLeaderElection_GetFirehoseLeader_NoLeader(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx := context.Background()

	le := testLeaderElection(t, m, "get-leader-test")

	// Should be nil when no leader exists (fresh directory per test)
	leader, err := le.GetFirehoseLeader(ctx)
	require.NoError(t, err)
	require.Nil(t, leader, "should return nil when no leader exists")
}

func TestLeaderElection_TryAcquireLease_AlreadyLeader(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx := context.Background()

	le := testLeaderElection(t, m, "already-leader-test")

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

	le := testLeaderElection(t, m, "renew-not-leader-test")

	// Try to renew without being leader
	err := le.RenewLease(ctx)
	require.ErrorIs(t, err, ErrNotLeader)
}

func TestLeaderElection_ReleaseLease_NotLeader(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx := context.Background()

	le := testLeaderElection(t, m, "release-not-leader-test")

	// Try to release without being leader - should not error
	err := le.ReleaseLease(ctx)
	require.NoError(t, err)
}

func TestLeaderElection_DoubleStop(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	le := testLeaderElection(t, m, "double-stop-test")

	go func() {
		_ = le.Run(ctx)
	}()

	time.Sleep(testLeaseDuration)

	// Double stop should not panic
	le.Stop()
	le.Stop()
}

func TestLeaderElection_LeaderStealing(t *testing.T) {
	t.Parallel()

	m := testModels(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var leader1Acquired, leader2Acquired atomic.Int32

	le1 := m.NewLeaderElection(testDB(t), LeaderElectionConfig{
		Identity:            "stealer-1",
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
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
		OnBecameLeader: func(ctx context.Context) {
			leader2Acquired.Add(1)
		},
	})

	// Start both at nearly the same time
	go func() { _ = le1.Run(ctx) }()
	go func() { _ = le2.Run(ctx) }()

	// Wait for election to stabilize
	time.Sleep(3 * testLeaseDuration)

	// Exactly one should be leader
	require.True(t, (le1.IsLeader() || le2.IsLeader()), "one should be leader")
	require.False(t, (le1.IsLeader() && le2.IsLeader()), "both should not be leader")

	// Total acquisitions should be 1
	totalAcquisitions := leader1Acquired.Load() + leader2Acquired.Load()
	require.Equal(t, int32(1), totalAcquisitions, "exactly one acquisition should have happened")

	le1.Stop()
	le2.Stop()
}
