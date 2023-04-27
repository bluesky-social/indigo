// Package lex generates Go code for lexicons.
//
// (It is not a lexer.)
package lex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/imports"
)

const (
	EncodingCBOR = "application/cbor"
	EncodingJSON = "application/json"
	EncodingCAR  = "application/vnd.ipld.car"
	EncodingANY  = "*/*"
)

type Schema struct {
	prefix string

	Lexicon int                    `json:"lexicon"`
	ID      string                 `json:"id"`
	Defs    map[string]*TypeSchema `json:"defs"`
}

// TODO(bnewbold): suspect this param needs updating for lex refactors
type Param struct {
	Type     string `json:"type"`
	Maximum  int    `json:"maximum"`
	Required bool   `json:"required"`
}

type OutputType struct {
	Encoding string      `json:"encoding"`
	Schema   *TypeSchema `json:"schema"`
}

type InputType struct {
	Encoding string      `json:"encoding"`
	Schema   *TypeSchema `json:"schema"`
}

type TypeSchema struct {
	prefix    string
	id        string
	defName   string
	defMap    map[string]*ExtDef
	needsCbor bool
	needsType bool

	Type        string      `json:"type"`
	Key         string      `json:"key"`
	Description string      `json:"description"`
	Parameters  *TypeSchema `json:"parameters"`
	Input       *InputType  `json:"input"`
	Output      *OutputType `json:"output"`
	Record      *TypeSchema `json:"record"`

	Ref        string                 `json:"ref"`
	Refs       []string               `json:"refs"`
	Required   []string               `json:"required"`
	Nullable   []string               `json:"nullable"`
	Properties map[string]*TypeSchema `json:"properties"`
	MaxLength  int                    `json:"maxLength"`
	Items      *TypeSchema            `json:"items"`
	Const      any                    `json:"const"`
	Enum       []string               `json:"enum"`
	Closed     bool                   `json:"closed"`

	Default any `json:"default"`
	Minimum any `json:"minimum"`
	Maximum any `json:"maximum"`
}

func (s *Schema) Name() string {
	p := strings.Split(s.ID, ".")
	return p[len(p)-2] + p[len(p)-1]
}

type outputType struct {
	Name      string
	Type      *TypeSchema
	NeedsCbor bool
	NeedsType bool
}

func (s *Schema) AllTypes(prefix string, defMap map[string]*ExtDef) []outputType {
	var out []outputType

	var walk func(name string, ts *TypeSchema, needsCbor bool)
	walk = func(name string, ts *TypeSchema, needsCbor bool) {
		if ts == nil {
			panic(fmt.Sprintf("nil type schema in %q (%s)", name, s.ID))
		}

		if needsCbor {
			fmt.Println("Setting to record: ", name)
			if name == "EmbedImages_View" {
				panic("not ok")
			}
			ts.needsCbor = true
		}

		ts.prefix = prefix
		ts.id = s.ID
		ts.defMap = defMap
		if ts.Type == "object" ||
			(ts.Type == "union" && len(ts.Refs) > 0) {
			out = append(out, outputType{
				Name:      name,
				Type:      ts,
				NeedsCbor: ts.needsCbor,
			})

			for _, r := range ts.Refs {
				refname := r
				if strings.HasPrefix(refname, "#") {
					refname = s.ID + r
				}

				ed, ok := defMap[refname]
				if !ok {
					panic(fmt.Sprintf("cannot find: %q", refname))
				}

				fmt.Println("UNION REF", refname, name, needsCbor)

				if needsCbor {
					ed.Type.needsCbor = true
				}

				ed.Type.needsType = true
			}
		}

		if ts.Type == "ref" {
			refname := ts.Ref
			if strings.HasPrefix(refname, "#") {
				refname = s.ID + ts.Ref
			}

			sub, ok := defMap[refname]
			if !ok {
				panic(fmt.Sprintf("missing ref: %q", refname))
			}

			if needsCbor {
				sub.Type.needsCbor = true
			}
		}

		for childname, val := range ts.Properties {
			walk(name+"_"+strings.Title(childname), val, ts.needsCbor)
		}

		if ts.Items != nil {
			walk(name+"_Elem", ts.Items, ts.needsCbor)
		}

		if ts.Input != nil {
			if ts.Input.Schema == nil {
				if ts.Input.Encoding != "application/cbor" && ts.Input.Encoding != "*/*" {
					panic(fmt.Sprintf("strange input type def in %s", s.ID))
				}
			} else {
				walk(name+"_Input", ts.Input.Schema, ts.needsCbor)
			}
		}

		if ts.Output != nil {
			if ts.Output.Schema == nil {
				if ts.Output.Encoding != "application/cbor" && ts.Output.Encoding != "application/vnd.ipld.car" && ts.Output.Encoding != "*/*" {
					panic(fmt.Sprintf("strange output type def in %s", s.ID))
				}
			} else {
				walk(name+"_Output", ts.Output.Schema, ts.needsCbor)
			}
		}

		if ts.Type == "record" {
			ts.Record.needsType = true
			walk(name, ts.Record, true)
		}

	}

	tname := nameFromID(s.ID, prefix)

	for name, def := range s.Defs {
		n := tname + "_" + strings.Title(name)
		if name == "main" {
			n = tname
		}
		walk(n, def, def.needsCbor)
	}

	return out
}

func ReadSchema(f string) (*Schema, error) {
	fi, err := os.Open(f)
	if err != nil {
		return nil, err
	}
	defer fi.Close()

	var s Schema
	if err := json.NewDecoder(fi).Decode(&s); err != nil {
		return nil, err
	}

	return &s, nil
}

func BuildExtDefMap(ss []*Schema, prefixes []string) map[string]*ExtDef {
	out := make(map[string]*ExtDef)
	for _, s := range ss {
		for k, d := range s.Defs {
			d.defMap = out
			d.id = s.ID
			d.defName = k

			var pref string
			for _, p := range prefixes {
				if strings.HasPrefix(s.ID, p) {
					pref = p
					break
				}
			}
			d.prefix = pref

			n := s.ID
			if k != "main" {
				n = s.ID + "#" + k
			}
			out[n] = &ExtDef{
				Type: d,
			}
		}
	}
	return out
}

type ExtDef struct {
	Type *TypeSchema
}

// TODO: this method is necessary because in lexicon there is no way to know if
// a type needs to be marshaled with a "$type" field up front, you can only
// know for sure by seeing where the type is used.
func FixRecordReferences(schemas []*Schema, defmap map[string]*ExtDef, prefix string) {
	for _, s := range schemas {
		if !strings.HasPrefix(s.ID, prefix) {
			continue
		}

		tps := s.AllTypes(prefix, defmap)
		for _, t := range tps {
			if t.Type.Type == "record" {
				t.NeedsType = true
				t.Type.needsType = true
			}

			if t.Type.Type == "union" {
				for _, r := range t.Type.Refs {
					if r[0] == '#' {
						r = s.ID + r
					}

					if _, known := defmap[r]; known != true {
						panic(fmt.Sprintf("reference to unknown record type: %s", r))
					}

					if t.NeedsCbor {
						defmap[r].Type.needsCbor = true
					}
				}
			}
		}
	}
}

func printerf(w io.Writer) func(format string, args ...any) {
	return func(format string, args ...any) {
		fmt.Fprintf(w, format, args...)
	}
}

func GenCodeForSchema(pkg string, prefix string, fname string, reqcode bool, s *Schema, defmap map[string]*ExtDef, imports map[string]string) error {
	buf := new(bytes.Buffer)
	pf := printerf(buf)

	s.prefix = prefix
	for _, d := range s.Defs {
		d.prefix = prefix
	}

	// Add the standard Go generated code header as recognized by GitHub, VS Code, etc.
	// See https://golang.org/s/generatedcode.
	pf("// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.\n\n")

	pf("package %s\n\n", pkg)

	pf("// schema: %s\n\n", s.ID)

	pf("import (\n")
	pf("\t\"context\"\n")
	pf("\t\"fmt\"\n")
	pf("\t\"encoding/json\"\n")
	pf("\tcbg \"github.com/whyrusleeping/cbor-gen\"\n")
	pf("\t\"github.com/bluesky-social/indigo/xrpc\"\n")
	pf("\t\"github.com/bluesky-social/indigo/lex/util\"\n")
	for k, v := range imports {
		if k != prefix {
			pf("\t%s %q\n", importNameForPrefix(k), v)
		}
	}
	pf(")\n\n")

	tps := s.AllTypes(prefix, defmap)

	if err := writeDecoderRegister(buf, tps); err != nil {
		return err
	}

	sort.Slice(tps, func(i, j int) bool {
		return tps[i].Name < tps[j].Name
	})
	for _, ot := range tps {
		fmt.Println("TYPE: ", ot.Name, ot.NeedsCbor, ot.NeedsType)
		if err := ot.Type.WriteType(ot.Name, buf); err != nil {
			return err
		}
	}

	if reqcode {
		name := nameFromID(s.ID, prefix)
		main, ok := s.Defs["main"]
		if ok {
			if err := writeMethods(name, main, buf); err != nil {
				return err
			}
		}
	}

	if err := writeCodeFile(buf.Bytes(), fname); err != nil {
		return err
	}

	return nil
}

func writeDecoderRegister(w io.Writer, tps []outputType) error {
	var buf bytes.Buffer
	outf := printerf(&buf)

	for _, t := range tps {
		if t.Type.needsType && !strings.Contains(t.Name, "_") {
			id := t.Type.id
			if t.Type.defName != "" {
				id = id + "#" + t.Type.defName
			}
			if buf.Len() == 0 {
				outf("func init() {\n")
			}
			outf("util.RegisterType(%q, &%s{})\n", id, t.Name)
		}
	}
	if buf.Len() == 0 {
		return nil
	}
	outf("}")
	_, err := w.Write(buf.Bytes())
	return err
}

func writeCodeFile(b []byte, fname string) error {
	fixed, err := imports.Process(fname, b, nil)
	if err != nil {
		return fmt.Errorf("failed to format output of %q with goimports: %w", fname, err)
	}

	if err := os.WriteFile(fname, fixed, 0664); err != nil {
		return err
	}

	return nil
}

func writeMethods(typename string, ts *TypeSchema, w io.Writer) error {
	switch ts.Type {
	case "token":
		n := ts.id
		if ts.defName != "main" {
			n += "#" + ts.defName
		}

		fmt.Fprintf(w, "const %s = %q\n", typename, n)
		return nil
	case "record":
		return nil
	case "query":
		return ts.WriteRPC(w, typename)
	case "procedure":
		return ts.WriteRPC(w, typename)
	case "object", "string":
		return nil
	case "subscription":
		// TODO: should probably have some methods generated for this
		return nil
	default:
		return fmt.Errorf("unrecognized lexicon type %q", ts.Type)
	}
}

func nameFromID(id, prefix string) string {
	parts := strings.Split(strings.TrimPrefix(id, prefix), ".")
	var tname string
	for _, s := range parts {
		tname += strings.Title(s)
	}

	return tname

}

func orderedMapIter[T any](m map[string]T, cb func(string, T) error) error {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		if err := cb(k, m[k]); err != nil {
			return err
		}
	}
	return nil
}

func (s *TypeSchema) WriteRPC(w io.Writer, typename string) error {
	pf := printerf(w)
	fname := typename

	params := "ctx context.Context, c *xrpc.Client"
	inpvar := "nil"
	inpenc := ""

	if s.Input != nil {
		inpvar = "input"
		inpenc = s.Input.Encoding
		switch s.Input.Encoding {
		case EncodingCBOR, EncodingANY:
			params = fmt.Sprintf("%s, input io.Reader", params)
		case EncodingJSON:
			params = fmt.Sprintf("%s, input *%s_Input", params, fname)

		default:
			return fmt.Errorf("unsupported input encoding (RPC input): %q", s.Input.Encoding)
		}
	}

	if s.Parameters != nil {
		if err := orderedMapIter(s.Parameters.Properties, func(name string, t *TypeSchema) error {
			tn, err := s.typeNameForField(name, "", *t)
			if err != nil {
				return err
			}

			// TODO: deal with optional params
			params = params + fmt.Sprintf(", %s %s", name, tn)
			return nil
		}); err != nil {
			return err
		}
	}

	out := "error"
	if s.Output != nil {
		switch s.Output.Encoding {
		case EncodingCBOR, EncodingCAR, EncodingANY:
			out = "([]byte, error)"
		case EncodingJSON:
			outname := fname + "_Output"
			if s.Output.Schema.Type == "ref" {
				outname = s.typeNameFromRef(s.Output.Schema.Ref)
			}

			out = fmt.Sprintf("(*%s, error)", outname)
		default:
			return fmt.Errorf("unrecognized encoding scheme (RPC output): %q", s.Output.Encoding)
		}
	}

	pf("// %s calls the XRPC method %q.\n", fname, s.id)
	if s.Parameters != nil && len(s.Parameters.Properties) > 0 {
		pf("//\n")
		if err := orderedMapIter(s.Parameters.Properties, func(name string, t *TypeSchema) error {
			if t.Description != "" {
				pf("// %s: %s\n", name, t.Description)
			}
			return nil
		}); err != nil {
			return err
		}
	}
	pf("func %s(%s) %s {\n", fname, params, out)

	outvar := "nil"
	errRet := "err"
	outRet := "nil"
	if s.Output != nil {
		switch s.Output.Encoding {
		case EncodingCBOR, EncodingCAR, EncodingANY:
			pf("buf := new(bytes.Buffer)\n")
			outvar = "buf"
			errRet = "nil, err"
			outRet = "buf.Bytes(), nil"
		case EncodingJSON:
			outname := fname + "_Output"
			if s.Output.Schema.Type == "ref" {
				outname = s.typeNameFromRef(s.Output.Schema.Ref)
			}
			pf("\tvar out %s\n", outname)
			outvar = "&out"
			errRet = "nil, err"
			outRet = "&out, nil"
		default:
			return fmt.Errorf("unrecognized output encoding (func signature): %q", s.Output.Encoding)
		}
	}

	queryparams := "nil"
	if s.Parameters != nil {
		queryparams = "params"
		pf(`
	params := map[string]interface{}{
`)
		if err := orderedMapIter(s.Parameters.Properties, func(name string, t *TypeSchema) error {
			pf(`"%s": %s,
`, name, name)
			return nil
		}); err != nil {
			return err
		}
		pf("}\n")
	}

	var reqtype string
	switch s.Type {
	case "procedure":
		reqtype = "xrpc.Procedure"
	case "query":
		reqtype = "xrpc.Query"
	default:
		return fmt.Errorf("can only generate RPC for Query or Procedure (got %s)", s.Type)
	}

	pf("\tif err := c.Do(ctx, %s, %q, \"%s\", %s, %s, %s); err != nil {\n", reqtype, inpenc, s.id, queryparams, inpvar, outvar)
	pf("\t\treturn %s\n", errRet)
	pf("\t}\n\n")
	pf("\treturn %s\n", outRet)
	pf("}\n\n")

	return nil
}

func doTemplate(w io.Writer, info interface{}, templ string) error {
	t := template.Must(template.New("").
		Funcs(template.FuncMap{
			"TODO": func(thing string) string {
				return "//TODO: " + thing
			},
		}).Parse(templ))

	return t.Execute(w, info)
}

func CreateHandlerStub(pkg string, impmap map[string]string, dir string, schemas []*Schema, handlers bool) error {
	buf := new(bytes.Buffer)

	if err := WriteXrpcServer(buf, schemas, pkg, impmap); err != nil {
		return err
	}

	fname := filepath.Join(dir, "stubs.go")
	if err := writeCodeFile(buf.Bytes(), fname); err != nil {
		return err
	}

	if handlers {
		buf := new(bytes.Buffer)

		if err := WriteServerHandlers(buf, schemas, pkg, impmap); err != nil {
			return err
		}

		fname := filepath.Join(dir, "handlers.go")
		if err := writeCodeFile(buf.Bytes(), fname); err != nil {
			return err
		}

	}

	return nil
}

func importNameForPrefix(prefix string) string {
	return strings.Join(strings.Split(prefix, "."), "") + "types"
}

func WriteServerHandlers(w io.Writer, schemas []*Schema, pkg string, impmap map[string]string) error {
	pf := printerf(w)
	pf("package %s\n\n", pkg)
	pf("import (\n")
	pf("\t\"context\"\n")
	pf("\t\"fmt\"\n")
	pf("\t\"encoding/json\"\n")
	pf("\t\"github.com/bluesky-social/indigo/xrpc\"\n")
	for k, v := range impmap {
		pf("\t%s\"%s\"\n", importNameForPrefix(k), v)
	}
	pf(")\n\n")

	for _, s := range schemas {

		var prefix string
		for k := range impmap {
			if strings.HasPrefix(s.ID, k) {
				prefix = k
				break
			}
		}

		main, ok := s.Defs["main"]
		if !ok {
			fmt.Printf("WARNING: schema %q doesn't have a main def\n", s.ID)
			continue
		}

		if main.Type == "procedure" || main.Type == "query" {
			fname := idToTitle(s.ID)
			tname := nameFromID(s.ID, prefix)
			impname := importNameForPrefix(prefix)
			if err := main.WriteHandlerStub(w, fname, tname, impname); err != nil {
				return err
			}
		}
	}

	return nil
}

func WriteXrpcServer(w io.Writer, schemas []*Schema, pkg string, impmap map[string]string) error {
	pf := printerf(w)
	pf("package %s\n\n", pkg)
	pf("import (\n")
	pf("\t\"context\"\n")
	pf("\t\"fmt\"\n")
	pf("\t\"encoding/json\"\n")
	pf("\t\"github.com/bluesky-social/indigo/xrpc\"\n")
	pf("\t\"github.com/labstack/echo/v4\"\n")

	var prefixes []string
	orderedMapIter[string](impmap, func(k, v string) error {
		prefixes = append(prefixes, k)
		pf("\t%s\"%s\"\n", importNameForPrefix(k), v)
		return nil
	})
	pf(")\n\n")

	ssets := make(map[string][]*Schema)
	for _, s := range schemas {
		var pref string
		for _, p := range prefixes {
			if strings.HasPrefix(s.ID, p) {
				pref = p
				break
			}
		}
		if pref == "" {
			return fmt.Errorf("no matching prefix for schema %q (tried %s)", s.ID, prefixes)
		}

		ssets[pref] = append(ssets[pref], s)
	}

	for _, p := range prefixes {
		ss := ssets[p]

		pf("func (s *Server) RegisterHandlers%s(e *echo.Echo) error {\n", idToTitle(p))
		for _, s := range ss {

			main, ok := s.Defs["main"]
			if !ok {
				continue
			}

			var verb string
			switch main.Type {
			case "query":
				verb = "GET"
			case "procedure":
				verb = "POST"
			default:
				continue
			}

			pf("e.%s(\"/xrpc/%s\", s.Handle%s)\n", verb, s.ID, idToTitle(s.ID))
		}

		pf("return nil\n}\n\n")

		for _, s := range ss {

			var prefix string
			for k := range impmap {
				if strings.HasPrefix(s.ID, k) {
					prefix = k
					break
				}
			}

			main, ok := s.Defs["main"]
			if !ok {
				continue
			}

			if main.Type == "procedure" || main.Type == "query" {
				fname := idToTitle(s.ID)
				tname := nameFromID(s.ID, prefix)
				impname := importNameForPrefix(prefix)
				if err := main.WriteRPCHandler(w, fname, tname, impname); err != nil {
					return fmt.Errorf("writing handler for %s: %w", s.ID, err)
				}
			}
		}
	}

	return nil
}

func idToTitle(id string) string {
	var fname string
	for _, p := range strings.Split(id, ".") {
		fname += strings.Title(p)
	}
	return fname
}

func (s *TypeSchema) WriteHandlerStub(w io.Writer, fname, shortname, impname string) error {
	pf := printerf(w)
	paramtypes := []string{"ctx context.Context"}
	if s.Type == "query" {

		if s.Parameters != nil {
			var required map[string]bool
			if s.Parameters.Required != nil {
				required = make(map[string]bool)
				for _, r := range s.Required {
					required[r] = true
				}
			}
			orderedMapIter[*TypeSchema](s.Parameters.Properties, func(k string, t *TypeSchema) error {
				switch t.Type {
				case "string":
					paramtypes = append(paramtypes, k+" string")
				case "integer":
					// TODO(bnewbold) could be handling "nullable" here
					if required != nil && !required[k] {
						paramtypes = append(paramtypes, k+" *int")
					} else {
						paramtypes = append(paramtypes, k+" int")
					}
				case "float":
					return fmt.Errorf("non-integer numbers currently unsupported")
				case "array":
					paramtypes = append(paramtypes, k+"[]"+t.Items.Type)
				default:
					return fmt.Errorf("unsupported handler parameter type: %s", t.Type)
				}
				return nil
			})
		}
	}

	returndef := "error"
	if s.Output != nil {
		switch s.Output.Encoding {
		case "application/json":
			outname := shortname + "_Output"
			if s.Output.Schema.Type == "ref" {
				outname = s.typeNameFromRef(s.Output.Schema.Ref)
			}
			returndef = fmt.Sprintf("(*%s.%s, error)", impname, outname)
		case "application/cbor", "application/vnd.ipld.car", "*/*":
			returndef = fmt.Sprintf("(io.Reader, error)")
		default:
			return fmt.Errorf("unrecognized output encoding (handler stub): %q", s.Output.Encoding)
		}
	}

	if s.Input != nil {
		switch s.Input.Encoding {
		case "application/json":
			paramtypes = append(paramtypes, fmt.Sprintf("input *%s.%s_Input", impname, shortname))
		case "application/cbor":
			paramtypes = append(paramtypes, "r io.Reader")
		}
	}

	pf("func (s *Server) handle%s(%s) %s {\n", fname, strings.Join(paramtypes, ","), returndef)
	pf("panic(\"not yet implemented\")\n}\n\n")

	return nil
}

func (s *TypeSchema) WriteRPCHandler(w io.Writer, fname, shortname, impname string) error {
	pf := printerf(w)
	tname := shortname

	pf("func (s *Server) Handle%s(c echo.Context) error {\n", fname)

	pf("ctx, span := otel.Tracer(\"server\").Start(c.Request().Context(), %q)\n", "Handle"+fname)
	pf("defer span.End()\n")

	paramtypes := []string{"ctx context.Context"}
	params := []string{"ctx"}
	if s.Type == "query" {
		if s.Parameters != nil {
			// TODO(bnewbold): could be handling 'nullable' here
			required := make(map[string]bool)
			for _, r := range s.Parameters.Required {
				required[r] = true
			}
			for k, v := range s.Parameters.Properties {
				if v.Default != nil {
					required[k] = true
				}
			}
			if err := orderedMapIter(s.Parameters.Properties, func(k string, t *TypeSchema) error {
				switch t.Type {
				case "string":
					params = append(params, k)
					paramtypes = append(paramtypes, k+" string")
					pf("%s := c.QueryParam(\"%s\")\n", k, k)
				case "integer":
					params = append(params, k)

					if !required[k] {
						paramtypes = append(paramtypes, k+" *int")
						pf(`
var %s *int
if p := c.QueryParam("%s"); p != "" {
	%s_val, err := strconv.Atoi(p)
	if err != nil {
		return err
	}
	%s  = &%s_val
}
`, k, k, k, k, k)
					} else if t.Default != nil {
						paramtypes = append(paramtypes, k+" int")
						pf(`
var %s int
if p := c.QueryParam("%s"); p != "" {
var err error
%s, err = strconv.Atoi(p)
if err != nil {
	return err
}
} else {
	%s = %d
}
`, k, k, k, k, int(t.Default.(float64)))
					} else {

						paramtypes = append(paramtypes, k+" int")
						pf(`
%s, err := strconv.Atoi(c.QueryParam("%s"))
if err != nil {
	return err
}
`, k, k)
					}

				case "float":
					return fmt.Errorf("non-integer numbers currently unsupported")
				case "boolean":
					params = append(params, k)
					if !required[k] {
						paramtypes = append(paramtypes, k+" *bool")
						pf(`
var %s *bool
if p := c.QueryParam("%s"); p != "" {
	%s_val, err := strconv.ParseBool(p)
	if err != nil {
		return err
	}
	%s  = &%s_val
}
`, k, k, k, k, k)
					} else if t.Default != nil {
						paramtypes = append(paramtypes, k+" bool")
						pf(`
var %s bool
if p := c.QueryParam("%s"); p != "" {
var err error
%s, err = strconv.ParseBool(p)
if err != nil {
	return err
}
} else {
	%s = %v
}
`, k, k, k, k, t.Default.(bool))
					} else {

						paramtypes = append(paramtypes, k+" bool")
						pf(`
%s, err := strconv.ParseBool(c.QueryParam("%s"))
if err != nil {
	return err
}
`, k, k)
					}

				case "array":
					if t.Items.Type != "string" {
						return fmt.Errorf("currently only string arrays are supported in query params")
					}
					paramtypes = append(paramtypes, k+" []string")
					params = append(params, k)
					pf(`
%s := c.QueryParams()["%s"]
`, k, k)

				default:
					return fmt.Errorf("unsupported handler parameter type: %s", t.Type)
				}
				return nil
			}); err != nil {
				return err
			}
		}
	} else if s.Type == "procedure" {
		if s.Input != nil {
			intname := impname + "." + tname + "_Input"
			switch s.Input.Encoding {
			case EncodingJSON:
				pf(`
var body %s
if err := c.Bind(&body); err != nil {
	return err
}
`, intname)
				paramtypes = append(paramtypes, "body *"+intname)
				params = append(params, "&body")
			case EncodingCBOR:
				pf("body := c.Request().Body\n")
				paramtypes = append(paramtypes, "r io.Reader")
				params = append(params, "body")
			case EncodingANY:
				pf("body := c.Request().Body\n")
				pf("contentType := c.Request().Header.Get(\"Content-Type\")\n")
				paramtypes = append(paramtypes, "r io.Reader", "contentType string")
				params = append(params, "body", "contentType")

			default:
				return fmt.Errorf("unrecognized input encoding: %q", s.Input.Encoding)
			}
		}
	} else {
		return fmt.Errorf("can only generate handlers for queries or procedures")
	}

	assign := "handleErr"
	returndef := "error"
	if s.Output != nil {
		switch s.Output.Encoding {
		case EncodingJSON:
			assign = "out, handleErr"
			outname := tname + "_Output"
			if s.Output.Schema.Type == "ref" {
				outname = s.typeNameFromRef(s.Output.Schema.Ref)
			}
			pf("var out *%s.%s\n", impname, outname)
			returndef = fmt.Sprintf("(*%s.%s, error)", impname, outname)
		case EncodingCBOR, EncodingCAR, EncodingANY:
			assign = "out, handleErr"
			pf("var out io.Reader\n")
			returndef = "(io.Reader, error)"
		default:
			return fmt.Errorf("unrecognized output encoding (RPC output handler): %q", s.Output.Encoding)
		}
	}
	pf("var handleErr error\n")
	pf("// func (s *Server) handle%s(%s) %s\n", fname, strings.Join(paramtypes, ","), returndef)
	pf("%s = s.handle%s(%s)\n", assign, fname, strings.Join(params, ","))
	pf("if handleErr != nil {\nreturn handleErr\n}\n")

	if s.Output != nil {
		switch s.Output.Encoding {
		case EncodingJSON:
			pf("return c.JSON(200, out)\n}\n\n")
		case EncodingANY:
			pf("return c.Stream(200, \"application/octet-stream\", out)\n}\n\n")
		case EncodingCBOR:
			pf("return c.Stream(200, \"application/octet-stream\", out)\n}\n\n")
		case EncodingCAR:
			pf("return c.Stream(200, \"application/vnd.ipld.car\", out)\n}\n\n")
		default:
			return fmt.Errorf("unrecognized output encoding (RPC output handler return): %q", s.Output.Encoding)
		}
	} else {
		pf("return nil\n}\n\n")
	}

	return nil
}

func (s *TypeSchema) typeNameFromRef(r string) string {
	ts, err := s.lookupRef(r)
	if err != nil {
		panic(err)
	}

	if ts.prefix == "" {
		panic(fmt.Sprintf("no prefix for referenced type: %s", ts.id))
	}

	if s.prefix == "" {
		panic(fmt.Sprintf("no prefix for referencing type: %q %q", s.id, s.defName))
	}

	// TODO: probably not technically correct, but i'm kinda over how lexicon
	// tries to enforce application logic in a schema language
	if ts.Type == "string" {
		return "string"
	}

	var pkg string
	if ts.prefix != s.prefix {
		pkg = importNameForPrefix(ts.prefix) + "."
	}

	return pkg + ts.TypeName()
}

func (s *TypeSchema) TypeName() string {
	if s.id == "" {
		panic("type schema hint fields not set")
	}
	if s.prefix == "" {
		panic("why no prefix?")
	}
	n := nameFromID(s.id, s.prefix)
	if s.defName != "main" {
		n += "_" + strings.Title(s.defName)
	}

	return n
}

func (s *TypeSchema) typeNameForField(name, k string, v TypeSchema) (string, error) {
	switch v.Type {
	case "string":
		return "string", nil
	case "float":
		return "float64", nil
	case "integer":
		return "int64", nil
	case "boolean":
		return "bool", nil
	case "object":
		return "*" + name + "_" + strings.Title(k), nil
	case "ref":
		return "*" + s.typeNameFromRef(v.Ref), nil
	case "datetime":
		// TODO: maybe do a native type?
		return "string", nil
	case "unknown":
		return "*util.LexiconTypeDecoder", nil
	case "union":
		return "*" + name + "_" + strings.Title(k), nil
	case "blob":
		return "*util.LexBlob", nil
	case "array":
		subt, err := s.typeNameForField(name+"_"+strings.Title(k), "Elem", *v.Items)
		if err != nil {
			return "", err
		}

		return "[]" + subt, nil
	case "cid-link":
		return "util.LexLink", nil
	case "bytes":
		return "util.LexBytes", nil
	default:
		return "", fmt.Errorf("field %q in %s has unsupported type name (%s)", k, name, v.Type)
	}
}

func (ts *TypeSchema) lookupRef(ref string) (*TypeSchema, error) {
	fqref := ref
	if strings.HasPrefix(ref, "#") {
		fmt.Println("updating fqref: ", ts.id)
		fqref = ts.id + ref
	}
	rr, ok := ts.defMap[fqref]
	if !ok {
		fmt.Println(ts.defMap)
		panic(fmt.Sprintf("no such ref: %q", fqref))
	}

	return rr.Type, nil
}

func (ts *TypeSchema) WriteType(name string, w io.Writer) error {
	name = strings.Title(name)
	if err := ts.writeTypeDefinition(name, w); err != nil {
		return err
	}

	if err := ts.writeTypeMethods(name, w); err != nil {
		return err
	}

	return nil
}

func (ts *TypeSchema) writeTypeDefinition(name string, w io.Writer) error {
	pf := printerf(w)

	switch {
	case strings.HasSuffix(name, "_Output"):
		pf("// %s is the output of a %s call.\n", name, ts.id)
	case strings.HasSuffix(name, "Input"):
		pf("// %s is the input argument to a %s call.\n", name, ts.id)
	case ts.defName != "":
		pf("// %s is a %q in the %s schema.\n", name, ts.defName, ts.id)
	}
	if ts.Description != "" {
		pf("//\n// %s\n", ts.Description)
	}

	switch ts.Type {
	case "string":
		// TODO: deal with max length
		pf("type %s string\n", name)
	case "float":
		pf("type %s float64\n", name)
	case "integer":
		pf("type %s int64\n", name)
	case "boolean":
		pf("type %s bool\n", name)
	case "object":
		if len(ts.Properties) == 0 {
			pf("type %s interface{}\n", name)
			return nil
		}

		if ts.needsType {
			pf("//\n// RECORDTYPE: %s\n", name)
		}

		pf("type %s struct {\n", name)

		if ts.needsType {
			var omit string
			if ts.id == "com.atproto.repo.strongRef" { // TODO: hack
				omit = ",omitempty"
			}
			pf("\tLexiconTypeID string `json:\"$type,const=%s%s\" cborgen:\"$type,const=%s%s\"`\n", ts.id, omit, ts.id, omit)
		} else {
			//pf("\tLexiconTypeID string `json:\"$type,omitempty\" cborgen:\"$type,omitempty\"`\n")
		}

		required := make(map[string]bool)
		for _, req := range ts.Required {
			required[req] = true
		}

		nullable := make(map[string]bool)
		for _, req := range ts.Nullable {
			nullable[req] = true
		}

		if err := orderedMapIter(ts.Properties, func(k string, v *TypeSchema) error {
			goname := strings.Title(k)

			tname, err := ts.typeNameForField(name, k, *v)
			if err != nil {
				return err
			}

			var ptr string
			var omit string
			if !required[k] {
				omit = ",omitempty"
				if !strings.HasPrefix(tname, "*") && !strings.HasPrefix(tname, "[]") {
					ptr = "*"
				}
			}
			if nullable[k] {
				omit = ""
				if !strings.HasPrefix(tname, "*") && !strings.HasPrefix(tname, "[]") {
					ptr = "*"
				}
			}

			jsonOmit, cborOmit := omit, omit

			// TODO: hard-coded hacks for now, making this type (with underlying type []byte)
			// be omitempty.
			if ptr == "" && tname == "util.LexBytes" {
				jsonOmit = ",omitempty"
			}

			if v.Description != "" {
				pf("\t// %s: %s\n", k, v.Description)
			}
			pf("\t%s %s%s `json:\"%s%s\" cborgen:\"%s%s\"`\n", goname, ptr, tname, k, jsonOmit, k, cborOmit)
			return nil
		}); err != nil {
			return err
		}

		pf("}\n\n")

	case "array":
		tname, err := ts.typeNameForField(name, "elem", *ts.Items)
		if err != nil {
			return err
		}

		pf("type %s []%s\n", name, tname)

	case "union":
		if len(ts.Refs) > 0 {
			pf("type %s struct {\n", name)
			for _, r := range ts.Refs {
				tname := ts.typeNameFromRef(r)
				pf("\t%s *%s\n", tname, tname)
			}
			pf("}\n\n")
		}
	default:
		return fmt.Errorf("%s has unrecognized type type %s", name, ts.Type)
	}

	return nil
}

func (ts *TypeSchema) writeTypeMethods(name string, w io.Writer) error {
	switch ts.Type {
	case "string", "float", "array", "boolean", "integer":
		return nil
	case "object":
		if err := ts.writeJsonMarshalerObject(name, w); err != nil {
			return err
		}

		if err := ts.writeJsonUnmarshalerObject(name, w); err != nil {
			return err
		}

		return nil
	case "union":
		if len(ts.Refs) > 0 {
			reft, err := ts.lookupRef(ts.Refs[0])
			if err != nil {
				return err
			}

			if reft.Type == "string" {
				return nil
			}

			if err := ts.writeJsonMarshalerEnum(name, w); err != nil {
				return err
			}

			if err := ts.writeJsonUnmarshalerEnum(name, w); err != nil {
				return err
			}

			if ts.needsCbor {
				if err := ts.writeCborMarshalerEnum(name, w); err != nil {
					return err
				}

				if err := ts.writeCborUnmarshalerEnum(name, w); err != nil {
					return err
				}
			}

			return nil
		}

		return fmt.Errorf("%q unsupported for marshaling", name)
	default:
		return fmt.Errorf("%q has unrecognized type type %s", name, ts.Type)
	}
}

func forEachProp(t TypeSchema, cb func(k string, ts TypeSchema) error) error {
	var keys []string
	for k := range t.Properties {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		subv := t.Properties[k]

		if err := cb(k, *subv); err != nil {
			return err
		}
	}
	return nil
}

func (ts *TypeSchema) writeJsonMarshalerObject(name string, w io.Writer) error {
	return nil // no need for a special json marshaler right now
}

func (ts *TypeSchema) writeJsonMarshalerEnum(name string, w io.Writer) error {
	pf := printerf(w)
	pf("func (t *%s) MarshalJSON() ([]byte, error) {\n", name)

	for _, e := range ts.Refs {
		tname := ts.typeNameFromRef(e)
		if strings.HasPrefix(e, "#") {
			e = ts.id + e
		}

		pf("\tif t.%s != nil {\n", tname)
		pf("\tt.%s.LexiconTypeID = %q\n", tname, e)
		pf("\t\treturn json.Marshal(t.%s)\n\t}\n", tname)
	}

	pf("\treturn nil, fmt.Errorf(\"cannot marshal empty enum\")\n}\n")
	return nil
}

func (s *TypeSchema) writeJsonUnmarshalerObject(name string, w io.Writer) error {
	// TODO: would be nice to add some validation...
	return nil
	//pf("func (t *%s) UnmarshalJSON(b []byte) (error) {\n", name)
}

func (ts *TypeSchema) getTypeConstValueForType(ref string) (any, error) {
	rr, err := ts.lookupRef(ref)
	if err != nil {
		return nil, err
	}

	reft, ok := rr.Properties["type"]
	if !ok {
		return nil, nil
	}

	return reft.Const, nil
}

func (ts *TypeSchema) writeJsonUnmarshalerEnum(name string, w io.Writer) error {
	pf := printerf(w)
	pf("func (t *%s) UnmarshalJSON(b []byte) (error) {\n", name)
	pf("\ttyp, err := util.TypeExtract(b)\n")
	pf("\tif err != nil {\n\t\treturn err\n\t}\n\n")
	pf("\tswitch typ {\n")
	for _, e := range ts.Refs {
		if strings.HasPrefix(e, "#") {
			e = ts.id + e
		}

		goname := ts.typeNameFromRef(e)

		pf("\t\tcase \"%s\":\n", e)
		pf("\t\t\tt.%s = new(%s)\n", goname, goname)
		pf("\t\t\treturn json.Unmarshal(b, t.%s)\n", goname)
	}

	if ts.Closed {
		pf(`
			default:
				return fmt.Errorf("closed enums must have a matching value")
		`)
	} else {
		pf(`
			default:
				return nil
		`)

	}

	pf("\t}\n")
	pf("}\n\n")

	return nil
}

func (ts *TypeSchema) writeCborMarshalerEnum(name string, w io.Writer) error {
	pf := printerf(w)
	pf("func (t *%s) MarshalCBOR(w io.Writer) error {\n", name)
	pf(`
	if t == nil {
		_, err := w.Write(cbg.CborNull)
		return err
	}
`)

	for _, e := range ts.Refs {
		tname := ts.typeNameFromRef(e)
		pf("\tif t.%s != nil {\n", tname)
		pf("\t\treturn t.%s.MarshalCBOR(w)\n\t}\n", tname)
	}

	pf("\treturn fmt.Errorf(\"cannot cbor marshal empty enum\")\n}\n")
	return nil
}

func (ts *TypeSchema) writeCborUnmarshalerEnum(name string, w io.Writer) error {
	pf := printerf(w)
	pf("func (t *%s) UnmarshalCBOR(r io.Reader) error {\n", name)
	pf("\ttyp, b, err := util.CborTypeExtractReader(r)\n")
	pf("\tif err != nil {\n\t\treturn err\n\t}\n\n")
	pf("\tswitch typ {\n")
	for _, e := range ts.Refs {
		if strings.HasPrefix(e, "#") {
			e = ts.id + e
		}

		goname := ts.typeNameFromRef(e)

		pf("\t\tcase \"%s\":\n", e)
		pf("\t\t\tt.%s = new(%s)\n", goname, goname)
		pf("\t\t\treturn t.%s.UnmarshalCBOR(bytes.NewReader(b))\n", goname)
	}

	if ts.Closed {
		pf(`
			default:
				return fmt.Errorf("closed enums must have a matching value")
		`)
	} else {
		pf(`
			default:
				return nil
		`)

	}

	pf("\t}\n")
	pf("}\n\n")

	return nil
}
