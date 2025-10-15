package main

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Intermediate representation of a complete lexicon schema file, containing one or more definitions.
type FlatLexicon struct {
	NSID         syntax.NSID
	Description  *string
	ExternalRefs map[string]bool // NSID with optional ref
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
	nsid, err := syntax.ParseNSID(sf.ID)
	if err != nil {
		return nil, err
	}

	fl := FlatLexicon{
		NSID:         nsid,
		Description:  sf.Description,
		ExternalRefs: map[string]bool{},
		Defs:         map[string]FlatDef{},
		Types:        []FlatType{},
	}

	// iterate defs in sorted order; except "main" is always first if present
	defNames := []string{}
	hasMain := false
	for name := range sf.Defs {
		if name == "main" {
			hasMain = true
			continue
		}
		defNames = append(defNames, name)
	}
	sort.Strings(defNames)
	if hasMain {
		defNames = append([]string{"main"}, defNames...)
	}

	for _, name := range defNames {
		def := sf.Defs[name]
		if err := fl.flattenDef(name, &def); err != nil {
			return nil, err
		}
	}

	return &fl, nil
}

func (fl *FlatLexicon) flattenDef(name string, def *lexicon.SchemaDef) error {

	t, err := defType(def)
	if err != nil {
		return err
	}

	fd := FlatDef{
		Name: name,
		Type: t,
	}
	fl.Defs[name] = fd

	return fl.flattenType(&fd, []string{}, def)
}

func (fl *FlatLexicon) flattenType(fd *FlatDef, tpath []string, def *lexicon.SchemaDef) error {

	t, err := defType(def)
	if err != nil {
		return err
	}

	ft := FlatType{
		DefName: fd.Name,
		Path:    slices.Clone(tpath),
		Type:    t,
		Schema:  def,
	}

	switch v := def.Inner.(type) {
	case lexicon.SchemaRecord:
		if err := fl.flattenObject(fd, tpath, &v.Record); err != nil {
			return err
		}
	case lexicon.SchemaQuery:
		// v.Properties: only boolean, integer, string, or unknown are allowed, so recursion not really needed?
		if v.Output != nil && v.Output.Schema != nil {
			tp := slices.Clone(tpath)
			tp = append(tp, "output")
			if err := fl.flattenType(fd, tp, v.Output.Schema); err != nil {
				return err
			}
		}
	case lexicon.SchemaProcedure:
		// v.Properties: same as above
		if v.Output != nil && v.Output.Schema != nil {
			tp := slices.Clone(tpath)
			tp = append(tp, "output")
			if err := fl.flattenType(fd, tp, v.Output.Schema); err != nil {
				return err
			}
		}
		if v.Input != nil && v.Input.Schema != nil {
			tp := slices.Clone(tpath)
			tp = append(tp, "input")
			if err := fl.flattenType(fd, tp, v.Input.Schema); err != nil {
				return err
			}
		}
	case lexicon.SchemaSubscription:
		// v.Properties: same as above
		if v.Message != nil {
			switch vv := v.Message.Schema.Inner.(type) {
			case lexicon.SchemaUnion:
				for _, ref := range vv.Refs {
					if !strings.HasPrefix(ref, "#") {
						fl.ExternalRefs[strings.TrimSuffix(ref, "#main")] = true
					}
				}
			default:
				return fmt.Errorf("subscription with non-union message schema: %T", v.Message.Schema.Inner)
			}

		}
	case lexicon.SchemaObject:
		if err := fl.flattenObject(fd, tpath, &v); err != nil {
			return err
		}
	case lexicon.SchemaRef:
		if !strings.HasPrefix(v.Ref, "#") {
			fl.ExternalRefs[strings.TrimSuffix(v.Ref, "#main")] = true
		}
	case lexicon.SchemaUnion:
		for _, ref := range v.Refs {
			if !strings.HasPrefix(ref, "#") {
				fl.ExternalRefs[strings.TrimSuffix(ref, "#main")] = true
			}
		}
	case lexicon.SchemaArray:
		// flatten the inner item
		tp := slices.Clone(tpath)
		tp = append(tp, "elem")
		if err := fl.flattenType(fd, tpath, &v.Items); err != nil {
			return err
		}
		// don't emit the array itself
		return nil
	case lexicon.SchemaString, lexicon.SchemaNull, lexicon.SchemaInteger, lexicon.SchemaBoolean, lexicon.SchemaUnknown, lexicon.SchemaBytes:
		// don't emit
		// NOTE: might want to emit some string "knownValue" lists in the future?
		return nil
	case lexicon.SchemaCIDLink, lexicon.SchemaBlob:
		// don't emit
		return nil
	case lexicon.SchemaToken:
		// pass-through (emit)
	case lexicon.SchemaPermissionSet, lexicon.SchemaPermission:
		// pass-through (emit)
	default:
		return fmt.Errorf("unsupported def type for flattening (%s): %T", fd.Name, def.Inner)
	}

	fl.Types = append(fl.Types, ft)
	return nil
}

func (fl *FlatLexicon) flattenObject(fd *FlatDef, tpath []string, obj *lexicon.SchemaObject) error {
	for fname, field := range obj.Properties {
		tp := slices.Clone(tpath)
		tp = append(tp, fname)
		switch v := field.Inner.(type) {
		case lexicon.SchemaNull, lexicon.SchemaBoolean, lexicon.SchemaInteger, lexicon.SchemaString, lexicon.SchemaBytes:
			// no-op
		case lexicon.SchemaCIDLink, lexicon.SchemaBlob, lexicon.SchemaUnknown:
			// no-op, but maybe set a flag on def?
		case lexicon.SchemaArray:
			tp = append(tp, "elem")
			if err := fl.flattenType(fd, tp, &v.Items); err != nil {
				return err
			}
		case lexicon.SchemaObject:
			if err := fl.flattenType(fd, tp, &field); err != nil {
				return err
			}
		case lexicon.SchemaRef:
			if !strings.HasPrefix(v.Ref, "#") {
				// remove any #main suffix
				fl.ExternalRefs[strings.TrimSuffix(v.Ref, "#main")] = true
			}
		case lexicon.SchemaUnion:
			if err := fl.flattenType(fd, tp, &field); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported field type for object flattening: %T", field.Inner)
		}
	}
	return nil
}
