package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestPebbleThing(t *testing.T) {
	start := time.Now()
	ctx := context.Background()
	s := NewPebbleStore("/Users/paulfrazee/tmp/test-tarfiles/pebble.db/")
	s.Setup(ctx)
	records, _ := s.ListAllRecords(ctx, "did:plc:ragtjsm2j2vknwkz3zp4oxrd", 0)
	collections := 0
	recordCount := 0
	for _, sub := range records {
		collections++
		for range sub {
			recordCount++
		}
	}
	fmt.Printf("%d collections, %d records\n", collections, recordCount)
	s.Close()
	elapsed := time.Since(start)
	fmt.Printf("Completed in %s", elapsed)
}
