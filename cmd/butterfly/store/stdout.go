// Package store provides a stdout implementation of the Store interface
package store

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/cmd/butterfly/remote"
)

// Output modes for StdoutStore
const (
	StdoutStoreModePassthrough = iota
	StdoutStoreModeStats
)

// StdoutStore implements Store by writing to stdout
type StdoutStore struct {
	Mode int

	// Stats tracking
	stats map[string]*repoStats
}

type repoStats struct {
	numRecords  int
	numCommits  int
	numErrors   int
	collections map[string]int
}

// Setup initializes the store
func (s *StdoutStore) Setup(ctx context.Context) error {
	if s.Mode == StdoutStoreModeStats {
		s.stats = make(map[string]*repoStats)
	}
	return nil
}

// Close outputs final statistics if in stats mode
func (s *StdoutStore) Close() error {
	if s.Mode == StdoutStoreModeStats && len(s.stats) > 0 {
		s.printStats()
	}
	return nil
}

// BackfillRepo resets a repo and re-ingests it from a remote stream
func (s *StdoutStore) BackfillRepo(ctx context.Context, did string, stream *remote.RemoteStream) error {
	return s.ActiveSync(ctx, stream)
}

// ActiveSync processes live update events from a remote stream
func (s *StdoutStore) ActiveSync(ctx context.Context, stream *remote.RemoteStream) error {
	for event := range stream.Ch {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch s.Mode {
		case StdoutStoreModePassthrough:
			fmt.Printf("%+v\n", event)
		case StdoutStoreModeStats:
			s.updateStats(event)
		}
	}
	return nil
}

func (s *StdoutStore) updateStats(event remote.StreamEvent) {
	stats, exists := s.stats[event.Did]
	if !exists {
		stats = &repoStats{
			collections: make(map[string]int),
		}
		s.stats[event.Did] = stats
	}

	switch event.Kind {
	case remote.EventKindCommit:
		stats.numCommits++
		if event.Commit != nil {
			stats.numRecords++
			stats.collections[event.Commit.Collection]++
		}
	case remote.EventKindError:
		stats.numErrors++
	}
}

func (s *StdoutStore) printStats() {
	fmt.Println("\n=== Repository Statistics ===")
	for did, stats := range s.stats {
		fmt.Printf("\nRepo: %s\n", did)
		fmt.Printf("  Records: %d\n", stats.numRecords)
		fmt.Printf("  Commits: %d\n", stats.numCommits)
		if stats.numErrors > 0 {
			fmt.Printf("  Errors: %d\n", stats.numErrors)
		}

		if len(stats.collections) > 0 {
			fmt.Println("  Collections:")
			for col, count := range stats.collections {
				fmt.Printf("    %s: %d\n", col, count)
			}
		}
	}
}

// KvGet retrieves a value from general KV storage (not yet implemented)
func (s *StdoutStore) KvGet(namespace string, key string) (string, error) {
	return "", fmt.Errorf("KvGet not yet implemented for stdout store")
}

// KvPut stores a value in general KV storage (not yet implemented)
func (s *StdoutStore) KvPut(namespace string, key string, value string) error {
	return fmt.Errorf("KvPut not yet implemented for stdout store")
}

// KvDel deletes a value from general KV storage (not yet implemented)
func (s *StdoutStore) KvDel(namespace string, key string) error {
	return fmt.Errorf("KvDel not yet implemented for stdout store")
}
