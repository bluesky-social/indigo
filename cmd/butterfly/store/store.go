// Package store defines interfaces for persisting AT Protocol data
package store

import (
	"context"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
)

// Store defines the interface for data persistence in the butterfly sync engine
type Store interface {
	// Setup initializes the store
	Setup(ctx context.Context) error

	// Close tears down the store and releases resources
	Close() error

	// BackfillRepo resets a repo and re-ingests it from a remote stream
	// The implementation should handle context cancellation appropriately
	BackfillRepo(ctx context.Context, did string, stream *remote.RemoteStream) error

	// ActiveSync processes live update events from a remote stream
	// The implementation should handle context cancellation appropriately
	ActiveSync(ctx context.Context, stream *remote.RemoteStream) error

	// General-purpose KV storage

	// Get from general kv storage
	KvGet(namespace string, key string) (string, error)
	// Put to general kv storage
	KvPut(namespace string, key string, value string) error
	// Delete from general kv storage
	KvDel(namespace string, key string) error
}

// StoreType identifies the type of store
type StoreType string

const (
	StoreTypeStdout     StoreType = "stdout"
	StoreTypeDuckDB     StoreType = "duckdb"
	StoreTypeClickHouse StoreType = "clickhouse"
	StoreTypeTarFiles   StoreType = "tarfiles"
	StoreTypePebble     StoreType = "pebble"
)
