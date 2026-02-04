package foundation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/directory"
	"github.com/apple/foundationdb/bindings/go/src/fdb/tuple"
	"github.com/bluesky-social/indigo/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
)

var (
	// ErrNotFound is returned when a requested item does not exist in the database
	ErrNotFound = errors.New("not found")
)

var (
	queryDuration *prometheus.HistogramVec
)

type Config struct {
	Tracer trace.Tracer

	APIVersion      int
	ClusterFilePath string
	RetryLimit      int64

	// Chunker configures how large values are split for storage.
	// If nil, default chunking settings are used.
	Chunker *ChunkerConfig
}

type DB struct {
	*fdb.Database

	Tracer  trace.Tracer
	Chunker *Chunker
}

func New(ctx context.Context, service string, cfg *Config) (*DB, error) {
	if cfg.RetryLimit <= 0 {
		return nil, fmt.Errorf("invalid transaction retry limit")
	}

	queryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:      "query_duration_secs",
			Namespace: service,
			Help:      "Duration histogram of FoundationDB queries in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 18), // 0.1ms to ~13s
		},
		[]string{"query", "status"},
	)

	if err := fdb.APIVersion(cfg.APIVersion); err != nil {
		return nil, fmt.Errorf("failed to set fdb client api version: %w", err)
	}

	db, err := fdb.OpenDatabase(cfg.ClusterFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize fdb client from cluster file %q: %w", cfg.ClusterFilePath, err)
	}

	if err := db.Options().SetTransactionTimeout(5000); err != nil { // milliseconds
		return nil, fmt.Errorf("failed to set fdb transaction timeout: %w", err)
	}

	if err := db.Options().SetTransactionRetryLimit(cfg.RetryLimit); err != nil {
		return nil, fmt.Errorf("failed to set fdb transaction retry limit: %w", err)
	}

	d := &DB{
		Database: &db,
		Tracer:   cfg.Tracer,
		Chunker:  NewChunker(cfg.Chunker),
	}

	// check that the connection can be established
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := d.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return d, nil
}

// Pings the database to ensure we have connectivity
func (db *DB) Ping(ctx context.Context) (err error) {
	_, err = ReadTransaction(db, func(tx fdb.ReadTransaction) ([]byte, error) {
		return tx.Get(fdb.Key("PING")).Get()
	})

	return
}

// Executes the anonymous function as a write transaction, then attempts to cast the return type
func Transaction[T any](db *DB, fn func(tx fdb.Transaction) (T, error)) (T, error) {
	var t T

	resI, err := db.Transact(func(tx fdb.Transaction) (any, error) {
		return fn(tx)
	})

	if err != nil {
		return t, err
	}

	// handle nil result (common when function only has side effects)
	if resI == nil {
		return t, nil
	}

	res, ok := resI.(T)
	if !ok {
		return t, fmt.Errorf("failed to cast transaction result %T to %T", resI, t)
	}

	return res, nil
}

// Executes the anonymous function as a read transaction, then attempts to cast the return type
func ReadTransaction[T any](db *DB, fn func(tx fdb.ReadTransaction) (T, error)) (T, error) {
	var t T

	resI, err := db.ReadTransact(func(tx fdb.ReadTransaction) (any, error) {
		return fn(tx)
	})

	if err != nil {
		return t, err
	}

	if resI == nil {
		return t, ErrNotFound
	}

	res, ok := resI.(T)
	if !ok {
		return t, fmt.Errorf("failed to cast read transaction result %T to %T", resI, t)
	}

	return res, nil
}

// Constructs the FDB key in the directory (if any) with the given list of arguments
func Pack(dir directory.DirectorySubspace, keys ...tuple.TupleElement) fdb.Key {
	tup := tuple.Tuple(keys)
	if dir == nil {
		return fdb.Key(tup.Pack())
	}
	return dir.Pack(tup)
}

// Executes the given anonymous function as a read transaction, then attempts to protobuf unmarshal
// the resulting `[]byte` in to the given `item`. Returns `ErrNotFound` if the item does not exist in the db.
func ReadProto(db *DB, item proto.Message, fn func(fdb.ReadTransaction) ([]byte, error)) error {
	buf, err := ReadTransaction(db, fn)
	if err != nil {
		return err
	}
	if len(buf) == 0 {
		return ErrNotFound
	}

	return proto.Unmarshal(buf, item)
}

// Observe records prometheus metrics and OTEL span status for foundation queries.
// The returned `done` function must be called to end the span and record metrics.
func Observe(ctx context.Context, db *DB, name string) (context.Context, trace.Span, func(error)) {
	ctx, span := db.Tracer.Start(ctx, name)
	start := time.Now()

	called := false
	return ctx, span, func(err error) {
		if called {
			return
		}
		called = true

		defer span.End()

		var status string
		switch {
		case err == nil:
			status = metrics.StatusOK
			span.SetStatus(codes.Ok, "")
		case errors.Is(err, ErrNotFound):
			status = metrics.StatusNotFound
			span.SetStatus(codes.Ok, "not found")
		default:
			status = metrics.StatusError
			span.RecordError(err)
		}

		queryDuration.WithLabelValues(name, status).Observe(time.Since(start).Seconds())
	}
}
