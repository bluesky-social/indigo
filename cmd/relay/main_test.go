package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafeDatabaseURLForLog(t *testing.T) {
	assert.Equal(t, "sqlite://data/relay/relay.sqlite", safeDatabaseURLForLog("sqlite://data/relay/relay.sqlite"))
	assert.Equal(t, "postgres=<redacted>", safeDatabaseURLForLog("postgres=host=localhost user=relay password=secret dbname=relay"))
	assert.Equal(t, "postgres://relay:xxxxx@localhost:5432/relay?sslmode=disable", safeDatabaseURLForLog("postgres://relay:secret@localhost:5432/relay?sslmode=disable"))
	assert.Equal(t, "postgres://localhost:5432/relay?password=xxxxx&sslmode=disable", safeDatabaseURLForLog("postgres://localhost:5432/relay?sslmode=disable&password=secret"))
}
