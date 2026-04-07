package syntax

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// special handle string constant indicating that handle resolution failed
	HandleInvalid = Handle("handle.invalid")
)

// String type which represents a syntaxtually valid handle identifier, as would pass Lexicon syntax validation.
//
// Always use [ParseHandle] instead of wrapping strings directly, especially when working with input.
//
// Syntax specification: https://atproto.com/specs/handle
type Handle string

func ParseHandle(raw string) (Handle, error) {
	if raw == "" {
		return "", errors.New("expected handle, got empty string")
	}
	if len(raw) > 253 {
		return "", errors.New("handle is too long (253 chars max)")
	}

	// Walk through dot-separated labels.
	labelCount := 0
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == '.' {
			label := raw[start:i]
			if len(label) == 0 || len(label) > 63 {
				return "", fmt.Errorf("handle syntax didn't validate: %s", raw)
			}
			if !isAlphanumeric(label[0]) {
				return "", fmt.Errorf("handle syntax didn't validate: %s", raw)
			}
			if !isAlphanumeric(label[len(label)-1]) {
				return "", fmt.Errorf("handle syntax didn't validate: %s", raw)
			}
			for j := 1; j < len(label)-1; j++ {
				if !isAlphanumericOrHyphen(label[j]) {
					return "", fmt.Errorf("handle syntax didn't validate: %s", raw)
				}
			}
			labelCount++
			start = i + 1
		}
	}

	if labelCount < 2 {
		return "", fmt.Errorf("handle syntax didn't validate: %s", raw)
	}

	// TLD (last label) must start with a letter.
	lastDot := strings.LastIndexByte(raw, '.')
	tld := raw[lastDot+1:]
	if !isAlpha(tld[0]) {
		return "", fmt.Errorf("handle syntax didn't validate: %s", raw)
	}

	return Handle(raw), nil
}

// Some top-level domains (TLDs) are disallowed for registration across the atproto ecosystem. The *syntax* is valid, but these should never be considered acceptable handles for account registration or linking.
//
// The reserved '.test' TLD is allowed, for testing and development. It is expected that '.test' domain resolution will fail in a real-world network.
func (h Handle) AllowedTLD() bool {
	switch h.TLD() {
	case "local",
		"arpa",
		"invalid",
		"localhost",
		"internal",
		"example",
		"onion",
		"alt":
		return false
	}
	return true
}

func (h Handle) TLD() string {
	parts := strings.Split(string(h.Normalize()), ".")
	return parts[len(parts)-1]
}

// Is this the special "handle.invalid" handle?
func (h Handle) IsInvalidHandle() bool {
	return h.Normalize() == HandleInvalid
}

func (h Handle) Normalize() Handle {
	return Handle(strings.ToLower(string(h)))
}

func (h Handle) AtIdentifier() AtIdentifier {
	return AtIdentifier(h)
}

func (h Handle) String() string {
	return string(h)
}

func (h Handle) MarshalText() ([]byte, error) {
	return []byte(h.String()), nil
}

func (h *Handle) UnmarshalText(text []byte) error {
	handle, err := ParseHandle(string(text))
	if err != nil {
		return err
	}
	*h = handle
	return nil
}
