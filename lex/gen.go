package lex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"html/template"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	EncodingCBOR = "application/cbor"
	EncodingJSON = "application/json"
)

type Schema struct {
	prefix string

	Lexicon int                    `json:"lexicon"`
	ID      string                 `json:"id"`
	Defs    map[string]*TypeSchema `json:"defs"`
}

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
	prefix  string
	id      string
	defName string
	defMap  map[string]*ExtDef

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
	Properties map[string]*TypeSchema `json:"properties"`
	MaxLength  int                    `json:"maxLength"`
	Items      *TypeSchema            `json:"items"`
	Const      any                    `json:"const"`
	Enum       []string               `json:"enum"`
	Closed     bool                   `json:"closed"`
}

func (s *Schema) Name() string {
	p := strings.Split(s.ID, ".")
	return p[len(p)-2] + p[len(p)-1]
}

type outputType struct {
	Name    string
	DefName string
	Type    *TypeSchema
	Record  bool
}

func (s *Schema) AllTypes(prefix string, defMap map[string]*ExtDef) []outputType {
	var out []outputType

	var walk func(name string, ts *TypeSchema, record bool)
	walk = func(name string, ts *TypeSchema, record bool) {
		if ts == nil {
			panic(fmt.Sprintf("nil type schema in %q (%s)", name, s.ID))

		}
		ts.prefix = prefix
		ts.id = s.ID
		ts.defMap = defMap
		if ts.Type == "object" ||
			(ts.Type == "union" && len(ts.Refs) > 0) {
			out = append(out, outputType{
				Name:   name,
				Type:   ts,
				Record: record,
			})
		}

		for childname, val := range ts.Properties {
			walk(name+"_"+strings.Title(childname), val, false)
		}

		if ts.Items != nil {
			walk(name+"_Elem", ts.Items, false)
		}

		if ts.Input != nil {
			if ts.Input.Schema == nil {
				if ts.Input.Encoding != "application/cbor" {
					panic(fmt.Sprintf("strange input type def in %s", s.ID))
				}
			} else {
				walk(name+"_Input", ts.Input.Schema, false)
			}
		}

		if ts.Output != nil {
			if ts.Output.Schema == nil {
				if ts.Output.Encoding != "application/cbor" {
					panic(fmt.Sprintf("strange output type def in %s", s.ID))
				}
			} else {
				walk(name+"_Output", ts.Output.Schema, false)
			}
		}

		if ts.Type == "record" {
			walk(name, ts.Record, false)
		}

	}

	tname := nameFromID(s.ID, prefix)

	for name, def := range s.Defs {
		n := tname + "_" + strings.Title(name)
		if name == "main" {
			n = tname
		}
		walk(n, def, false)
	}

	return out
}

func ReadSchema(f string) (*Schema, error) {
	fi, err := os.Open(f)
	if err != nil {
		return nil, err
	}

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
			fmt.Println("def map: ", n)
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

func GenCodeForSchema(pkg string, prefix string, fname string, reqcode bool, s *Schema, defmap map[string]*ExtDef, imports map[string]string) error {
	buf := new(bytes.Buffer)

	s.prefix = prefix
	for _, d := range s.Defs {
		fmt.Println("def id: ", d.id)
		d.prefix = prefix
	}

	fmt.Fprintf(buf, "package %s\n\n", pkg)
	fmt.Fprintf(buf, "import (\n")
	fmt.Fprintf(buf, "\t\"context\"\n")
	fmt.Fprintf(buf, "\t\"fmt\"\n")
	fmt.Fprintf(buf, "\t\"encoding/json\"\n")
	fmt.Fprintf(buf, "\t\"github.com/whyrusleeping/gosky/xrpc\"\n")
	fmt.Fprintf(buf, "\t\"github.com/whyrusleeping/gosky/lex/util\"\n")
	for k, v := range imports {
		if k != prefix {
			fmt.Fprintf(buf, "\t%s %q\n", importNameForPrefix(k), v)
		}
	}
	fmt.Fprintf(buf, ")\n\n")
	fmt.Fprintf(buf, "// schema: %s\n\n", s.ID)

	tps := s.AllTypes(prefix, defmap)

	for _, ot := range tps {
		if err := ot.Type.WriteType(ot.Name, buf); err != nil {
			return err
		}
	}

	if reqcode {
		name := nameFromID(s.ID, prefix)
		if err := writeMethods(name, s.Defs["main"], buf); err != nil {
			return err
		}
	}

	if err := writeCodeFile(buf.Bytes(), fname); err != nil {
		return err
	}

	return nil
}

func writeCodeFile(b []byte, fname string) error {
	formatted, err := format.Source(b)
	if err != nil {
		fmt.Println(string(b))
		return fmt.Errorf("failed to format generated file: %w", err)
	}

	fixed, err := fixImports(formatted)
	if err != nil {
		return err
	}

	if err := os.WriteFile(fname, fixed, 0664); err != nil {
		return err
	}

	return nil
}

func fixImports(b []byte) ([]byte, error) {
	cmd := exec.Command("goimports")

	cmd.Stdin = bytes.NewReader(b)
	buf := new(bytes.Buffer)
	cmd.Stdout = buf

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func writeMethods(typename string, ts *TypeSchema, w io.Writer) error {
	switch ts.Type {
	case "token":
		fmt.Fprintf(w, "const %s = %q\n", typename, ts.id+"#"+ts.defName)
		return nil
	case "record":
		return nil
	case "query":
		return ts.WriteRPC(w, typename)
	case "procedure":
		return ts.WriteRPC(w, typename)
	case "object":
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

func orderedMapIter[T any](m map[string]*T, cb func(string, T) error) error {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		if err := cb(k, *m[k]); err != nil {
			return err
		}
	}
	return nil
}

func (s *TypeSchema) WriteRPC(w io.Writer, typename string) error {
	fname := typename

	params := "ctx context.Context, c *xrpc.Client"
	inpvar := "nil"
	inpenc := ""

	if s.Input != nil {
		inpvar = "input"
		inpenc = s.Input.Encoding
		switch s.Input.Encoding {
		case EncodingCBOR:
			params = fmt.Sprintf("%s, input io.Reader", params)
		case EncodingJSON:
			params = fmt.Sprintf("%s, input %s_Input", params, fname)
		default:
			return fmt.Errorf("unsupported input encoding: %q", s.Input.Encoding)
		}
	}

	if s.Parameters != nil {
		if err := orderedMapIter[TypeSchema](s.Parameters.Properties, func(name string, t TypeSchema) error {
			tn, err := s.typeNameForField(name, "", t)
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
		case EncodingCBOR:
			out = "([]byte, error)"
		case EncodingJSON:
			out = fmt.Sprintf("(*%s_Output, error)", fname)
		default:
			return fmt.Errorf("unrecognized encoding scheme: %q", s.Output.Encoding)
		}
	}

	fmt.Fprintf(w, "func %s(%s) %s {\n", fname, params, out)

	outvar := "nil"
	errRet := "err"
	outRet := "nil"
	if s.Output != nil {
		switch s.Output.Encoding {
		case EncodingCBOR:
			fmt.Fprintf(w, "buf := new(bytes.Buffer)\n")
			outvar = "buf"
			errRet = "nil, err"
			outRet = "buf.Bytes(), nil"

		case EncodingJSON:
			fmt.Fprintf(w, "\tvar out %s_Output\n", fname)
			outvar = "&out"
			errRet = "nil, err"
			outRet = "&out, nil"
		default:
			return fmt.Errorf("unrecognized output encoding: %q", s.Output.Encoding)
		}
	}

	queryparams := "nil"
	if s.Parameters != nil {
		queryparams = "params"
		fmt.Fprintf(w, `
	params := map[string]interface{}{
`)
		if err := orderedMapIter[TypeSchema](s.Parameters.Properties, func(name string, t TypeSchema) error {
			fmt.Fprintf(w, `"%s": %s,
`, name, name)
			return nil
		}); err != nil {
			return err
		}
		fmt.Fprintf(w, "}\n")
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

	fmt.Fprintf(w, "\tif err := c.Do(ctx, %s, %q, \"%s\", %s, %s, %s); err != nil {\n", reqtype, inpenc, s.id, queryparams, inpvar, outvar)
	fmt.Fprintf(w, "\t\treturn %s\n", errRet)
	fmt.Fprintf(w, "\t}\n\n")
	fmt.Fprintf(w, "\treturn %s\n", outRet)
	fmt.Fprintf(w, "}\n\n")

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
	fmt.Fprintf(w, "package %s\n\n", pkg)
	fmt.Fprintf(w, "import (\n")
	fmt.Fprintf(w, "\t\"context\"\n")
	fmt.Fprintf(w, "\t\"fmt\"\n")
	fmt.Fprintf(w, "\t\"encoding/json\"\n")
	fmt.Fprintf(w, "\t\"github.com/whyrusleeping/gosky/xrpc\"\n")
	for k, v := range impmap {
		fmt.Fprintf(w, "\t%s\"%s\"\n", importNameForPrefix(k), v)
	}
	fmt.Fprintf(w, ")\n\n")


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
			return fmt.Errorf("schema %q doesnt have a main def", s.ID)
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
	fmt.Fprintf(w, "package %s\n\n", pkg)
	fmt.Fprintf(w, "import (\n")
	fmt.Fprintf(w, "\t\"context\"\n")
	fmt.Fprintf(w, "\t\"fmt\"\n")
	fmt.Fprintf(w, "\t\"encoding/json\"\n")
	fmt.Fprintf(w, "\t\"github.com/whyrusleeping/gosky/xrpc\"\n")
	fmt.Fprintf(w, "\t\"github.com/labstack/echo/v4\"\n")
	for k, v := range impmap {
		fmt.Fprintf(w, "\t%s\"%s\"\n", importNameForPrefix(k), v)
	}
	fmt.Fprintf(w, ")\n\n")

	fmt.Fprintf(w, "func (s *Server) RegisterHandlers(e echo.Echo) error {\n")
	for _, s := range schemas {

		main, ok := s.Defs["main"]
		if !ok {
			return fmt.Errorf("schema %q has no main", s.ID)
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

		fmt.Fprintf(w, "e.%s(\"/xrpc/%s\", s.Handle%s)\n", verb, s.ID, idToTitle(s.ID))
	}

	fmt.Fprintf(w, "return nil\n}\n\n")

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
			return fmt.Errorf("schema %q doesnt have a main def", s.ID)
		}

		if main.Type == "procedure" || main.Type == "query" {
			fname := idToTitle(s.ID)
			tname := nameFromID(s.ID, prefix)
			impname := importNameForPrefix(prefix)
			if err := main.WriteRPCHandler(w, fname, tname, impname); err != nil {
				return err
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
	paramtypes := []string{"ctx context.Context"}
	if s.Type == "query" {
		if s.Parameters != nil {
			orderedMapIter[TypeSchema](s.Parameters.Properties, func(k string, t TypeSchema) error {
				switch t.Type {
				case "string":
					paramtypes = append(paramtypes, k+" string")
				case "integer":
					paramtypes = append(paramtypes, k+" int")
				case "number":
					return fmt.Errorf("non-integer numbers currently unsupported")
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
			returndef = fmt.Sprintf("(*%s.%s_Output, error)", impname, shortname)
		case "application/cbor":
			returndef = fmt.Sprintf("([]byte, error)" )
		default:
			return fmt.Errorf("unsupported encoding: %q", s.Output.Encoding)
		}
	}

	fmt.Fprintf(w, "func (s *Server) handle%s(%s) %s\n", fname, strings.Join(paramtypes, ","), returndef)

	return nil
}

func (s *TypeSchema) WriteRPCHandler(w io.Writer, fname, shortname, impname string) error {
	tname := shortname

	fmt.Fprintf(w, "func (s *Server) Handle%s(c echo.Context) error {\n", fname)

	fmt.Fprintf(w, "ctx, span := otel.Tracer(\"server\").Start(c.Request().Context(), %q)\n", "Handle"+fname)
	fmt.Fprintf(w, "defer span.End()\n")

	paramtypes := []string{"ctx context.Context"}
	params := []string{"ctx"}
	if s.Type == "query" {
		if s.Parameters != nil {
			orderedMapIter[TypeSchema](s.Parameters.Properties, func(k string, t TypeSchema) error {
				switch t.Type {
				case "string":
					params = append(params, k)
					paramtypes = append(paramtypes, k+" string")
					fmt.Fprintf(w, "%s := c.QueryParam(\"%s\")\n", k, k)
				case "integer":
					params = append(params, k)
					paramtypes = append(paramtypes, k+" int")
					fmt.Fprintf(w, `
%s, err := strconv.Atoi(c.QueryParam("%s"))
if err != nil {
	return err
}
`, k, k)
				case "number":
					return fmt.Errorf("non-integer numbers currently unsupported")
				default:
					return fmt.Errorf("unsupported handler parameter type: %s", t.Type)
				}
				return nil
			})
		}
	} else if s.Type == "procedure" {
		fmt.Fprintf(w, `
var body %s.%s
if err := c.Bind(&body); err != nil {
	return err
}
`, impname, tname+"_Input")
	} else {
		return fmt.Errorf("can only generate handlers for queries or procedures")
	}

	assign := "handleErr"
	returndef := "error"
	if s.Output != nil {
		assign = "out, handleErr"
		fmt.Fprintf(w, "var out *%s.%s\n", impname, tname+"_Output")
		returndef = fmt.Sprintf("(*%s.%s_Output, error)", impname, tname)
	}
	fmt.Fprintf(w, "var handleErr error\n")
	fmt.Fprintf(w, "// func (s *Server) handle%s(%s) %s\n", fname, strings.Join(paramtypes, ","), returndef)
	fmt.Fprintf(w, "%s = s.handle%s(%s)\n", assign, fname, strings.Join(params, ","))
	fmt.Fprintf(w, "if handleErr != nil {\nreturn handleErr\n}\n")

	if s.Output != nil {
	fmt.Fprintf(w, "return c.JSON(200, out)\n}\n\n")
} else {
	fmt.Fprintf(w, "return nil\n}\n\n")
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
	case "number":
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
		return "any", nil
	case "union":
		return "*" + name + "_" + strings.Title(k), nil
	case "image":
		return "*util.Blob", nil
	case "blob":
		return "*util.Blob", nil
	case "array":
		subt, err := s.typeNameForField(name+"_"+strings.Title(k), "Elem", *v.Items)
		if err != nil {
			return "", err
		}

		return "[]" + subt, nil
	default:
		return "", fmt.Errorf("field %q in %s has unsupported type name", k, name)
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
	switch ts.Type {
	case "string":
		// TODO: deal with max length
		fmt.Fprintf(w, "type %s string\n", name)
	case "number":
		fmt.Fprintf(w, "type %s float64\n", name)
	case "integer":
		fmt.Fprintf(w, "type %s int64\n", name)
	case "boolean":
		fmt.Fprintf(w, "type %s bool\n", name)
	case "object":
		if len(ts.Properties) == 0 {
			fmt.Fprintf(w, "type %s interface{}\n", name)
			return nil
		}

		fmt.Fprintf(w, "type %s struct {\n", name)

		for k, v := range ts.Properties {
			goname := strings.Title(k)

			tname, err := ts.typeNameForField(name, k, *v)
			if err != nil {
				return err
			}

			fmt.Fprintf(w, "\t%s %s `json:\"%s\"`\n", goname, tname, k)
		}
		fmt.Fprintf(w, "}\n\n")

	case "array":
		tname, err := ts.typeNameForField(name, "elem", *ts.Items)
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "type %s []%s\n", name, tname)

	case "union":
		if len(ts.Refs) > 0 {
			fmt.Fprintf(w, "type %s struct {\n", name)
			for _, r := range ts.Refs {
				tname := ts.typeNameFromRef(r)
				fmt.Fprintf(w, "\t%s *%s\n", tname, tname)
			}
			fmt.Fprintf(w, "}\n\n")
		}
	default:
		return fmt.Errorf("%s has unrecognized type type %s", name, ts.Type)
	}

	return nil
}

func (ts *TypeSchema) writeTypeMethods(name string, w io.Writer) error {
	switch ts.Type {
	case "string", "number", "array", "boolean", "integer":
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
	if len(ts.Properties) == 0 {
		// TODO: this is a hacky special casing of record types...
		return nil
	}

	fmt.Fprintf(w, "func (t *%s) MarshalJSON() ([]byte, error) {\n", name)

	if err := forEachProp(*ts, func(k string, ts TypeSchema) error {
		if ts.Const != nil {
			// TODO: maybe check for mutations before overwriting? mutations would mean bad code
			switch ts.Const.(type) {
			case string:
				fmt.Fprintf(w, "\tt.%s = %q\n", strings.Title(k), ts.Const)
			case bool:
				fmt.Fprintf(w, "\tt.%s = %v\n", strings.Title(k), ts.Const)
			default:
				return fmt.Errorf("unsupported const type: %T", ts.Const)

			}
		}

		return nil
	}); err != nil {
		return err
	}

	// TODO: this is ugly since i can't just pass things through to json.Marshal without causing an infinite recursion...
	fmt.Fprintf(w, "\tout := make(map[string]interface{})\n")
	if err := forEachProp(*ts, func(k string, ts TypeSchema) error {
		fmt.Fprintf(w, "\tout[%q] = t.%s\n", k, strings.Title(k))
		return nil
	}); err != nil {
		return err
	}

	fmt.Fprintf(w, "\treturn json.Marshal(out)\n}\n\n")
	return nil
}

func (ts *TypeSchema) writeJsonMarshalerEnum(name string, w io.Writer) error {
	fmt.Fprintf(w, "func (t *%s) MarshalJSON() ([]byte, error) {\n", name)

	for _, e := range ts.Refs {
		tname := ts.typeNameFromRef(e)
		fmt.Fprintf(w, "\tif t.%s != nil {\n", tname)
		fmt.Fprintf(w, "\t\treturn json.Marshal(t.%s)\n\t}\n", tname)
	}

	fmt.Fprintf(w, "\treturn nil, fmt.Errorf(\"cannot marshal empty enum\")\n}\n")
	return nil
}

func (s *TypeSchema) writeJsonUnmarshalerObject(name string, w io.Writer) error {
	// TODO: would be nice to add some validation...
	return nil
	//fmt.Fprintf(w, "func (t *%s) UnmarshalJSON(b []byte) (error) {\n", name)
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
	fmt.Fprintf(w, "func (t *%s) UnmarshalJSON(b []byte) (error) {\n", name)
	fmt.Fprintf(w, "\ttyp, err := util.EnumTypeExtract(b)\n")
	fmt.Fprintf(w, "\tif err != nil {\n\t\treturn err\n\t}\n\n")
	fmt.Fprintf(w, "\tswitch typ {\n")
	for _, e := range ts.Refs {
		if strings.HasPrefix(e, "#") {
			e = ts.id + e
		}


		goname := ts.typeNameFromRef(e)

		fmt.Fprintf(w, "\t\tcase \"%s\":\n", e)
		fmt.Fprintf(w, "\t\t\tt.%s = new(%s)\n", goname, goname)
		fmt.Fprintf(w, "\t\t\treturn json.Unmarshal(b, t.%s)\n", goname)
	}

	if ts.Closed {
		fmt.Fprintf(w, `
			default:
				return fmt.Errorf("closed enums must have a matching value")
		`)
	} else {
		fmt.Fprintf(w, `
			default:
				return nil
		`)

	}

	fmt.Fprintf(w, "\t}\n")
	fmt.Fprintf(w, "}\n\n")

	return nil
}
