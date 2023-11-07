package lexicon

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// An aggregation of lexicon schemas, and methods for validating generic data against those schemas.
type Catalog struct {
	// TODO: not safe zero value; hide this field? seems aggressive
	Schemas map[string]Schema
}

func NewCatalog() Catalog {
	return Catalog{
		Schemas: make(map[string]Schema),
	}
}

type Schema struct {
	ID       string
	Revision *int
	Def      any
}

func (c *Catalog) Resolve(name string) (*Schema, error) {
	// default to #main if name doesn't have a fragment
	if !strings.Contains(name, "#") {
		name = name + "#main"
	}
	s, ok := c.Schemas[name]
	if !ok {
		return nil, fmt.Errorf("schema not found in catalog: %s", name)
	}
	return &s, nil
}

func (c *Catalog) AddSchemaFile(sf SchemaFile) error {
	base := sf.ID
	for frag, def := range sf.Defs {
		if len(frag) == 0 || strings.Contains(frag, "#") || strings.Contains(frag, ".") {
			// TODO: more validation here?
			return fmt.Errorf("schema name invalid: %s", frag)
		}
		name := base + "#" + frag
		if _, ok := c.Schemas[name]; ok {
			return fmt.Errorf("catalog already contained a schema with name: %s", name)
		}
		if err := def.CheckSchema(); err != nil {
			return err
		}
		// "A file can have at most one definition with one of the "primary" types. Primary types should always have the name main. It is possible for main to describe a non-primary type."
		switch def.Inner.(type) {
		case SchemaRecord, SchemaQuery, SchemaProcedure, SchemaSubscription:
			if frag != "main" {
				return fmt.Errorf("record, query, procedure, and subscription types must be 'main', not: %s", frag)
			}
		}
		s := Schema{
			ID:       name,
			Revision: sf.Revision,
			Def:      def.Inner,
		}
		c.Schemas[name] = s
	}
	return nil
}

func (c *Catalog) LoadDirectory(dirPath string) error {
	return filepath.WalkDir(dirPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".json") {
			return nil
		}
		// TODO: logging
		fmt.Println(p)
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()

		b, err := io.ReadAll(f)
		if err != nil {
			return err
		}

		var sf SchemaFile
		if err = json.Unmarshal(b, &sf); err != nil {
			return err
		}
		if err = c.AddSchemaFile(sf); err != nil {
			return err
		}
		return nil
	})
}

func (c *Catalog) Validate(d any, id string) error {
	schema, err := c.Resolve(id)
	if err != nil {
		return nil
	}
	switch v := schema.Def.(type) {
	case SchemaRecord:
		obj, ok := d.(map[string]any)
		if !ok {
			return fmt.Errorf("expected an object, got: %s", reflect.TypeOf(d))
		}
		// XXX: return v.Validate(d, schema.ID)
		_ = obj
		_ = v
		return nil
	case SchemaQuery:
		// XXX: return v.Validate(d)
		return nil
	case SchemaProcedure:
		// XXX: return v.Validate(d)
		return nil
	case SchemaSubscription:
		obj, ok := d.(map[string]any)
		if !ok {
			return fmt.Errorf("expected an object, got: %s", reflect.TypeOf(d))
		}
		// XXX: return v.Validate(d)
		_ = obj
		return nil
	case SchemaToken:
		str, ok := d.(string)
		if !ok {
			return fmt.Errorf("expected a string for token, got: %s", reflect.TypeOf(d))
		}
		if str != id {
			return fmt.Errorf("expected token (%s), got: %s", id, str)
		}
		return nil
	default:
		return c.validateDef(schema.Def, d)
	}
}

func (c *Catalog) validateDef(def any, d any) error {
	// TODO:
	switch v := def.(type) {
	case SchemaNull:
		return v.Validate(d)
	case SchemaBoolean:
		return v.Validate(d)
	case SchemaInteger:
		return v.Validate(d)
	case SchemaString:
		return v.Validate(d)
	case SchemaBytes:
		return v.Validate(d)
	case SchemaCIDLink:
		return v.Validate(d)
	case SchemaArray:
		arr, ok := d.([]any)
		if !ok {
			return fmt.Errorf("expected an array, got: %s", reflect.TypeOf(d))
		}
		// XXX: return v.ValidateArray(d, v)
		_ = arr
		return nil
	case SchemaObject:
		obj, ok := d.(map[string]any)
		if !ok {
			return fmt.Errorf("expected an object, got: %s", reflect.TypeOf(d))
		}
		return c.ValidateObject(v, obj)
	case SchemaBlob:
		return v.Validate(d)
	case SchemaParams:
		return v.Validate(d)
	case SchemaRef:
		// recurse
		return c.Validate(d, v.Ref)
	case SchemaUnion:
		// XXX: special ValidateUnion helper
		return nil
	case SchemaUnknown:
		return v.Validate(d)
	default:
		return fmt.Errorf("unhandled schema type: %s", reflect.TypeOf(v))
	}
}

func (c *Catalog) ValidateObject(s SchemaObject, d map[string]any) error {
	for _, k := range s.Required {
		if _, ok := d[k]; !ok {
			return fmt.Errorf("required field missing: %s", k)
		}
	}
	for k, def := range s.Properties {
		if v, ok := d[k]; ok {
			err := c.validateDef(def.Inner, v)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
