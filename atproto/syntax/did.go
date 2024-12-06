package syntax

import (
	"errors"
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
var plcChars = ""

func isASCIIAlphaNum(c rune) bool {
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
		return true
	}
	return false
}

func ParseDID(raw string) (DID, error) {
	// fast-path for did:plc, avoiding regex
	if len(raw) == 32 && strings.HasPrefix(raw, "did:plc:") {
		// NOTE: this doesn't really check base32, just broader alphanumberic. might pass invalid PLC DIDs, but they still have overall valid DID syntax
		isPlc := true
		for _, c := range raw[8:32] {
			if !isASCIIAlphaNum(c) {
				isPlc = false
				break
			}
		}
		if isPlc {
			return DID(raw), nil
		}
	}
	if raw == "" {
		return "", errors.New("expected DID, got empty string")
	}
	if len(raw) > 2*1024 {
		return "", errors.New("DID is too long (2048 chars max)")
	}
	if !didRegex.MatchString(raw) {
		return "", errors.New("DID syntax didn't validate via regex")
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
