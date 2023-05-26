package util

import (
	"fmt"
	"strings"

	lexutil "github.com/bluesky-social/indigo/lex/util"
)

type ParsedUri struct {
	Did        lexutil.FormatDID
	Collection lexutil.FormatNSID
	Rkey       string
}

func parseAtUri(uri string) (*ParsedUri, error) {
	if !strings.HasPrefix(uri, "at://") {
		return nil, fmt.Errorf("AT uris must be prefixed with 'at://'")
	}

	trimmed := strings.TrimPrefix(uri, "at://")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 3 {
		return nil, fmt.Errorf("AT uris must have three parts: did, collection, tid")
	}

	return &ParsedUri{
		Did:        lexutil.NewFormatDID(parts[0]),
		Collection: lexutil.NewFormatNSID(parts[1]),
		Rkey:       parts[2],
	}, nil
}
