package testutil

import (
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

var (
	setupOnce sync.Once
	testingDB *foundation.DB
)

func repoRoot() string {
	// thisFile is internal/testutil/foundation.go, so go up two levels
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

func TestDB(t *testing.T) *foundation.DB {
	tracer := otel.Tracer("test")

	var err error
	setupOnce.Do(func() {
		testingDB, err = foundation.New(
			t.Context(),
			"testing",
			&foundation.Config{
				Tracer:          tracer,
				ClusterFilePath: filepath.Join(repoRoot(), "foundation.cluster"),
				APIVersion:      730,
				RetryLimit:      100,
			},
		)
	})
	require.NoError(t, err)
	require.NotNil(t, testingDB)

	return testingDB
}
