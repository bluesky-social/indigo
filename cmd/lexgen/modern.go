package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/lex"
	"github.com/bluesky-social/indigo/lex/lexgen"
	"golang.org/x/tools/imports"
)

func matchesNamespace(id, prefix string) bool {
	return id == prefix || strings.HasPrefix(id, prefix+".")
}

func modernTarget(id string, packages []lex.Package) *lex.Package {
	var target *lex.Package
	for i := range packages {
		pkg := &packages[i]
		if matchesNamespace(id, pkg.Prefix) && (target == nil || len(pkg.Prefix) > len(target.Prefix)) {
			target = pkg
		}
	}
	return target
}

// runModern uses the established build configuration for both output targeting and reference imports.
// External schemas populate the catalog but never become output targets.
func runModern(paths, externalPaths []string, packages []lex.Package, legacy bool, unknownType string) error {
	config := lexgen.NewGenConfig()
	if legacy {
		config = lexgen.LegacyConfig()
	}
	switch unknownType {
	case "json-raw-message", "map-string-any", "type-decoder":
		config.UnknownType = unknownType
	default:
		return fmt.Errorf("unsupported --unknown-type: %s", unknownType)
	}
	config.PackageMappings = make(map[string]string, len(packages))
	for _, pkg := range packages {
		if pkg.Prefix == "" || pkg.Import == "" || !token.IsIdentifier(pkg.GoPackage) || pkg.GoPackage == "_" {
			return fmt.Errorf("modern build packages require a namespace prefix, import path, and valid package name: %+v", pkg)
		}
		if _, ok := config.PackageMappings[pkg.Prefix]; ok {
			return fmt.Errorf("duplicate package prefix: %s", pkg.Prefix)
		}
		config.PackageMappings[pkg.Prefix] = pkg.Import
	}

	catalog := lexicon.NewBaseCatalog()
	schemas := make(map[string]*lexicon.SchemaFile)
	allPaths := append(append([]string(nil), paths...), externalPaths...)
	for _, path := range allPaths {
		path = filepath.Clean(path)
		if _, ok := schemas[path]; ok {
			continue
		}
		blob, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading lexicon %q: %w", path, err)
		}
		var schema lexicon.SchemaFile
		if err := json.Unmarshal(blob, &schema); err != nil {
			return fmt.Errorf("parsing lexicon %q: %w", path, err)
		}
		// AddSchemaFile finishes parsing the shared definition map before validating it.
		// Do not call FinishParse twice: union expansion appends its internal references.
		if err := catalog.AddSchemaFile(schema); err != nil {
			return fmt.Errorf("loading lexicon %q: %w", path, err)
		}
		schemas[path] = &schema
	}

	generated := make(map[string]bool)
	for _, path := range paths {
		path = filepath.Clean(path)
		if generated[path] {
			continue
		}
		generated[path] = true
		schema := schemas[path]
		target := modernTarget(schema.ID, packages)
		if target == nil || target.Outdir == "" {
			continue
		}
		flat, err := lexgen.FlattenSchemaFile(schema)
		if err != nil {
			return fmt.Errorf("flattening lexicon %q: %w", path, err)
		}
		fileConfig := *config
		fileConfig.OutputPackage = target.GoPackage
		fileConfig.OutputImport = target.Import
		var buf bytes.Buffer
		gen := lexgen.CodeGenerator{Config: &fileConfig, Lex: flat, Cat: catalog, Out: &buf}
		if err := gen.WriteLexicon(); err != nil {
			return fmt.Errorf("generating lexicon %q: %w", path, err)
		}
		outputPath := filepath.Join(target.Outdir, gen.FileName())
		output, err := imports.Process(outputPath, buf.Bytes(), &imports.Options{Comments: true, TabIndent: true, TabWidth: 8})
		if err != nil {
			return fmt.Errorf("formatting lexicon %q: %w", path, err)
		}
		if err := os.MkdirAll(target.Outdir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(outputPath, output, 0644); err != nil {
			return err
		}
	}
	return nil
}
