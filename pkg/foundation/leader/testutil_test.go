package leader

import (
	"log/slog"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/internal/testutil"
	"github.com/bluesky-social/indigo/pkg/clock"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
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
