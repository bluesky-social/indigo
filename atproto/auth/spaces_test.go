package auth_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth"
)

func TestSpacePermissionScopeRoundTrips(t *testing.T) {
	valid := []string{
		"space:com.example.forum",
		"space:*",
		"space:com.example.forum?authority=self",
		"space:com.example.forum?authority=*&skey=*",
		"space:com.example.forum?authority=did%3Aplc%3Aauthority&skey=default",
		"space:com.example.forum?action=read_self&authority=*",
		"space:com.example.forum?action=read&action=create&collection=org.other.post&collection=*&manage=update&manage=delete&skey=default",
		"space:com.example.forum?manage=create&skey=*",
		"space:com.example.forum?skey=" + strings.Repeat("a", 512),
		"space?spaceType=com.example.forum&authority=*",
	}
	for _, raw := range valid {
		t.Run(raw, func(t *testing.T) {
			permission, err := auth.ParsePermissionString(raw)
			if err != nil {
				t.Fatal(err)
			}
			if permission.Resource != "space" || permission.Type != "permission" {
				t.Fatalf("wrong permission discriminator: %+v", permission)
			}
			decoded, err := auth.ParsePermissionString(permission.ScopeString())
			if err != nil || !reflect.DeepEqual(permission, decoded) {
				t.Fatalf("scope roundtrip: %q, %v", permission.ScopeString(), err)
			}
			wire, err := json.Marshal(permission)
			if err != nil {
				t.Fatal(err)
			}
			var jsonPermission auth.Permission
			if err := json.Unmarshal(wire, &jsonPermission); err != nil || !reflect.DeepEqual(*permission, jsonPermission) {
				t.Fatalf("permission JSON roundtrip: %s, %v", wire, err)
			}
		})
	}
}

func TestSpacePermissionOmissionRetainsDynamicDefaults(t *testing.T) {
	permission, err := auth.ParsePermissionString("space:com.example.forum")
	if err != nil {
		t.Fatal(err)
	}
	// Parsing preserves omission instead of materializing policy defaults:
	// self authority, wildcard key, read/create/update/delete, no manage, and
	// collections resolved dynamically from the space declaration by consumers.
	if permission.Authority != "" || permission.SpaceKey != "" || permission.Action != nil || permission.Manage != nil || permission.Collection != nil || permission.ScopeString() != "space:com.example.forum" {
		t.Fatalf("parser replaced omitted defaults: %+v", permission)
	}
	wildcard, err := auth.ParsePermissionString("space:*")
	if err != nil || wildcard.Collection != nil {
		t.Fatalf("wildcard type manufactured write collections: %+v, %v", wildcard, err)
	}
	perms, err := auth.ParseOAuthScope("atproto space:com.example.forum space:bad space:*?authority=*&action=read")
	if err != nil || len(perms) != 2 || perms[0].SpaceType != "com.example.forum" || perms[1].SpaceType != "*" {
		t.Fatalf("OAuth scope integration: %+v, %v", perms, err)
	}
}

func TestSpacePermissionRejectsMalformedParameters(t *testing.T) {
	invalid := []string{
		"space", "space:", "space?spaceType=", "space:com.example", "space:com.example.*",
		"space:com.example.forum?spaceType=com.example.forum",
		"space?spaceType=com.example.forum&spaceType=com.example.other",
		"space:com.example.forum?authority=", "space:com.example.forum?authority=SELF",
		"space:com.example.forum?authority=author.example", "space:com.example.forum?authority=did:plc:authority%23service",
		"space:com.example.forum?authority=self&authority=*",
		"space:com.example.forum?skey=", "space:com.example.forum?skey=.", "space:com.example.forum?skey=..",
		"space:com.example.forum?skey=a%2Fb", "space:com.example.forum?skey=a&skey=b",
		"space:com.example.forum?skey=" + strings.Repeat("a", 513),
		"space:com.example.forum?collection=", "space:com.example.forum?collection=org.other.*",
		"space:com.example.forum?collection=not-an-nsid",
		"space:com.example.forum?action=", "space:com.example.forum?action=READ", "space:com.example.forum?action=manage",
		"space:com.example.forum?manage=", "space:com.example.forum?manage=read", "space:com.example.forum?manage=read_self",
		"space:com.example.forum?aud=*", "space:com.example.forum?lxm=com.example.query",
		"space:com.example.forum?unknown=value",
	}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			permission, err := auth.ParsePermissionString(raw)
			if err == nil || permission != nil || !errors.Is(err, auth.ErrInvalidPermissionParams) {
				t.Fatalf("malformed scope accepted or wrong error: %+v, %v", permission, err)
			}
		})
	}
}

func TestSpacePermissionEncodedPositionalType(t *testing.T) {
	permission, err := auth.ParsePermissionString("space:com%2Eexample%2Eforum")
	if err != nil || permission.SpaceType != "com.example.forum" || permission.ScopeString() != "space:com.example.forum" {
		t.Fatalf("encoded positional space type: %+v, %v", permission, err)
	}
	if _, err := auth.ParsePermissionString("space:com%XXexample.forum"); !errors.Is(err, auth.ErrInvalidPermissionSyntax) {
		t.Fatalf("invalid positional encoding accepted or wrong error: %v", err)
	}
}
