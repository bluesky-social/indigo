package lexicon

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Interface type for a resolver or container of lexicon schemas, and methods for validating generic data against those schemas.
type Catalog interface {
	// Looks up a schema reference (NSID string with optional fragment) to a Schema object.
	Resolve(ref string) (*Schema, error)
}

// Trivial in-memory Lexicon Catalog implementation.
type BaseCatalog struct {
	schemas map[string]Schema
}

// Creates a new empty BaseCatalog
func NewBaseCatalog() BaseCatalog {
	return BaseCatalog{
		schemas: make(map[string]Schema),
	}
}

// Returns a scheman definition (`Schema` struct) for a Lexicon reference.
//
// A Lexicon ref string is an NSID with an optional #-separated fragment. If the fragment isn't specified, '#main' is used by default.
func (c *BaseCatalog) Resolve(ref string) (*Schema, error) {
	if ref == "" {
		return nil, fmt.Errorf("tried to resolve empty string name")
	}
	// default to #main if name doesn't have a fragment
	if !strings.Contains(ref, "#") {
		ref = ref + "#main"
	}
	s, ok := c.schemas[ref]
	if !ok {
		return nil, fmt.Errorf("schema not found in catalog: %s", ref)
	}
	return &s, nil
}

// Inserts a schema loaded from a JSON file in to the catalog.
func (c *BaseCatalog) AddSchemaFile(sf SchemaFile) error {
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
		if _, ok := c.schemas[name]; ok {
			return fmt.Errorf("catalog already contained a schema with name: %s", name)
		}
		// "A file can have at most one definition with one of the "primary" types. Primary types should always have the name main. It is possible for main to describe a non-primary type."
		switch s := def.Inner.(type) {
		case SchemaRecord, SchemaQuery, SchemaProcedure, SchemaSubscription:
			if frag != "main" {
				return fmt.Errorf("record, query, procedure, and subscription types must be 'main', not: %s", frag)
			}
		case SchemaToken:
			// add fully-qualified name to token
			s.fullName = name
			def.Inner = s
		}
		def.SetBase(base)
		if err := def.CheckSchema(); err != nil {
			return err
		}
		s := Schema{
			ID:  name,
			Def: def.Inner,
		}
		c.schemas[name] = s
	}
	return nil
}

// internal helper for loading JSON files from bytes
func (c *BaseCatalog) addSchemaFromBytes(b []byte) error {
	var sf SchemaFile
	if err := json.Unmarshal(b, &sf); err != nil {
		return err
	}
	if err := c.AddSchemaFile(sf); err != nil {
		return err
	}
	return nil
}

// Recursively loads all '.json' files from a directory in to the catalog.
func (c *BaseCatalog) LoadDirectory(dirPath string) error {
	walkFunc := func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".json") {
			return nil
		}
		slog.Debug("loading Lexicon schema file", "path", p)
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()

		b, err := io.ReadAll(f)
		if err != nil {
			return err
		}
		return c.addSchemaFromBytes(b)
	}
	return filepath.WalkDir(dirPath, walkFunc)
}

// Recursively loads all '.json' files from an embed.FS
func (c *BaseCatalog) LoadEmbedFS(efs embed.FS) error {
	walkFunc := func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".json") {
			return nil
		}

		slog.Debug("loading embedded Lexicon schema file", "path", p)
		b, err := efs.ReadFile(p)
		if err != nil {
			return err
		}
		return c.addSchemaFromBytes(b)
	}
	return fs.WalkDir(efs, ".", walkFunc)
}
