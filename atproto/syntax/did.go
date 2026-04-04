package syntax

import (
	"errors"
	"strings"
)

// Represents a syntaxtually valid DID identifier, as would pass Lexicon syntax validation.
//
// Always use [ParseDID] instead of wrapping strings directly, especially when working with input.
//
// Syntax specification: https://atproto.com/specs/did
type DID string

func isAlphanumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isLowerAlpha(c byte) bool {
	return c >= 'a' && c <= 'z'
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isAlphanumericOrHyphen(c byte) bool {
	return isAlphanumeric(c) || c == '-'
}

func isDIDIdentChar(c byte) bool {
	return isAlphanumeric(c) || c == '.' || c == '_' || c == ':' || c == '%' || c == '-'
}

func ParseDID(raw string) (DID, error) {
	if raw == "" {
		return "", errors.New("expected DID, got empty string")
	}

	if len(raw) > 2*1024 {
		return "", errors.New("DID is too long (2048 chars max)")
	}

	// Must start with "did:".
	if len(raw) < 5 || raw[0] != 'd' || raw[1] != 'i' || raw[2] != 'd' || raw[3] != ':' {
		return "", errors.New("DID syntax didn't vaidate")
	}

	// Method segment: lowercase alpha only, terminated by ':'.
	i := 4
	for i < len(raw) && raw[i] != ':' {
		if !isLowerAlpha(raw[i]) {
			return "", errors.New("DID syntax didn't vaidate")
		}
		i++
	}
	if i == 4 || i >= len(raw) {
		return "", errors.New("DID syntax didn't vaidate")
	}

	// Skip ':' after method.
	i++
	if i >= len(raw) {
		return "", errors.New("DID syntax didn't vaidate")
	}

	// Identifier: [a-zA-Z0-9._:%-]* ending with [a-zA-Z0-9._-].
	for j := i; j < len(raw); j++ {
		if !isDIDIdentChar(raw[j]) {
			return "", errors.New("DID syntax didn't vaidate")
		}
	}

	// Last char cannot be '%' or ':'.
	last := raw[len(raw)-1]
	if last == '%' || last == ':' {
		return "", errors.New("DID syntax didn't vaidate")
	}

	// Validate percent-encoding: every '%' must be followed by exactly two hex digits.
	for j := i; j < len(raw); j++ {
		if raw[j] == '%' {
			if j+2 >= len(raw) || !isHexDigit(raw[j+1]) || !isHexDigit(raw[j+2]) {
				return "", errors.New("DID syntax didn't vaidate")
			}
		}
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
	return AtIdentifier(d)
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
