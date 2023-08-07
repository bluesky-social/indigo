package identifier

import (
	"fmt"
	"regexp"
	"strings"
)

// String type which represents a syntaxtually valid DID identifier, as would pass Lexicon syntax validation.
// Syntax specification: <https://atproto.com/specs/did>
type DID string

func NewDID(raw string) (DID, error) {
	if len(raw) > 2*1024 {
		return "", fmt.Errorf("DID is too long (2048 chars max)")
	}
	var didRegex = regexp.MustCompile("^did:[a-z]+:[a-zA-Z0-9._:%-]*[a-zA-Z0-9._-]$")
	if !didRegex.MatchString(raw) {
		return "", fmt.Errorf("DID syntax didn't validate via regex")
	}
	return DID(raw), nil
}

// Returns the "identifier" part of the DID
func (d DID) Method() string {
	parts := strings.SplitN(string(d), ":", 3)
	return parts[1]
}

// Returns the "identifier" part of the DID
func (d DID) Identifier() string {
	parts := strings.SplitN(string(d), ":", 3)
	return parts[2]
}
