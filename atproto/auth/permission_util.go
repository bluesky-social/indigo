package auth

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Parsed components of a generic AT scope string. This is for internal or low-level use; most code should use [ParsePermissionString] instead.
type GenericPermission struct {
	Resource   string     `json:"resource"`
	Positional string     `json:"positional"`
	Params     url.Values `json:"params"`
}

func ParseGenericScope(scope string) (*GenericPermission, error) {

	if !isASCII(scope) {
		return nil, ErrInvalidPermissionSyntax
	}

	front, query, _ := strings.Cut(scope, "?")
	resource, positional, _ := strings.Cut(front, ":")

	// XXX: more charset restrictions

	params, err := url.ParseQuery(query)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPermissionSyntax, err)
	}

	p := GenericPermission{
		Resource:   resource,
		Positional: positional,
		Params:     params,
	}
	return &p, nil
}

func (p *GenericPermission) ScopeString() string {
	scope := p.Resource
	if p.Positional != "" {
		scope = scope + ":" + p.Positional
	}
	if len(p.Params) > 0 {
		scope = scope + "?" + p.Params.Encode()
	}
	return scope
}

// TODO: replace with helper in syntax pkg
func validBlobAccept(accept string) bool {
	if accept == "*/*" {
		return true
	}
	parts := strings.SplitN(accept, "/", 3)
	if len(parts) != 2 {
		return false
	}
	if parts[0] == "*" || parts[0] == "" {
		return false
	}
	if parts[1] == "**" || parts[1] == "" {
		return false
	}
	return true
}

// TODO: replace with helper in syntax pkg
func validServiceRef(accept string) bool {
	parts := strings.SplitN(accept, "#", 3)
	if len(parts) != 2 {
		return false
	}
	_, err := syntax.ParseDID(parts[0])
	if err != nil {
		return false
	}
	if len(parts[1]) == 0 {
		return false
	}
	return true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}
	return true
}
