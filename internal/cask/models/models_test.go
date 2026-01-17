package models

import (
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

var (
	setupOnce sync.Once

	// Should be retrieved via `testDB(t)`; don't use this directly
	testingDB *foundation.DB
)

func testDB(t *testing.T) *foundation.DB {
	tracer := otel.Tracer("test")

	var err error
	setupOnce.Do(func() {
		testingDB, err = foundation.New(
			t.Context(),
			&foundation.Config{
				Tracer:          tracer,
				ClusterFilePath: "../../../foundation.cluster",
				APIVersion:      730,
				RetryLimit:      100,
			},
		)
	})
	require.NoError(t, err)
	require.NotNil(t, testingDB)

	return testingDB
}
