package auth

import (
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// ScopeAllowsSpaceRead reports whether an expanded OAuth scope set covers
// whole-space reads (and getDelegationToken). This is a selection hint, not an
// authorization decision: the PDS remains authoritative. Unknown or malformed
// scopes are ignored. Permission-set references must first be expanded by the
// authorization server; they do not themselves establish a read grant here.
func ScopeAllowsSpaceRead(scopes []string, accountDID syntax.DID, space syntax.SpaceRef) bool {
	if _, err := syntax.ParseSpaceRef(space.String()); err != nil {
		return false
	}
	for _, scope := range scopes {
		p, err := ParsePermissionString(scope)
		if err != nil || p.Resource != "space" {
			continue
		}
		if p.SpaceType != "*" && p.SpaceType != space.SpaceType().String() {
			continue
		}
		authority := p.Authority
		if authority == "" || authority == "self" {
			authority = accountDID.String()
		}
		if authority != "*" && authority != space.SpaceAuthority().String() {
			continue
		}
		if p.SpaceKey != "" && p.SpaceKey != "*" && p.SpaceKey != space.SpaceKey().String() {
			continue
		}
		// Read grants deliberately ignore collection and manage constraints.
		if len(p.Action) == 0 || slices.Contains(p.Action, "read") {
			return true
		}
	}
	return false
}
