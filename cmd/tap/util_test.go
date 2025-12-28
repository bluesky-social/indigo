package main

import (
	"io"
	"log/slog"
	"testing"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}

func TestMatchesRecordContentFilters(t *testing.T) {
	logger := newTestLogger()

	const likeCollectionType = "app.bsky.feed.like"

	rec := map[string]any{
		"$type":     "app.bsky.feed.like",
		"createdAt": "2024-04-18T13:18:37.363Z",
		"subject": map[string]any{
			"cid": "bafyreicz7a43jdklahupwtxqnoqnaceyey4wbw3b726fmmqmox4fxi3lmy",
			"uri": "at://did:plc:o4s55v3tsfph6whswxccpsia/app.bsky.feed.generator/aaaixbb5liqbu",
		},
	}

	t.Run("empty filters -> true", func(t *testing.T) {
		if !matchesRecordContentFilters(logger, likeCollectionType, rec, []string{}) {
			t.Fatal("expected true for empty filters")
		}
	})

	t.Run("other collection filter ignored -> true", func(t *testing.T) {
		filters := []string{"com.example.other: $.subject.uri =~ \"nomatch\""}
		if !matchesRecordContentFilters(logger, likeCollectionType, rec, filters) {
			t.Fatal("expected true when other-collection filter is ignored")
		}
	})

	t.Run("non-raw match -> true", func(t *testing.T) {
		filters := []string{"app.bsky.feed.like: $.subject.uri =~ \"app.bsky.feed.generator\""}
		if !matchesRecordContentFilters(logger, likeCollectionType, rec, filters) {
			t.Fatal("expected true for matching non-raw regex filter")
		}
	})

	t.Run("raw regex match -> true", func(t *testing.T) {
		filters := []string{"app.bsky.feed.like: $.subject.uri =~ r\"app\\.bsky\\.feed\\.generator\""}
		if !matchesRecordContentFilters(logger, likeCollectionType, rec, filters) {
			t.Fatal("expected true for matching raw regex filter")
		}
	})

	t.Run("raw regex with wildcard match -> true", func(t *testing.T) {
		filters := []string{"app.bsky.feed.like: $.subject.uri =~ r\"app\\.bsky\\.feed\\.*\""}
		if !matchesRecordContentFilters(logger, likeCollectionType, rec, filters) {
			t.Fatal("expected true for matching raw regex filter with wildcard")
		}
	})

	t.Run("non r-string regex with wildcard match -> false", func(t *testing.T) {
		filters := []string{"app.bsky.feed.like: $.subject.uri =~ \"app.bsky.feed.*\""}
		if matchesRecordContentFilters(logger, likeCollectionType, rec, filters) {
			t.Fatal("expected false for non r-string regex filter with wildcard")
		}
	})

	t.Run("same collection but no match -> false", func(t *testing.T) {
		filters := []string{"app.bsky.feed.like: $.subject.uri =~ \"nomatch\""}
		if matchesRecordContentFilters(logger, likeCollectionType, rec, filters) {
			t.Fatal("expected false for same-collection non-matching filter")
		}
	})

	t.Run("invalid filter syntax -> true", func(t *testing.T) {
		filters := []string{"foo bar baz"}
		if !matchesRecordContentFilters(logger, likeCollectionType, rec, filters) {
			t.Fatal("expected true due to ignoring invalid filter syntax")
		}
	})
}
