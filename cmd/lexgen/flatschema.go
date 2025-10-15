package main

import (
	"fmt"
	"log/slog"
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
	nsid, err := syntax.ParseNSID(sf.ID)
	if err != nil {
		return nil, err
	}

	fl := FlatLexicon{
		NSID:         nsid,
		Description:  sf.Description,
		ExternalRefs: map[string]bool{},
		SelfRefs:     map[string]bool{},
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

	// XXX: recurse
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
		// XXX
		/*
			if v.Output != nil && v.Output.Schema != nil {
				tp := slices.Clone(tpath)
				tp = append(tp, "output")
				if err := fl.flattenType(fd, tp, v.Output.Schema); err != nil {
					return err
				}
			}
			if v.Input != nil {
				props = v.Input
			}
			if v.Output != nil {
				props = v.Output
			}
		*/
	case lexicon.SchemaSubscription:
		// XXX:
		/*
			if v.Message != nil {
				props = v.Message
			}
		*/
	case lexicon.SchemaArray:
		// XXX: isCompoundDef
	case lexicon.SchemaObject:
		if err := fl.flattenObject(fd, tpath, &v); err != nil {
			return err
		}
	case lexicon.SchemaRef:
		if !strings.HasPrefix(v.Ref, "#") {
			fl.ExternalRefs[v.Ref] = true
		}
	case lexicon.SchemaUnion:
		for _, ref := range v.Refs {
			if !strings.HasPrefix(ref, "#") {
				fl.ExternalRefs[ref] = true
			}
		}
	case lexicon.SchemaString, lexicon.SchemaNull, lexicon.SchemaInteger, lexicon.SchemaBoolean, lexicon.SchemaUnknown:
		// pass, don't emit (?)
		return nil
	case lexicon.SchemaToken:
		// pass
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
			if err := fl.flattenType(fd, tp, &v.Items); err != nil {
				return err
			}
		case lexicon.SchemaObject:
			if err := fl.flattenType(fd, tp, &field); err != nil {
				return err
			}
		case lexicon.SchemaRef:
			slog.Info("reference", "ref", v.Ref)
			if strings.HasPrefix(v.Ref, "#") {
				fl.SelfRefs[v.Ref] = true
			} else {
				// TODO: normalize #main refs?
				fl.ExternalRefs[v.Ref] = true
			}
		case lexicon.SchemaUnion:
			for _, ref := range v.Refs {
				if strings.HasPrefix(ref, "#") {
					fl.SelfRefs[ref] = true
				} else {
					// TODO: normalize #main refs?
					fl.ExternalRefs[ref] = true
				}
			}
		default:
			return fmt.Errorf("unsupported field type for object flattening: %T", field.Inner)
		}
	}
	return nil
}
