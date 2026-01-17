package models

import (
	"fmt"

	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"github.com/apple/foundationdb/bindings/go/src/fdb/directory"
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

func (m *Models) FirehoseLeaderDir() directory.DirectorySubspace {
	return m.firehoseLeader
}
