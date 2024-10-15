package syntax

import (
	"errors"
	"regexp"
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
	var uriRegex = regexp.MustCompile(`^[a-z][a-z.-]{0,80}:[[:graph:]]+$`)
	if !uriRegex.MatchString(raw) {
		return "", errors.New("URI syntax didn't validate via regex")
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
