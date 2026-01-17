package fdb

import (
	"fmt"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
)

type Config struct {
	ClusterFilePath string
	APIVersion      int

	RetryLimit int64
}

func New(cfg Config) (*fdb.Database, error) {
	if cfg.RetryLimit <= 0 {
		return nil, fmt.Errorf("invalid transaction retry limit")
	}

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

	// check that the connection can be established
	_, err = db.ReadTransact(func(tx fdb.ReadTransaction) (any, error) {
		return tx.Get(fdb.Key("PING")).Get()
	})
	if err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &db, nil
}
