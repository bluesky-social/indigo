package syntax

import (
	"fmt"
	"regexp"
	"strings"
)

// Represents a syntaxtually valid DID identifier, as would pass Lexicon syntax validation.
//
// Syntax specification: https://atproto.com/specs/did
type DID string

func ParseDID(raw string) (DID, error) {
	if len(raw) > 2*1024 {
		return "", fmt.Errorf("DID is too long (2048 chars max)")
	}
	var didRegex = regexp.MustCompile(`^did:[a-z]+:[a-zA-Z0-9._:%-]*[a-zA-Z0-9._-]$`)
	if !didRegex.MatchString(raw) {
		return "", fmt.Errorf("DID syntax didn't validate via regex")
	}
	return DID(raw), nil
}

// The "method" part of the DID, between the 'did:' prefix and the final identifier segment, normalized to lower-case.
func (d DID) Method() string {
	// syntax guarantees that there are at least 3 parts of split
	parts := strings.SplitN(string(d), ":", 3)
	return strings.ToLower(parts[1])
}

// The final "identifier" segment of the DID
func (d DID) Identifier() string {
	// syntax guarantees that there are at least 3 parts of split
	parts := strings.SplitN(string(d), ":", 3)
	return parts[2]
}
