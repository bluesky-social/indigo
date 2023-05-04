package util

import (
	"fmt"
	"strings"
)

type ParsedUri struct {
	Did        string
	Collection string
	Rkey       string
}

func ParseAtUri(uri string) (*ParsedUri, error) {
	if !strings.HasPrefix(uri, "at://") {
		return nil, fmt.Errorf("AT uris must be prefixed with 'at://'")
	}

	trimmed := strings.TrimPrefix(uri, "at://")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 3 {
		return nil, fmt.Errorf("AT uris must have three parts: did, collection, tid")
	}

	return &ParsedUri{
		Did:        parts[0],
		Collection: parts[1],
		Rkey:       parts[2],
	}, nil
}
