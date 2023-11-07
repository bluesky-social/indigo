package lexicon

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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

//func (c *Catalog) ValidateData(d map[string]any) error
