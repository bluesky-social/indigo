package syntax

import (
	"fmt"
	"regexp"
	"strings"
)

// String type which represents a syntaxtually valid handle identifier, as would pass Lexicon syntax validation.
//
// Syntax specification: https://atproto.com/specs/handle
type Handle string

func ParseHandle(raw string) (Handle, error) {
	if len(raw) > 253 {
		return "", fmt.Errorf("Handle is too long (253 chars max)")
	}
	var handleRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\\.)+[a-zA-Z]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	if !handleRegex.MatchString(raw) {
		return "", fmt.Errorf("Handle syntax didn't validate via regex")
	}
	return Handle(raw), nil
}

// Some top-level domains (TLDs) are disallowed for registration across the atproto ecosystem. The *syntax* is valid, but these should never be considered acceptable handles for account registration or linking.
func (h *Handle) AllowedTLD() bool {
	switch h.TLD() {
	case "local",
		"arpa",
		"invalid",
		"localhost",
		"internal",
		"onion":
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
	return h.Normalize() == "handle.invalid"
}

func (h Handle) Normalize() Handle {
	return Handle(strings.ToLower(string(h)))
}
