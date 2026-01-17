package models

import (
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	"go.opentelemetry.io/otel/trace"
)

type Models struct {
	tracer trace.Tracer
	db     *fdb.Database
}

func New(tr trace.Tracer, db *fdb.Database) (*Models, error) {
	return NewWithPrefix(tr, db, "")
}

func NewWithPrefix(tr trace.Tracer, db *fdb.Database, prefix string) (*Models, error) {
	models := &Models{
		tracer: tr,
		db:     db,
	}

	return models, nil
}
