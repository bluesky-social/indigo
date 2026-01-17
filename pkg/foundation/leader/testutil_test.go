package leader

import (
	"log/slog"
	"testing"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb/directory"
	"github.com/bluesky-social/indigo/internal/testutil"
	"github.com/bluesky-social/indigo/pkg/clock"
	"github.com/bluesky-social/indigo/pkg/foundation"
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

func testDir(t *testing.T) directory.DirectorySubspace {
	t.Helper()
	db := testDB(t)
	dir, err := directory.CreateOrOpen(db.Database, []string{t.Name(), "leader"}, nil)
	require.NoError(t, err)
	return dir
}

func testLeaderElection(t *testing.T, identity string, clk clock.Clock) *LeaderElection {
	t.Helper()
	db := testDB(t)
	dir := testDir(t)
	return New(db, dir, LeaderElectionConfig{
		Identity:            identity,
		Logger:              slog.Default(),
		LeaseDuration:       testLeaseDuration,
		RenewalInterval:     testRenewalInterval,
		AcquisitionInterval: testAcquisitionInterval,
		Clock:               clk,
	})
}

// waitForWaiters waits until the mock clock has at least n waiters.
// Uses a short real-time sleep between checks for goroutine scheduling.
func waitForWaiters(t *testing.T, clk *clock.MockClock, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		return clk.WaiterCount() >= n
	}, 2*time.Second, 5*time.Millisecond, "timed out waiting for %d waiters, got %d", n, clk.WaiterCount())
}

// advanceAndWait advances the mock clock and waits briefly for goroutines to process
func advanceAndWait(clk *clock.MockClock, d time.Duration) {
	clk.Advance(d)
	time.Sleep(10 * time.Millisecond) // Let goroutines process
}
