package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/lex"
)

func TestModernTargetsAndExternalCatalog(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "api")
	requested := filepath.Join(dir, "query.json")
	external := filepath.Join(dir, "defs.json")
	unmatched := filepath.Join(dir, "unmatched.json")
	writeModernSchema(t, requested, `{"lexicon":1,"id":"com.atproto.space.query","defs":{"main":{"type":"query","output":{"encoding":"application/json","schema":{"type":"object","properties":{"policy":{"type":"union","refs":["org.example.shared.defs#entry"]}}}}}}}`)
	writeModernSchema(t, external, `{"lexicon":1,"id":"org.example.shared.defs","defs":{"entry":{"type":"object","properties":{}}}}`)
	writeModernSchema(t, unmatched, `{"lexicon":1,"id":"com.atproto.repo.other","defs":{"main":{"type":"object","properties":{}}}}`)
	packages := []lex.Package{
		{GoPackage: "other", Prefix: "com.atproto", Import: "example.test/other"},
		{GoPackage: "atproto", Prefix: "com.atproto.space", Outdir: out, Import: "example.test/api"},
		{GoPackage: "atproto", Prefix: "org.example.shared", Import: "example.test/api"},
	}
	// Repeated paths and overlapping external input do not duplicate catalog definitions or output.
	if err := runModern([]string{requested, requested, unmatched}, []string{external, requested}, packages, false, "json-raw-message"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "spacequery.go" {
		t.Fatalf("unexpected generation targets: %v", entries)
	}
	if target := modernTarget("com.atproto.spaces.query", packages); target == nil || target.Prefix != "com.atproto" {
		t.Fatalf("namespace targeting ignored segment boundary: %+v", target)
	}
}

func TestModernTargetUsesLongestPrefix(t *testing.T) {
	packages := []lex.Package{
		{Prefix: "com.atproto"},
		{Prefix: "com.atproto.space"},
		{Prefix: "com.atproto.space.special"},
	}
	for _, test := range []struct{ id, prefix string }{
		{"com.atproto.space.special.query", "com.atproto.space.special"},
		{"com.atproto.space.query", "com.atproto.space"},
		{"com.atproto.repo.query", "com.atproto"},
	} {
		if target := modernTarget(test.id, packages); target == nil || target.Prefix != test.prefix {
			t.Fatalf("target for %s: %+v", test.id, target)
		}
	}
	if target := modernTarget("org.example.query", packages); target != nil {
		t.Fatalf("unmapped lexicon selected target %+v", target)
	}
}

func TestModernConfigurationErrors(t *testing.T) {
	valid := lex.Package{GoPackage: "atproto", Prefix: "com.atproto.space", Import: "example.test/api"}
	for _, test := range []struct {
		name     string
		packages []lex.Package
		unknown  string
	}{
		{"unknown representation", []lex.Package{valid}, "invalid"},
		{"missing mapping", []lex.Package{{GoPackage: "atproto", Prefix: valid.Prefix}}, "json-raw-message"},
		{"invalid package", []lex.Package{{GoPackage: "package", Prefix: valid.Prefix, Import: valid.Import}}, "json-raw-message"},
		{"duplicate prefix", []lex.Package{valid, valid}, "json-raw-message"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := runModern(nil, nil, test.packages, false, test.unknown); err == nil {
				t.Fatal("accepted invalid modern configuration")
			}
		})
	}
}

func writeModernSchema(t *testing.T, path, schema string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}
}
