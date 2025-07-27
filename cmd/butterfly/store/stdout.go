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

	// KV storage (namespace -> key -> value)
	kvStore map[string]map[string]string
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
	// Initialize KV store
	s.kvStore = make(map[string]map[string]string)
	return nil
}

// Close outputs final statistics if in stats mode
func (s *StdoutStore) Close() error {
	if s.Mode == StdoutStoreModeStats {
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

	// Print KV store statistics
	if len(s.kvStore) > 0 {
		fmt.Println("\n=== KV Store Statistics ===")
		totalKeys := 0
		for namespace, nsMap := range s.kvStore {
			keyCount := len(nsMap)
			totalKeys += keyCount
			fmt.Printf("\nNamespace: %s\n", namespace)
			fmt.Printf("  Keys: %d\n", keyCount)

			// Show sample keys (up to 5)
			if keyCount > 0 {
				fmt.Println("  Sample keys:")
				shown := 0
				for key := range nsMap {
					fmt.Printf("    - %s\n", key)
					shown++
					if shown >= 5 {
						if keyCount > 5 {
							fmt.Printf("    ... and %d more\n", keyCount-5)
						}
						break
					}
				}
			}
		}
		fmt.Printf("\nTotal namespaces: %d\n", len(s.kvStore))
		fmt.Printf("Total keys: %d\n", totalKeys)
	}
}

// KvGet retrieves a value from general KV storage
func (s *StdoutStore) KvGet(namespace string, key string) (string, error) {
	if s.kvStore == nil {
		return "", fmt.Errorf("KV store not initialized - call Setup() first")
	}

	nsMap, exists := s.kvStore[namespace]
	if !exists {
		return "", fmt.Errorf("key not found in namespace %q", namespace)
	}

	value, exists := nsMap[key]
	if !exists {
		return "", fmt.Errorf("key %q not found in namespace %q", key, namespace)
	}

	return value, nil
}

// KvPut stores a value in general KV storage
func (s *StdoutStore) KvPut(namespace string, key string, value string) error {
	if s.kvStore == nil {
		return fmt.Errorf("KV store not initialized - call Setup() first")
	}

	// Create namespace map if it doesn't exist
	if _, exists := s.kvStore[namespace]; !exists {
		s.kvStore[namespace] = make(map[string]string)
	}

	// Store the value
	s.kvStore[namespace][key] = value

	// Log the operation in passthrough mode
	if s.Mode == StdoutStoreModePassthrough {
		fmt.Printf("KV PUT: namespace=%q key=%q value=%q\n", namespace, key, value)
	}

	return nil
}

// KvDel deletes a value from general KV storage
func (s *StdoutStore) KvDel(namespace string, key string) error {
	if s.kvStore == nil {
		return fmt.Errorf("KV store not initialized - call Setup() first")
	}

	nsMap, exists := s.kvStore[namespace]
	if !exists {
		// Key doesn't exist, but that's not an error for delete
		return nil
	}

	delete(nsMap, key)

	// Clean up empty namespace
	if len(nsMap) == 0 {
		delete(s.kvStore, namespace)
	}

	// Log the operation in passthrough mode
	if s.Mode == StdoutStoreModePassthrough {
		fmt.Printf("KV DEL: namespace=%q key=%q\n", namespace, key)
	}

	return nil
}
