package auth

import (
	"fmt"
	"strings"
)

// High-level helper for parsing a space-delimited OAuth scope string in to a set of permissions.
//
// If the 'atproto' scope is not included, this will return an error. Otherwise invalid permission scope strings are simply ignored.
func ParseOAuthScope(scope string) ([]Permission, error) {

	foundAtproto := false
	perms := []Permission{}

	parts := strings.Split(scope, " ")
	for _, p := range parts {
		if p == "" {
			continue
		}
		if p == "atproto" {
			foundAtproto = true
			continue
		}
		perm, err := ParsePermissionString(p)
		if err != nil {
			continue
		}
		perms = append(perms, *perm)
	}
	if !foundAtproto {
		return nil, fmt.Errorf("required 'atproto' scope not found")
	}
	return perms, nil
}
