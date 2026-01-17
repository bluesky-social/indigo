package models

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/directory"
	caskmetrics "github.com/bluesky-social/indigo/internal/cask/metrics"
	"github.com/bluesky-social/indigo/pkg/foundation"
	"github.com/bluesky-social/indigo/pkg/metrics"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Models struct {
	tracer trace.Tracer
	db     *fdb.Database

	firehoseLeader directory.DirectorySubspace
}

func New(tr trace.Tracer, db *fdb.Database) (*Models, error) {
	return NewWithPrefix(tr, db, "")
}

func NewWithPrefix(tr trace.Tracer, db *fdb.Database, prefix string) (*Models, error) {
	models := &Models{
		tracer: tr,
		db:     db,
	}

	dirPath := []string{"firehoseLeader"}
	if prefix != "" {
		dirPath = []string{prefix, "firehoseLeader"}
	}

	var err error
	models.firehoseLeader, err = directory.CreateOrOpen(models.db, dirPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create firehoseLeader directory: %w", err)
	}

	return models, nil
}

// Records prometheus metrics and OTEL span status for foundation queries.
// The returned `done` function must be called to end the span and record metrics.
func observe(ctx context.Context, db *foundation.DB, name string) (context.Context, trace.Span, func(error)) {
	ctx, span := db.Tracer.Start(ctx, name)
	start := time.Now()

	return ctx, span, func(err error) {
		defer span.End()

		var status string
		switch {
		case err == nil:
			status = metrics.StatusOK
			span.SetStatus(codes.Ok, "")
		case errors.Is(err, foundation.ErrNotFound):
			status = metrics.StatusNotFound
			span.SetStatus(codes.Ok, "not found")
		default:
			status = metrics.StatusError
			span.RecordError(err)
		}

		caskmetrics.QueryDuration.WithLabelValues(name, status).Observe(time.Since(start).Seconds())
	}
}
