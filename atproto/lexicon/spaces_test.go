package lexicon_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/lexicon"
)

func validSpaceSchema() lexicon.SchemaSpace {
	return lexicon.SchemaSpace{
		Type: "space", Key: "any", Name: "Example Forum",
		NameLang:    map[string]string{"es": "Foro"},
		Collections: []string{"com.example.thread", "org.other.reaction"},
	}
}

func spaceSchemaFile(space lexicon.SchemaSpace) lexicon.SchemaFile {
	return lexicon.SchemaFile{Lexicon: 1, ID: "com.example.forum", Defs: map[string]lexicon.SchemaDef{"main": {Inner: space}}}
}

func TestSpaceSchemaDispatchCatalogAndRoundTrip(t *testing.T) {
	wire := []byte(`{"lexicon":1,"id":"com.example.forum","defs":{"main":{"type":"space","key":"any","name":"Example Forum","name:lang":{"es":"Foro"},"collections":["com.example.thread","org.other.reaction"]}}}`)
	var file lexicon.SchemaFile
	if err := json.Unmarshal(wire, &file); err != nil {
		t.Fatal(err)
	}
	main := file.Defs["main"]
	if !main.IsPrimary() || !main.IsDefinable() || main.IsConcrete() {
		t.Fatal("space schema definition classification is wrong")
	}
	if err := main.CheckSchema(); err != nil {
		t.Fatal(err)
	}
	catalog := lexicon.NewBaseCatalog()
	if err := catalog.AddSchemaFile(file); err != nil {
		t.Fatal(err)
	}
	resolved, err := catalog.Resolve("com.example.forum")
	if err != nil {
		t.Fatal(err)
	}
	space, ok := resolved.Def.(lexicon.SchemaSpace)
	if !ok || space.Name != "Example Forum" || len(space.Collections) != 2 || space.Collections[1] != "org.other.reaction" {
		t.Fatalf("space catalog dispatch failed: %+v", resolved)
	}
	encoded, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	var decoded lexicon.SchemaFile
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := lexicon.NewBaseCatalog().AddSchemaFile(decoded); err != nil {
		t.Fatalf("space schema JSON roundtrip failed: %v", err)
	}
}

func TestSpaceSchemaValidationBoundaries(t *testing.T) {
	for _, key := range []string{"tid", "nsid", "any", "literal:self", "literal:" + strings.Repeat("a", 512)} {
		space := validSpaceSchema()
		space.Key = key
		if err := space.CheckSchema(); err != nil {
			t.Fatalf("valid key %q rejected: %v", key, err)
		}
	}
	space := validSpaceSchema()
	space.Name = strings.Repeat("a", 64)
	space.Collections = []string{}
	if err := space.CheckSchema(); err != nil {
		t.Fatalf("empty declared collection set or boundary name rejected: %v", err)
	}
	invalid := []struct {
		name   string
		mutate func(*lexicon.SchemaSpace)
	}{
		{"wrong-type", func(s *lexicon.SchemaSpace) { s.Type = "record" }},
		{"missing-key", func(s *lexicon.SchemaSpace) { s.Key = "" }},
		{"invalid-key-type", func(s *lexicon.SchemaSpace) { s.Key = "slug" }},
		{"empty-literal-key", func(s *lexicon.SchemaSpace) { s.Key = "literal:" }},
		{"invalid-literal-key", func(s *lexicon.SchemaSpace) { s.Key = "literal:.." }},
		{"long-literal-key", func(s *lexicon.SchemaSpace) { s.Key = "literal:" + strings.Repeat("a", 513) }},
		{"missing-name", func(s *lexicon.SchemaSpace) { s.Name = "" }},
		{"long-name", func(s *lexicon.SchemaSpace) { s.Name = strings.Repeat("a", 65) }},
		{"invalid-language", func(s *lexicon.SchemaSpace) { s.NameLang = map[string]string{"bad_language": "Name"} }},
		{"empty-localized-name", func(s *lexicon.SchemaSpace) { s.NameLang = map[string]string{"es": ""} }},
		{"long-localized-name", func(s *lexicon.SchemaSpace) { s.NameLang = map[string]string{"es": strings.Repeat("a", 65)} }},
		{"missing-collections", func(s *lexicon.SchemaSpace) { s.Collections = nil }},
		{"wildcard-collection", func(s *lexicon.SchemaSpace) { s.Collections = []string{"*"} }},
		{"invalid-collection", func(s *lexicon.SchemaSpace) { s.Collections = []string{"invalid"} }},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			s := validSpaceSchema()
			tc.mutate(&s)
			if err := lexicon.NewBaseCatalog().AddSchemaFile(spaceSchemaFile(s)); err == nil {
				t.Fatal("invalid space schema entered catalog")
			}
		})
	}
	file := spaceSchemaFile(validSpaceSchema())
	file.Defs = map[string]lexicon.SchemaDef{"notMain": {Inner: validSpaceSchema()}}
	if err := lexicon.NewBaseCatalog().AddSchemaFile(file); err == nil {
		t.Fatal("space schema allowed outside main definition")
	}
}

func TestSpacePermissionSetParametersAndNamespace(t *testing.T) {
	for _, permission := range []lexicon.SchemaPermission{
		{Type: "permission", Resource: "space", SpaceType: "com.example.forum"},
		{Type: "permission", Resource: "space", SpaceType: "com.example.forum", Authority: "self", SpaceKey: "*", Action: []string{"read_self"}},
		{Type: "permission", Resource: "space", SpaceType: "com.example.child.forum", Authority: "*", Collection: []string{"*", "org.other.post"}, Action: []string{"read", "create", "update", "delete"}, Manage: []string{"create", "update", "delete"}},
		{Type: "permission", Resource: "space", SpaceType: "COM.EXAMPLE.forum", Authority: "did:plc:authority", SpaceKey: "default"},
	} {
		file := lexicon.SchemaFile{
			Lexicon: 1, ID: "com.example.authFull",
			Defs: map[string]lexicon.SchemaDef{"main": {Inner: lexicon.SchemaPermissionSet{Type: "permission-set", Permissions: []lexicon.SchemaPermission{permission}}}},
		}
		wire, err := json.Marshal(file)
		if err != nil {
			t.Fatal(err)
		}
		var decoded lexicon.SchemaFile
		if err := json.Unmarshal(wire, &decoded); err != nil {
			t.Fatal(err)
		}
		if err := lexicon.NewBaseCatalog().AddSchemaFile(decoded); err != nil {
			t.Fatalf("valid space permission-set rejected: %s: %v", wire, err)
		}
	}
	for _, spaceType := range []string{"com.example.forum", "com.example.other.forum", "com.examples.feed.forum", "org.other.forum"} {
		// This narrower set may name only com.example.feed.* resources.
		permission := lexicon.SchemaPermission{Type: "permission", Resource: "space", SpaceType: spaceType, Collection: []string{"*"}}
		if err := permission.CheckSchema(); err != nil {
			t.Fatal(err)
		}
		file := lexicon.SchemaFile{
			Lexicon: 1, ID: "com.example.feed.authFull",
			Defs: map[string]lexicon.SchemaDef{"main": {Inner: lexicon.SchemaPermissionSet{Type: "permission-set", Permissions: []lexicon.SchemaPermission{permission}}}},
		}
		if err := lexicon.NewBaseCatalog().AddSchemaFile(file); err == nil {
			t.Fatalf("space type outside namespace accepted: %s", spaceType)
		}
	}
	file := lexicon.SchemaFile{
		Lexicon: 1, ID: "com.example.feed.authFull",
		Defs: map[string]lexicon.SchemaDef{"main": {Inner: lexicon.SchemaPermissionSet{Type: "permission-set", Permissions: []lexicon.SchemaPermission{{Type: "permission", Resource: "space", SpaceType: "com.example.feed.forum", Collection: []string{"org.other.post"}}}}}},
	}
	if err := lexicon.NewBaseCatalog().AddSchemaFile(file); err != nil {
		t.Fatalf("foreign collections incorrectly subjected to namespace authority: %v", err)
	}
}

func TestSpacePermissionSetRejectsMalformedParameters(t *testing.T) {
	invalid := []struct {
		name   string
		mutate func(*lexicon.SchemaPermission)
	}{
		{"wildcard-type", func(p *lexicon.SchemaPermission) { p.SpaceType = "*" }},
		{"missing-type", func(p *lexicon.SchemaPermission) { p.SpaceType = "" }},
		{"invalid-type", func(p *lexicon.SchemaPermission) { p.SpaceType = "bad" }},
		{"invalid-authority", func(p *lexicon.SchemaPermission) { p.Authority = "author.example" }},
		{"authority-service", func(p *lexicon.SchemaPermission) { p.Authority = "did:plc:authority#service" }},
		{"invalid-key", func(p *lexicon.SchemaPermission) { p.SpaceKey = ".." }},
		{"long-key", func(p *lexicon.SchemaPermission) { p.SpaceKey = strings.Repeat("a", 513) }},
		{"invalid-collection", func(p *lexicon.SchemaPermission) { p.Collection = []string{"com.example.*"} }},
		{"invalid-action", func(p *lexicon.SchemaPermission) { p.Action = []string{"manage"} }},
		{"empty-action", func(p *lexicon.SchemaPermission) { p.Action = []string{""} }},
		{"invalid-management", func(p *lexicon.SchemaPermission) { p.Manage = []string{"read"} }},
		{"unexpected-rpc", func(p *lexicon.SchemaPermission) { p.LXM = []string{"com.example.query"} }},
		{"unexpected-audience", func(p *lexicon.SchemaPermission) { p.Audience = "*" }},
		{"unexpected-inherited-audience", func(p *lexicon.SchemaPermission) { p.InheritAud = true }},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			permission := lexicon.SchemaPermission{Type: "permission", Resource: "space", SpaceType: "com.example.forum"}
			tc.mutate(&permission)
			set := lexicon.SchemaPermissionSet{Type: "permission-set", Permissions: []lexicon.SchemaPermission{permission}}
			if err := set.CheckSchema(); err == nil {
				t.Fatal("malformed space permission accepted")
			}
		})
	}
}

func TestLexiconSpaceAddressFormats(t *testing.T) {
	ref := "at://did:plc:authority/space/com.example.forum/default"
	record := ref + "/did:plc:author/org.other.post/record"
	for _, format := range []string{"at-uri", "space-ref"} {
		schema := lexicon.SchemaString{Type: "string", Format: &format}
		if err := schema.CheckSchema(); err != nil {
			t.Fatalf("space format not recognized: %s: %v", format, err)
		}
		valid := []string{ref}
		invalid := []string{"https://example.com", ref + "/", record + "/extra", strings.Replace(record, "org.other.post", "invalid", 1)}
		if format == "at-uri" {
			valid = []string{ref, ref + "/did:plc:author", ref + "/did:plc:author/org.other.post", record, "at://author.example", "at://did:plc:author/org.other.post/record"}
		} else {
			invalid = append(invalid, ref+"/did:plc:author", ref+"/did:plc:author/org.other.post", record, "at://author.example/org.other.post/record")
		}
		for _, raw := range valid {
			if err := schema.Validate(raw, 0); err != nil {
				t.Fatalf("valid %s rejected: %q: %v", format, raw, err)
			}
		}
		for _, raw := range invalid {
			if err := schema.Validate(raw, 0); err == nil {
				t.Fatalf("malformed %s accepted: %q", format, raw)
			}
		}
	}
}
