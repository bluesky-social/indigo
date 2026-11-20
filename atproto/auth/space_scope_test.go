package auth

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

func TestScopeAllowsSpaceReadBoundaries(t *testing.T) {
	space := syntax.SpaceRef("at://did:plc:authority/space/com.example.board/default")
	for _, tc := range []struct {
		name, scope string
		account     syntax.DID
		allowed     bool
	}{
		{"read", "space:com.example.board?authority=*&action=read", "did:plc:user", true},
		{"read_self is not delegation", "space:com.example.board?authority=*&action=read_self", "did:plc:user", false},
		{"default self", "space:com.example.board", "did:plc:authority", true},
		{"default excludes other authority", "space:com.example.board", "did:plc:user", false},
		{"explicit self", "space:com.example.board?authority=self", "did:plc:authority", true},
		{"explicit self excludes others", "space:com.example.board?authority=self", "did:plc:user", false},
		{"exact triple", "space:com.example.board?authority=did:plc:authority&skey=default&action=read", "did:plc:user", true},
		{"wrong key", "space:com.example.board?authority=*&skey=other&action=read", "did:plc:user", false},
		{"wrong type", "space:com.example.other?authority=*&action=read", "did:plc:user", false},
		{"wildcard type default reads", "space:*?authority=*", "did:plc:user", true},
		{"collection never limits reads", "space:com.example.board?authority=*&collection=com.other.record&action=read", "did:plc:user", true},
		{"manage is not read", "space:com.example.board?authority=*&action=read_self&manage=update", "did:plc:user", false},
		{"writes are not read", "space:com.example.board?authority=*&action=create", "did:plc:user", false},
		{"malformed ignored", "space:com.example.board?authority=*&action=read&unknown=bad", "did:plc:user", false},
		{"duplicate authority rejected", "space:com.example.board?authority=*&authority=self", "did:plc:user", false},
		{"unexpanded include ignored", "include:com.example.permissions", "did:plc:user", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScopeAllowsSpaceRead([]string{"atproto", "invalid", tc.scope}, tc.account, space); got != tc.allowed {
				t.Fatalf("allowed=%v, want %v", got, tc.allowed)
			}
		})
	}
	if ScopeAllowsSpaceRead([]string{"space:*?authority=*"}, "did:plc:user", "invalid") {
		t.Fatal("invalid space accepted")
	}
}
