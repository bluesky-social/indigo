package main

import (
	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Intermediate representation of a complete lexicon schema file, containing one or more definitions.
type FlatLexicon struct {
	NSID         syntax.NSID
	ExternalRefs map[string]bool // NSID with optional ref
	SelfRefs     map[string]bool // def names
	Defs         map[string]FlatDef
	Types        []FlatType
}

// Minimal context about an individual top-level schema definition: just the short name and schema type.
type FlatDef struct {
	Name string
	Type string
}

// An individual "type definition", which is a small unit of schema definition that corresponds to a named unit of generated code. For example, a struct or API endpoint.
type FlatType struct {
	// the short name of the schema def that this type is under
	DefName string
	Path    []string
	Type    string
	Schema  *lexicon.SchemaDef
}

func FlattenSchemaFile(sf *lexicon.SchemaFile) (*FlatLexicon, error) {
	// XXX
}

func (fl *FlatLexicon) flattenDef(name string, def *lexicon.SchemaDef) error {
	// XXX
}

func (fl *FlatLexicon) flattenType(fd *FlatDef, tpath []string, def *lexicon.SchemaDef) error {
	// XXX
}
