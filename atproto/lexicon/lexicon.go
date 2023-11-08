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

// TODO: rkey? is nsid always known?
// TODO: nsid as syntax.NSID
func (c *Catalog) ValidateRecord(raw any, id string) error {
	def, err := c.Resolve(id)
	if err != nil {
		return err
	}
	s, ok := def.Def.(SchemaRecord)
	if !ok {
		return fmt.Errorf("schema is not of record type: %s", id)
	}
	d, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("record data is not object type")
	}
	t, ok := d["$type"]
	if !ok || t != id {
		return fmt.Errorf("record data missing $type, or didn't match expected NSID")
	}
	return c.validateObject(s.Record, d)
}

func (c *Catalog) validateData(def any, d any) error {
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
		return c.validateArray(v, arr)
	case SchemaObject:
		obj, ok := d.(map[string]any)
		if !ok {
			return fmt.Errorf("expected an object, got: %s", reflect.TypeOf(d))
		}
		return c.validateObject(v, obj)
	case SchemaBlob:
		return v.Validate(d)
	case SchemaParams:
		return v.Validate(d)
	case SchemaRef:
		// XXX: relative refs (in-file)
		// recurse
		next, err := c.Resolve(v.Ref)
		if err != nil {
			return err
		}
		return c.validateData(next.Def, d)
	case SchemaUnion:
		//return fmt.Errorf("XXX: union validation not implemented")
		return nil
	case SchemaUnknown:
		return v.Validate(d)
	case SchemaToken:
		str, ok := d.(string)
		if !ok {
			return fmt.Errorf("expected a string for token, got: %s", reflect.TypeOf(d))
		}
		// XXX: token validation not implemented
		_ = str
		return nil
	default:
		return fmt.Errorf("unhandled schema type: %s", reflect.TypeOf(v))
	}
}

func (c *Catalog) validateObject(s SchemaObject, d map[string]any) error {
	for _, k := range s.Required {
		if _, ok := d[k]; !ok {
			return fmt.Errorf("required field missing: %s", k)
		}
	}
	for k, def := range s.Properties {
		if v, ok := d[k]; ok {
			if v == nil && s.IsNullable(k) {
				continue
			}
			err := c.validateData(def.Inner, v)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Catalog) validateArray(s SchemaArray, arr []any) error {
	if (s.MinLength != nil && len(arr) < *s.MinLength) || (s.MaxLength != nil && len(arr) > *s.MaxLength) {
		return fmt.Errorf("array length out of bounds: %d", len(arr))
	}
	for _, v := range arr {
		err := c.validateData(s.Items.Inner, v)
		if err != nil {
			return err
		}
	}
	return nil
}
