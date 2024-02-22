package syntax

import (
	"fmt"
	"regexp"
)

// Colons are not allowed in record keys, but for now we're allowing them in the regex
var recordKeyRegex = regexp.MustCompile(`^[a-zA-Z0-9_:~.-]{1,512}$`)

// String type which represents a syntaxtually valid RecordKey identifier, as could be included in an AT URI
//
// Always use [ParseRecordKey] instead of wrapping strings directly, especially when working with input.
//
// Syntax specification: https://atproto.com/specs/record-key
type RecordKey string

func ParseRecordKey(raw string) (RecordKey, error) {
	if len(raw) > 512 {
		return "", fmt.Errorf("recordkey is too long (512 chars max)")
	}
	if raw == "" || raw == "." || raw == ".." {
		return "", fmt.Errorf("recordkey can not be empty, '.', or '..'")
	}
	if !recordKeyRegex.MatchString(raw) {
		return "", fmt.Errorf("recordkey syntax didn't validate via regex")
	}
	return RecordKey(raw), nil
}

func (r RecordKey) String() string {
	return string(r)
}

func (r RecordKey) MarshalText() ([]byte, error) {
	return []byte(r.String()), nil
}

func (r *RecordKey) UnmarshalText(text []byte) error {
	rkey, err := ParseRecordKey(string(text))
	if err != nil {
		return err
	}
	*r = rkey
	return nil
}
