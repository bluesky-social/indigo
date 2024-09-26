package syntax

import (
	"errors"
	"regexp"
	"strings"
)

var nsidRegex = regexp.MustCompile(`^[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+(\.[a-zA-Z]([a-zA-Z]{0,61}[a-zA-Z])?)$`)

// String type which represents a syntaxtually valid Namespace Identifier (NSID), as would pass Lexicon syntax validation.
//
// Always use [ParseNSID] instead of wrapping strings directly, especially when working with input.
//
// Syntax specification: https://atproto.com/specs/nsid
type NSID string

func ParseNSID(raw string) (NSID, error) {
	if raw == "" {
		return "", errors.New("expected NSID, got empty string")
	}
	if len(raw) > 317 {
		return "", errors.New("NSID is too long (317 chars max)")
	}
	if !nsidRegex.MatchString(raw) {
		return "", errors.New("NSID syntax didn't validate via regex")
	}
	return NSID(raw), nil
}

// Authority domain name, in regular DNS order, not reversed order, normalized to lower-case.
func (n NSID) Authority() string {
	parts := strings.Split(string(n), ".")
	if len(parts) < 2 {
		// something has gone wrong (would not validate); return empty string instead
		return ""
	}
	// NSID must have at least two parts, verified by ParseNSID
	parts = parts[:len(parts)-1]
	// reverse
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.ToLower(strings.Join(parts, "."))
}

func (n NSID) Name() string {
	parts := strings.Split(string(n), ".")
	return parts[len(parts)-1]
}

func (n NSID) String() string {
	return string(n)
}

func (n NSID) Normalize() NSID {
	parts := strings.Split(string(n), ".")
	if len(parts) < 2 {
		// something has gone wrong (would not validate); just return the whole identifier
		return n
	}
	name := parts[len(parts)-1]
	prefix := strings.ToLower(strings.Join(parts[:len(parts)-1], "."))
	return NSID(prefix + "." + name)
}

func (n NSID) MarshalText() ([]byte, error) {
	return []byte(n.String()), nil
}

func (n *NSID) UnmarshalText(text []byte) error {
	nsid, err := ParseNSID(string(text))
	if err != nil {
		return err
	}
	*n = nsid
	return nil
}
