package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type GenConfig struct {
	RegisterLexiconTypeID bool
	PackageMappings       map[string]string
	// one of: "type-decoder", "map-string-any", "json-raw-message"
	UnknownType string
}

type FlatGenerator struct {
	Config *GenConfig
	Lex    *FlatLexicon
	Out    io.Writer
}

func (gen *FlatGenerator) WriteLexicon() error {

	fmt.Fprintf(gen.Out, "// Auto-generated from Lexicon. DO NOT EDIT BY HAND.\n\n")
	fmt.Fprintf(gen.Out, "package %s\n\n", gen.pkgName())
	fmt.Fprintf(gen.Out, "// schema: %s\n\n", gen.Lex.NSID)
	fmt.Fprintln(gen.Out, "import (")
	for dep, _ := range gen.deps() {
		fmt.Fprintf(gen.Out, "    %s\n", dep)
	}
	fmt.Fprint(gen.Out, ")\n\n")

	for _, ft := range gen.Lex.Types {
		if err := gen.WriteType(&ft); err != nil {
			return err
		}
	}
	return nil
}

func (gen *FlatGenerator) pkgName() string {
	return nsidPkgName(gen.Lex.NSID)
}

func (gen *FlatGenerator) baseName() string {
	// TODO: memoize?
	return nsidBaseName(gen.Lex.NSID)
}

func (gen *FlatGenerator) deps() map[string]bool {
	d := map[string]bool{
		"lexutil \"github.com/bluesky-social/indigo/lex/util\"": true,
	}
	for _, t := range gen.Lex.Types {
		switch t.Type {
		case "query", "procedure":
			d["\"context\""] = true
		}
	}
	for ext, _ := range gen.Lex.ExternalRefs {
		if strings.HasPrefix(ext, "com.atproto.") {
			d["comatproto \"github.com/bluesky-social/indigo/api/atproto\""] = true
		} else {
			// XXX: handle more mappings; or log/error if missing
		}
	}
	return d
}

func (gen *FlatGenerator) WriteType(ft *FlatType) error {

	switch v := ft.Schema.Inner.(type) {
	// TODO: case lexicon.SchemaSubscription:
	case lexicon.SchemaRecord:
		if gen.Config.RegisterLexiconTypeID {
			fmt.Fprintf(gen.Out, "func init() {\n")
			fmt.Fprintf(gen.Out, "    lexutil.RegisterType(\"%s#main\", &%s{})", gen.Lex.NSID, gen.baseName())
			fmt.Fprintf(gen.Out, "}\n\n")
		}
		// HACK: insert record-level description in to object if nil
		if v.Description != nil && v.Record.Description == nil {
			v.Record.Description = v.Description
		}
		if err := gen.writeStruct(ft, &v.Record); err != nil {
			return err
		}
	case lexicon.SchemaQuery:
		return gen.writeQuery(ft, &v)
		// TODO: method
	case lexicon.SchemaProcedure:
		// TODO: method
	case lexicon.SchemaToken:
		// TODO: pass for now; could be a var/const?
	case lexicon.SchemaString, lexicon.SchemaInteger, lexicon.SchemaBoolean, lexicon.SchemaUnknown:
		// skip
	case lexicon.SchemaObject:
		if err := gen.writeStruct(ft, &v); err != nil {
			return err
		}
	case lexicon.SchemaUnion:
		// TODO: enum
	case lexicon.SchemaRef:
		// TODO: pass for now. could be an alias type?
	default:
		return fmt.Errorf("unhandled schema type for codegen: %T", ft.Schema.Inner)
	}

	return nil
}

func isRequired(required []string, fname string) bool {
	for _, k := range required {
		if k == fname {
			return true
		}
	}
	return false
}

func (gen *FlatGenerator) fieldType(def *lexicon.SchemaDef, optional bool) (string, error) {
	// XXX more completeness here
	switch v := def.Inner.(type) {
	case lexicon.SchemaNull:
		return "nil", nil
	case lexicon.SchemaBoolean:
		if optional {
			return "*bool", nil
		} else {
			return "bool", nil
		}
	case lexicon.SchemaInteger:
		if optional {
			return "*int", nil
		} else {
			return "int", nil
		}
	case lexicon.SchemaString:
		if optional {
			return "*string", nil
		} else {
			return "string", nil
		}
	case lexicon.SchemaBytes:
		if optional {
			return "*[]byte", nil
		} else {
			return "[]byte", nil
		}
	case lexicon.SchemaCIDLink:
		if optional {
			return "*cid.Cid", nil
		} else {
			return "cid.Cid", nil
		}
	case lexicon.SchemaArray:
		t, err := gen.fieldType(&v.Items, false)
		if err != nil {
			return "", err
		}
		if optional {
			return "*[]" + t, nil
		} else {
			return "[]" + t, nil
		}
	case lexicon.SchemaUnknown:
		switch gen.Config.UnknownType {
		case "type-decoder":
			return "*lexutil.LexiconTypeDecoder", nil
		case "json-raw-message":
			if optional {
				return "*json.RawMessage", nil
			} else {
				return "json.RawMessage", nil
			}
		case "map-string-any":
			return "map[string]any", nil
		default:
			return "map[string]any", nil
		}
	case lexicon.SchemaRef:
		ptr := ""
		if optional {
			ptr = "*"
		}
		if v.Ref == "#main" {
			return ptr + gen.baseName(), nil
		}
		if strings.HasPrefix(v.Ref, "#") {
			// local reference
			return fmt.Sprintf("%s%s_%s", ptr, gen.baseName(), strings.Title(v.Ref[1:])), nil
		} else {
			// external reference
			t, err := gen.externalRefType(v.Ref)
			if err != nil {
				return "", err
			}
			return ptr + t, nil
		}
	default:
		return "", fmt.Errorf("unhandled schema type in struct field: %T", def.Inner)
	}
}

func (gen *FlatGenerator) externalRefType(ref string) (string, error) {
	parts := strings.SplitN(ref, "#", 3)
	if len(parts) > 2 {
		return "", fmt.Errorf("failed to parse external ref: %s", ref)
	}
	nsid, err := syntax.ParseNSID(parts[0])
	if err != nil {
		return "", fmt.Errorf("failed to parse external ref NSID (%s): %w", ref, err)
	}
	if len(parts) == 1 || parts[1] == "main" {
		return fmt.Sprintf("%s.%s", nsidPkgName(nsid), nsidBaseName(nsid)), nil
	} else {
		return fmt.Sprintf("%s.%s_%s", nsidPkgName(nsid), nsidBaseName(nsid), strings.Title(parts[1])), nil
	}
}

func (gen *FlatGenerator) writeStruct(ft *FlatType, obj *lexicon.SchemaObject) error {

	name := gen.baseName()
	if ft.DefName != "main" {
		name += "_" + strings.Title(ft.DefName)
	}
	for _, sub := range ft.Path {
		name += "_" + strings.Title(sub)
	}

	if ft.DefName != "main" && len(ft.Path) == 0 {
		fmt.Fprintf(gen.Out, "// %s is a \"%s\" in the %s schema\n", name, ft.DefName, gen.Lex.NSID)
		if obj.Description != nil {
			fmt.Fprintln(gen.Out, "//")
		}
	}
	if obj.Description != nil {
		for _, l := range strings.Split(*obj.Description, "\n") {
			fmt.Fprintf(gen.Out, "// %s\n", l)
		}
	}
	fmt.Fprintf(gen.Out, "type %s struct {\n", name)

	// iterate field in sorted order
	fieldNames := []string{}
	for fname := range obj.Properties {
		fieldNames = append(fieldNames, fname)
	}
	sort.Strings(fieldNames)

	for _, fname := range fieldNames {
		field := obj.Properties[fname]
		optional := false
		omitempty := ""
		if obj.IsNullable(fname) || !isRequired(obj.Required, fname) {
			optional = true
			omitempty = ",omitempty"
		}

		var t string
		var err error

		switch field.Inner.(type) {
		case lexicon.SchemaObject, lexicon.SchemaUnion:
			t = name + "_" + strings.Title(fname)
			if optional {
				t = "*" + t
			}
		default:
			t, err = gen.fieldType(&field, optional)
			if err != nil {
				return err
			}
		}

		fmt.Fprintf(gen.Out, "    %s %s", strings.Title(fname), t)
		fmt.Fprintf(gen.Out, " `json:\"%s%s\" cborgen:\"%s%s\"`\n", fname, omitempty, fname, omitempty)
	}
	fmt.Fprintf(gen.Out, "}\n\n")

	return nil
}

func (gen *FlatGenerator) writeQuery(ft *FlatType, query *lexicon.SchemaQuery) error {
	name := gen.baseName()

	fmt.Fprintf(gen.Out, "// %s calls the XRPC method \"%s\".\n", name, gen.Lex.NSID)
	if query.Description != nil {
		fmt.Fprintln(gen.Out, "//")
		for _, l := range strings.Split(*query.Description, "\n") {
			fmt.Fprintf(gen.Out, "// %s\n", l)
		}
	}

	outputBytes := false
	outputStruct := ""
	if query.Output != nil && query.Output.Schema != nil {
		outputStruct = name + "_Output"
	} else if query.Output != nil && query.Output.Encoding != "" {
		outputBytes = true
	}

	paramNames := []string{}
	if query.Parameters != nil {
		for name := range query.Parameters.Properties {
			paramNames = append(paramNames, name)
		}
	}
	sort.Strings(paramNames)

	args := []string{"ctx context.Context"}
	reqParams := []string{}
	optParams := []string{}
	if len(paramNames) > 0 {
		fmt.Fprintln(gen.Out, "//")
		for _, name := range paramNames {
			param := query.Parameters.Properties[name]
			ptr := "*"
			if isRequired(query.Parameters.Required, name) {
				ptr = ""
				reqParams = append(reqParams, name)
			} else {
				optParams = append(optParams, name)
			}
			switch v := param.Inner.(type) {
			case lexicon.SchemaBoolean:
				if v.Description != nil && *v.Description != "" {
					fmt.Fprintf(gen.Out, "// %s: %s\n", name, *v.Description)
				}
				args = append(args, fmt.Sprintf("%s %sbool", name, ptr))
			case lexicon.SchemaInteger:
				if v.Description != nil && *v.Description != "" {
					fmt.Fprintf(gen.Out, "// %s: %s\n", name, *v.Description)
				}
				args = append(args, fmt.Sprintf("%s %sint64", name, ptr))
			case lexicon.SchemaString:
				if v.Description != nil && *v.Description != "" {
					fmt.Fprintf(gen.Out, "// %s: %s\n", name, *v.Description)
				}
				args = append(args, fmt.Sprintf("%s string", name))
			case lexicon.SchemaArray:
				if v.Description != nil && *v.Description != "" {
					fmt.Fprintf(gen.Out, "// %s[]: %s\n", name, *v.Description)
				}
				switch v.Items.Inner.(type) {
				case lexicon.SchemaBoolean:
					args = append(args, fmt.Sprintf("%s []bool", name))
				case lexicon.SchemaInteger:
					args = append(args, fmt.Sprintf("%s []int64", name))
				case lexicon.SchemaString:
					args = append(args, fmt.Sprintf("%s []string", name))
				default:
					return fmt.Errorf("unsupported parameter array type: %T", param.Inner)
				}
			default:
				return fmt.Errorf("unsupported parameter type: %T", param.Inner)
			}
		}
	}

	doOutParam := ""
	returnType := ""
	fmt.Fprintf(gen.Out, "func %s(%s) ", name, strings.Join(args, ", "))
	if outputStruct != "" {
		fmt.Fprintf(gen.Out, "(*%s, error) {\n", outputStruct)
		fmt.Fprintf(gen.Out, "    var out %s\n\n", outputStruct)
		doOutParam = "&out"
		returnType = "&out"
	} else if outputBytes {
		fmt.Fprintf(gen.Out, "([]byte, error) {\n")
		fmt.Fprintf(gen.Out, "    buf := new(bytes.Buffer)\n\n")
		doOutParam = "buf"
		returnType = "buf.Bytes()"
	} else {
		fmt.Fprintf(gen.Out, "error {\n")
		doOutParam = "nil"
		returnType = "nil"
	}
	// TODO: switch to map[string]any
	fmt.Fprintf(gen.Out, "    params := map[string]interface{}{}\n")
	for _, name := range optParams {
		param := query.Parameters.Properties[name]
		switch param.Inner.(type) {
		case lexicon.SchemaString:
			fmt.Fprintf(gen.Out, "    if %s != \"\" {\n", name)
			fmt.Fprintf(gen.Out, "        params[\"%s\"] = %s\n", name, name)
			fmt.Fprintf(gen.Out, "    }\n")
		case lexicon.SchemaArray:
			fmt.Fprintf(gen.Out, "    if len(%s) > 0 {\n", name)
			fmt.Fprintf(gen.Out, "        params[\"%s\"] = %s\n", name, name)
			fmt.Fprintf(gen.Out, "    }\n")
		default:
			fmt.Fprintf(gen.Out, "    if %s != nil {\n", name)
			fmt.Fprintf(gen.Out, "        params[\"%s\"] = *%s\n", name, name)
			fmt.Fprintf(gen.Out, "    }\n")
		}
	}
	for _, name := range reqParams {
		fmt.Fprintf(gen.Out, "    params[\"%s\"] = %s\n", name, name)
	}
	fmt.Fprintln(gen.Out, "")
	fmt.Fprintf(gen.Out, "    if err := c.LexDo(ctx, lexutil.Query, \"\", \"%s\", params, nil, %s); err != nil {\n", gen.Lex.NSID, doOutParam)
	fmt.Fprintf(gen.Out, "        return nil, err\n")
	fmt.Fprintf(gen.Out, "    }\n")
	fmt.Fprintf(gen.Out, "    return %s, nil\n", returnType)
	fmt.Fprintf(gen.Out, "}\n\n")

	return nil
}
