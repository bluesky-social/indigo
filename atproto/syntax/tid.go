package syntax

import (
	"fmt"
	"regexp"
)

// Represents a TID in string format, as would pass Lexicon syntax validation.
//
// Always use [ParseTID] instead of wrapping strings directly, especially when working with network input.
//
// Syntax specification: https://atproto.com/specs/record-key
type TID string

func ParseTID(raw string) (TID, error) {
	if len(raw) != 13 {
		return "", fmt.Errorf("TID is wrong length (expected 13 chars)")
	}
	var tidRegex = regexp.MustCompile(`^[234567abcdefghij][234567abcdefghijklmnopqrstuvwxyz]{12}$`)
	if !tidRegex.MatchString(raw) {
		return "", fmt.Errorf("TID syntax didn't validate via regex")
	}
	return TID(raw), nil
}

// TODO: additional helpers: to timestamp, from timestamp, from integer, etc

func (t TID) String() string {
	return string(t)
}

func (t TID) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

func (t *TID) UnmarshalText(text []byte) error {
	tid, err := ParseTID(string(text))
	if err != nil {
		return err
	}
	*t = tid
	return nil
}
