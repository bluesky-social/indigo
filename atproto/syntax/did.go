package syntax

import (
	"fmt"
	"regexp"
	"strings"
)

// Represents a syntaxtually valid DID identifier, as would pass Lexicon syntax validation.
//
// Always use [ParseDID] instead of wrapping strings directly, especially when working with input.
//
// Syntax specification: https://atproto.com/specs/did
type DID string

var didRegex = regexp.MustCompile(`^did:[a-z]+:[a-zA-Z0-9._:%-]*[a-zA-Z0-9._-]$`)

func ParseDID(raw string) (DID, error) {
	if raw == "" {
		return "", fmt.Errorf("expected DID, got empty string")
	}
	if len(raw) > 2*1024 {
		return "", fmt.Errorf("DID is too long (2048 chars max)")
	}
	if !didRegex.MatchString(raw) {
		return "", fmt.Errorf("DID syntax didn't validate via regex")
	}
	return DID(raw), nil
}

// The "method" part of the DID, between the 'did:' prefix and the final identifier segment, normalized to lower-case.
func (d DID) Method() string {
	// syntax guarantees that there are at least 3 parts of split
	parts := strings.SplitN(string(d), ":", 3)
	if len(parts) < 2 {
		// this should be impossible; return empty to avoid out-of-bounds
		return ""
	}
	return strings.ToLower(parts[1])
}

// The final "identifier" segment of the DID
func (d DID) Identifier() string {
	// syntax guarantees that there are at least 3 parts of split
	parts := strings.SplitN(string(d), ":", 3)
	if len(parts) < 3 {
		// this should be impossible; return empty to avoid out-of-bounds
		return ""
	}
	return parts[2]
}

func (d DID) AtIdentifier() AtIdentifier {
	return AtIdentifier{Inner: d}
}

func (d DID) String() string {
	return string(d)
}

func (d DID) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

func (d *DID) UnmarshalText(text []byte) error {
	did, err := ParseDID(string(text))
	if err != nil {
		return err
	}
	*d = did
	return nil
}
