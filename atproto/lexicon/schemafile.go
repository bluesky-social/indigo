package lexicon

import (
	"fmt"
	"strings"
)

// Serialization helper type for top-level Lexicon schema JSON objects (files).
//
// Note that the [FinishParse] method should always be called after unmarshalling a SchemaFile from JSON.
type SchemaFile struct {
	Lexicon     int                  `json:"lexicon"` // must be 1
	ID          string               `json:"id"`
	Description *string              `json:"description,omitempty"`
	Defs        map[string]SchemaDef `json:"defs"`
}

// Helper method which should always be called after parsing a schema file (eg, from JSON).
//
// Does some very basic validation (eg, lexicon language version), and fills in
// internal references (for example full name of tokens).
func (sf *SchemaFile) FinishParse() error {
	if sf.Lexicon != 1 {
		return fmt.Errorf("unsupported lexicon language version: %d", sf.Lexicon)
	}
	base := sf.ID
	for frag, def := range sf.Defs {
		if len(frag) == 0 || strings.Contains(frag, "#") || strings.Contains(frag, ".") {
			// TODO: more validation here?
			return fmt.Errorf("schema name invalid: %s", frag)
		}
		name := base + "#" + frag
		switch s := def.Inner.(type) {
		case SchemaToken:
			// add fully-qualified name to token
			s.FullName = name
			def.Inner = s
		}
		def.setBase(base)
		sf.Defs[frag] = def
	}
	return nil
}

// Calls [SchemaDef.CheckSchema] recursively over all defs
func (sf *SchemaFile) CheckSchema() error {
	if sf.Lexicon != 1 {
		return fmt.Errorf("unsupported lexicon language version: %d", sf.Lexicon)
	}

	for frag, def := range sf.Defs {
		if len(frag) == 0 || strings.Contains(frag, "#") || strings.Contains(frag, ".") {
			// TODO: more validation here?
			return fmt.Errorf("schema name invalid: %s", frag)
		}
		// "A file can have at most one definition with one of the "primary" types. Primary types should always have the name main. It is possible for main to describe a non-primary type."
		switch def.Inner.(type) {
		case SchemaRecord, SchemaQuery, SchemaProcedure, SchemaSubscription, SchemaPermissionSet:
			if frag != "main" {
				return fmt.Errorf("record, query, procedure, and subscription types must be 'main', not: %s", frag)
			}
		}
		if err := def.CheckSchema(); err != nil {
			return err
		}
	}
	return nil
}
