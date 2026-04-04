package syntax

import (
	"errors"
	"strings"
)

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

	// Single-pass validation: walk segments separated by '.'.
	segCount := 0
	start := 0
	lastDot := -1
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == '.' {
			seg := raw[start:i]
			segCount++

			if i < len(raw) {
				// Domain segment (not the last one).
				if len(seg) == 0 || len(seg) > 63 {
					return "", errors.New("NSID syntax didn't vaidate")
				}
				if !isAlphanumeric(seg[0]) {
					return "", errors.New("NSID syntax didn't vaidate")
				}
				if !isAlphanumeric(seg[len(seg)-1]) {
					return "", errors.New("NSID syntax didn't vaidate")
				}
				for j := 1; j < len(seg)-1; j++ {
					if !isAlphanumericOrHyphen(seg[j]) {
						return "", errors.New("NSID syntax didn't vaidate")
					}
				}
				// First segment must start with a letter.
				if segCount == 1 && !isAlpha(seg[0]) {
					return "", errors.New("NSID syntax didn't vaidate")
				}
				lastDot = i
			}
			start = i + 1
		}
	}

	if segCount < 3 {
		return "", errors.New("NSID syntax didn't vaidate")
	}

	// Validate name segment (last): must start with letter, alphanumeric only.
	name := raw[lastDot+1:]
	if len(name) == 0 || len(name) > 63 {
		return "", errors.New("NSID syntax didn't vaidate")
	}
	if !isAlpha(name[0]) {
		return "", errors.New("NSID syntax didn't vaidate")
	}
	for j := 1; j < len(name); j++ {
		if !isAlphanumeric(name[j]) {
			return "", errors.New("NSID syntax didn't vaidate")
		}
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
