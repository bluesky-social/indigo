package syntax

import (
	"errors"
	"fmt"
	"strings"
)

// Parses an atproto repo path string in to "collection" (NSID) and record key parts.
//
// Does not return partial success: either both collection and record key are complete (and error is nil), or both are empty string (and error is not nil)
func ParseRepoPath(raw string) (NSID, RecordKey, error) {
	parts := strings.SplitN(raw, "/", 3)
	if len(parts) != 2 {
		return "", "", errors.New("expected path to have two parts, separated by single slash")
	}
	nsid, err := ParseNSID(parts[0])
	if err != nil {
		return "", "", fmt.Errorf("collection part of path not a valid NSID: %w", err)
	}
	rkey, err := ParseRecordKey(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("record key part of path not valid: %w", err)
	}
	return nsid, rkey, nil
}
