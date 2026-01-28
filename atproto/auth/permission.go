package auth

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

var (
	ErrInvalidPermissionSyntax = errors.New("invalid permission syntax")
	ErrInvalidPermissionParams = errors.New("invalid permission parameters")
	ErrUnknownResource         = errors.New("unknown permission resource")
)

// Parsed components of an AT permission, as currently specified.
//
// This type is somewhat redundant with the "SchemaPermission" type in the indigo lexicon package, but it can represent all possible permissions, not just those found in permission sets.
type Permission struct {
	Type     string `json:"type,omitempty"`
	Resource string `json:"resource"`

	// common params (eg, identity, account)
	Accept     []string `json:"accept,omitempty"`
	Action     []string `json:"action,omitempty"`
	Attribute  string   `json:"attr,omitempty"`
	Audience   string   `json:"aud,omitempty"`
	InheritAud bool     `json:"inheritAud,omitempty"`
	Collection []string `json:"collection,omitempty"`
	Endpoint   []string `json:"lxm,omitempty"`
	NSID       string   `json:"nsid,omitempty"`
}

// Renders a permission as a permission scope string.
//
// If the permission contains information which only makes sense in the context of a permission-set (eg, the inheritAud flag), it will be silently dropped.
func (p *Permission) ScopeString() string {

	positional := ""
	params := make(url.Values)

	switch p.Resource {
	case "account":
		if p.Attribute != "" {
			positional = p.Attribute
		}
		if len(p.Action) != 0 {
			params["action"] = p.Action
		}
	case "blob":
		if len(p.Accept) == 1 {
			positional = p.Accept[0]
		} else if len(p.Accept) > 1 {
			params["accept"] = p.Accept
		}
	case "identity":
		if p.Attribute != "" {
			positional = p.Attribute
		}
	case "include":
		if p.NSID != "" {
			positional = p.NSID
		}
		if p.Audience != "" {
			params.Set("aud", p.Audience)
		}
	case "repo":
		if len(p.Collection) == 1 {
			positional = p.Collection[0]
		} else if len(p.Collection) > 1 {
			params["collection"] = p.Collection
		}
		if len(p.Action) != 0 {
			params["action"] = p.Action
		}
	case "rpc":
		if len(p.Endpoint) == 1 {
			positional = p.Endpoint[0]
		} else if len(p.Endpoint) > 1 {
			params["lxm"] = p.Endpoint
		}
		if p.Audience != "" {
			params.Set("aud", p.Audience)
		}
	default:
		return ""
	}

	scope := p.Resource
	if positional != "" {
		scope = scope + ":" + positional
	}
	if len(params) > 0 {
		scope = scope + "?" + params.Encode()
	}
	return scope
}

// Parses a permission scope string (as would be found as a component of an OAuth scope string) into a [Permission].
//
// This function is strict: it is case sensitive, verifies field syntax, and will throw an error on unknown parameters/fields. Note that calling code is usually supposed to simply skip any permission which cause such errors, not reject entire requests.
func ParsePermissionString(scope string) (*Permission, error) {
	g, err := ParseGenericScope(scope)
	if err != nil {
		return nil, err
	}

	p := Permission{
		Type:     "permission",
		Resource: g.Resource,
	}

	switch g.Resource {
	case "account":
		for k, _ := range g.Params {
			if !(k == "attr" || k == "action") {
				return nil, fmt.Errorf("%w: unsupported 'account' param: %s", ErrInvalidPermissionParams, k)
			}
		}
		if g.Params.Has("attr") {
			if g.Positional != "" || len(g.Params["attr"]) != 1 {
				return nil, ErrInvalidPermissionParams
			}
			p.Attribute = g.Params.Get("attr")
		}
		if g.Positional != "" {
			p.Attribute = g.Positional
		}
		if p.Attribute == "" {
			return nil, ErrInvalidPermissionParams
		}
		if p.Attribute != "" && p.Attribute != "email" && p.Attribute != "repo" {
			return nil, ErrInvalidPermissionParams
		}
		// NOTE: currently actions are `read` and `manage`, and it makes no sense to use more than one as `manage` implies `read`. This might change in the future, if more action are added.
		if len(g.Params["action"]) > 1 {
			return nil, ErrInvalidPermissionParams
		}
		p.Action = g.Params["action"]
		for _, act := range p.Action {
			if act != "read" && act != "manage" {
				return nil, ErrInvalidPermissionParams
			}
		}
	case "blob":
		for k, _ := range g.Params {
			if !(k == "accept") {
				return nil, fmt.Errorf("%w: unsupported 'blob' param: %s", ErrInvalidPermissionParams, k)
			}
		}
		if g.Params.Has("accept") {
			if g.Positional != "" {
				return nil, ErrInvalidPermissionParams
			}
			p.Accept = g.Params["accept"]
		}
		if g.Positional != "" {
			p.Accept = []string{g.Positional}
		}
		if len(p.Accept) == 0 {
			return nil, ErrInvalidPermissionParams
		}
		for _, acc := range p.Accept {
			if !validBlobAccept(acc) {
				return nil, ErrInvalidPermissionParams
			}
		}
	case "identity":
		for k, _ := range g.Params {
			if !(k == "attr") {
				return nil, fmt.Errorf("%w: unsupported 'identity' param: %s", ErrInvalidPermissionParams, k)
			}
		}
		if g.Params.Has("attr") {
			if g.Positional != "" || len(g.Params["attr"]) != 1 {
				return nil, ErrInvalidPermissionParams
			}
			p.Attribute = g.Params.Get("attr")
		}
		if g.Positional != "" {
			p.Attribute = g.Positional
		}
		if p.Attribute != "*" && p.Attribute != "handle" {
			return nil, ErrInvalidPermissionParams
		}
	case "include":
		for k, _ := range g.Params {
			if !(k == "nsid" || k == "aud") {
				return nil, fmt.Errorf("%w: unsupported 'include' param: %s", ErrInvalidPermissionParams, k)
			}
		}
		if g.Params.Has("nsid") {
			if g.Positional != "" || len(g.Params["nsid"]) != 1 {
				return nil, ErrInvalidPermissionParams
			}
			p.NSID = g.Params.Get("nsid")
		}
		if g.Positional != "" {
			p.NSID = g.Positional
		}
		_, err := syntax.ParseNSID(p.NSID)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidPermissionParams, err)
		}
		if g.Params.Has("aud") && (len(g.Params["aud"]) != 1 || g.Params.Get("aud") == "") {
			return nil, ErrInvalidPermissionParams
		}
		p.Audience = g.Params.Get("aud")
		if p.Audience != "" && p.Audience != "*" && !validServiceRef(p.Audience) {
			return nil, ErrInvalidPermissionParams
		}
		// possibly other params in the future...
	case "repo":
		for k, _ := range g.Params {
			if !(k == "collection" || k == "action") {
				return nil, fmt.Errorf("%w: unsupported 'repo' param: %s", ErrInvalidPermissionParams, k)
			}
		}
		if g.Params.Has("collection") {
			if g.Positional != "" {
				return nil, ErrInvalidPermissionParams
			}
			p.Collection = g.Params["collection"]
		}
		if g.Positional != "" {
			p.Collection = []string{g.Positional}
		}
		if len(p.Collection) == 0 {
			return nil, ErrInvalidPermissionParams
		}
		for _, coll := range p.Collection {
			if coll == "*" {
				continue
			}
			_, err := syntax.ParseNSID(coll)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidPermissionParams, err)
			}
		}
		p.Action = g.Params["action"]
		for _, act := range p.Action {
			if act != "create" && act != "update" && act != "delete" {
				return nil, ErrInvalidPermissionParams
			}
		}
	case "rpc":
		for k, _ := range g.Params {
			if !(k == "lxm" || k == "aud") {
				return nil, fmt.Errorf("%w: unsupported 'rpc' param: %s", ErrInvalidPermissionParams, k)
			}
		}
		if g.Params.Has("lxm") {
			if g.Positional != "" {
				return nil, ErrInvalidPermissionParams
			}
			p.Endpoint = g.Params["lxm"]
		}
		if g.Positional != "" {
			p.Endpoint = []string{g.Positional}
		}
		if len(p.Endpoint) == 0 {
			return nil, ErrInvalidPermissionParams
		}
		if len(g.Params["aud"]) != 1 {
			return nil, ErrInvalidPermissionParams
		}
		p.Audience = g.Params.Get("aud")
		for _, nsid := range p.Endpoint {
			if nsid == "*" {
				if p.Audience == "*" {
					return nil, ErrInvalidPermissionParams
				}
				continue
			}
			_, err := syntax.ParseNSID(nsid)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidPermissionParams, err)
			}
		}
		if p.Audience != "*" && !validServiceRef(p.Audience) {
			return nil, ErrInvalidPermissionParams
		}
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownResource, g.Resource)
	}
	return &p, nil
}
