package syntax

import (
	"fmt"
	"regexp"
)

// String type which represents a syntaxtually valid RecordKey identifier, as could be included in an AT URI
//
// Always use [ParseRecordKey] instead of wrapping strings directly, especially when working with input.
//
// Syntax specification: https://atproto.com/specs/record-key
type RecordKey string

func ParseRecordKey(raw string) (RecordKey, error) {
	if len(raw) > 253 {
		return "", fmt.Errorf("recordkey is too long (512 chars max)")
	}
	if raw == "" || raw == "." || raw == ".." {
		return "", fmt.Errorf("recordkey can not be empty, '.', or '..'")
	}
	var recordkeyRegex = regexp.MustCompile(`^[a-zA-Z0-9_~.-]{1,512}$`)
	if !recordkeyRegex.MatchString(raw) {
		return "", fmt.Errorf("recordkey syntax didn't validate via regex")
	}
	return RecordKey(raw), nil
}

func (h RecordKey) String() string {
	return string(h)
}
