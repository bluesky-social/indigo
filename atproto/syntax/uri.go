package syntax

import (
	"errors"
)

// Represents an arbitrary URI in string format, as would pass Lexicon syntax validation.
//
// The syntax is minimal and permissive, designed for fast verification and exact-string passthrough, not schema-specific parsing or validation. For example, will not validate AT-URI or DID strings.
//
// Always use [ParseURI] instead of wrapping strings directly, especially when working with network input.
type URI string

func ParseURI(raw string) (URI, error) {
	if raw == "" {
		return "", errors.New("expected URI, got empty string")
	}
	if len(raw) > 8192 {
		return "", errors.New("URI is too long (8192 chars max)")
	}

	// Scheme: starts with lowercase letter, then lowercase letters/digits/'+'/'.'/'-', then ':'.
	// Per RFC 3986 section 3.1: scheme = ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )
	if !isLowerAlpha(raw[0]) {
		return "", errors.New("URI syntax didn't vaidate")
	}

	i := 1
	for i < len(raw) && raw[i] != ':' {
		c := raw[i]
		if !isLowerAlpha(c) && !isDigit(c) && c != '+' && c != '.' && c != '-' {
			return "", errors.New("URI syntax didn't vaidate")
		}
		i++
		if i-1 > 80 {
			return "", errors.New("URI syntax didn't vaidate")
		}
	}
	if i >= len(raw) {
		return "", errors.New("URI syntax didn't vaidate")
	}

	// Skip ':'.
	i++
	if i >= len(raw) {
		return "", errors.New("URI syntax didn't vaidate")
	}

	// Body must be non-whitespace, non-control characters. This accepts non-ASCII
	// bytes (e.g. UTF-8 encoded internationalized URIs), which is slightly broader
	// than the previous [[:graph:]] regex that only matched Unicode graphic chars.
	for j := i; j < len(raw); j++ {
		c := raw[j]
		if c <= ' ' || c == 0x7F {
			return "", errors.New("URI syntax didn't vaidate")
		}
	}

	return URI(raw), nil
}

func (u URI) String() string {
	return string(u)
}

func (u URI) MarshalText() ([]byte, error) {
	return []byte(u.String()), nil
}

func (u *URI) UnmarshalText(text []byte) error {
	uri, err := ParseURI(string(text))
	if err != nil {
		return err
	}
	*u = uri
	return nil
}
