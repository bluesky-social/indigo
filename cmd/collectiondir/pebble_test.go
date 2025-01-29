package main

import (
	"context"
	"encoding/csv"
	"log/slog"
	"strings"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/vfs"
	"github.com/stretchr/testify/assert"
)

type debugWriter struct {
	t *testing.T
}

func (w *debugWriter) Write(p []byte) (n int, err error) {
	w.t.Helper()
	w.t.Log(string(p))
	return len(p), nil
}

// make a new pebble that writes to memory and logs to test.Log
func newMem(t *testing.T) *PebbleCollectionDirectory {
	memfs := vfs.NewMem()
	db, err := pebble.Open("wat", &pebble.Options{
		FS: memfs,
	})
	if err != nil {
		panic(err)
	}

	log := slog.New(slog.NewTextHandler(&debugWriter{t: t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	pcd := &PebbleCollectionDirectory{
		db:              db,
		collections:     make(map[string]uint32),
		collectionNames: make(map[uint32]string),
		log:             log,
	}
	if pcd.log == nil {
		pcd.log = slog.Default()
	}
	return pcd
}

// did, collection
const testDataCsv = `alice,post
alice,like
bob,post
bob,other
carol,post
eve,post
eve,like
eve,other`

func TestPebbleCollectionDirectory(t *testing.T) {
	assert := assert.New(t)

	pcd := newMem(t)
	defer func() {
		err := pcd.Close()
		if err != nil {
			t.Error(err)
		}
	}()

	rows, err := csv.NewReader(strings.NewReader(testDataCsv)).ReadAll()
	assert.NoError(err)
	for _, row := range rows {
		err := pcd.MaybeSetCollection(row[0], row[1])
		assert.NoError(err)
	}
	stats, err := pcd.GetCollectionStats()
	assert.NoError(err)
	t.Log(stats)
	assert.Equal(uint64(4), stats.CollectionCounts["post"])
	assert.Equal(uint64(2), stats.CollectionCounts["like"])
	assert.Equal(uint64(2), stats.CollectionCounts["other"])

	t.Log(pcd.collections)

	wat, nextCursor, err := pcd.ReadCollection(context.Background(), "post", "", 1000)
	assert.NoError(err)
	assert.Equal("", nextCursor)
	for _, row := range wat {
		assert.Equal("post", row.Collection)
	}
	assert.Equal(4, len(wat))

	wat, nextCursor, err = pcd.ReadCollection(context.Background(), "like", "", 1000)
	assert.NoError(err)
	assert.Equal("", nextCursor)
	for _, row := range wat {
		assert.Equal("like", row.Collection)
	}
	assert.Equal(2, len(wat))

	wat, nextCursor, err = pcd.ReadCollection(context.Background(), "other", "", 1000)
	assert.NoError(err)
	assert.Equal("", nextCursor)
	for _, row := range wat {
		assert.Equal("other", row.Collection)
	}
	assert.Equal(2, len(wat))
}
