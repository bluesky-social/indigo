package lex

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

type Schema struct {
	Lexicon     int                   `json:"lexicon"`
	ID          string                `json:"id"`
	Type        string                `json:"type"`
	Key         string                `json:"key"`
	Description string                `json:"description"`
	Parameters  map[string]Param      `json:"parameters"`
	Input       *InputType            `json:"input"`
	Output      *OutputType           `json:"output"`
	Defs        map[string]TypeSchema `json:"defs"`
	Record      *TypeSchema           `json:"record"`
}

type Param struct {
	Type     string `json:"type"`
	Maximum  int    `json:"maximum"`
	Required bool   `json:"required"`
}

type OutputType struct {
	Encoding string     `json:"encoding"`
	Schema   TypeSchema `json:"schema"`
}

type InputType struct {
	Encoding string     `json:"encoding"`
	Schema   TypeSchema `json:"schema"`
}

type TypeSchema struct {
	Type       string                `json:"type"`
	Ref        string                `json:"$ref"`
	Required   []string              `json:"required"`
	Properties map[string]TypeSchema `json:"properties"`
	MaxLength  int                   `json:"maxLength"`
	Items      *TypeSchema           `json:"items"`
	OneOf      []TypeSchema          `json:"oneOf"`
	Const      *string               `json:"const"`
}

func (s *Schema) Name() string {
	p := strings.Split(s.ID, ".")
	return p[len(p)-1]
}

type outputType struct {
	Name string
	Type TypeSchema
}

func (s *Schema) AllTypes(prefix string) []outputType {
	var out []outputType

	var walk func(name string, ts TypeSchema)
	walk = func(name string, ts TypeSchema) {
		if ts.Type == "object" ||
			(ts.Type == "" && len(ts.OneOf) > 0) {
			out = append(out, outputType{
				Name: name,
				Type: ts,
			})
		}

		for childname, val := range ts.Properties {
			walk(name+"_"+strings.Title(childname), val)
		}

		if ts.Items != nil {
			walk(name+"_Elem", *ts.Items)
		}
	}

	for name, def := range s.Defs {
		walk(name, def)
	}

	tname := nameFromID(s.ID, prefix)

	if s.Input != nil {
		walk(tname+"_Input", s.Input.Schema)
	}
	if s.Output != nil {
		walk(tname+"_Output", s.Output.Schema)
	}

	if s.Type == "record" {
		walk(tname, *s.Record)
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

func GenCodeForSchema(pkg string, prefix string, fname string, reqcode bool, s *Schema) error {
	fi, err := os.Create(fname)
	if err != nil {
		return err
	}
	defer fi.Close()

	fmt.Fprintf(fi, "package %s\n\n", pkg)
	fmt.Fprintf(fi, "import (\n")
	fmt.Fprintf(fi, "\t\"context\"\n")
	fmt.Fprintf(fi, "\t\"fmt\"\n")
	fmt.Fprintf(fi, "\t\"encoding/json\"\n")
	fmt.Fprintf(fi, "\t\"github.com/whyrusleeping/gosky/xrpc\"\n")
	fmt.Fprintf(fi, "\t\"github.com/whyrusleeping/gosky/lex/util\"\n")
	fmt.Fprintf(fi, ")\n\n")

	tps := s.AllTypes(prefix)

	for _, ot := range tps {
		if err := WriteType(ot.Name, ot.Type, fi); err != nil {
			return err
		}
	}

	if !reqcode {
		return nil
	}
	switch s.Type {
	case "token":
		n := nameFromID(s.ID, prefix)
		fmt.Fprintf(fi, "const %s = %q\n", n, s.ID)
		return nil
	case "record":
		return nil
	case "query":
		return WriteRPC(fi, prefix, s)
	case "procedure":
		return WriteRPC(fi, prefix, s)
	default:
		return fmt.Errorf("unrecognized lexicon type %q", s.Type)
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

func WriteRPC(w io.Writer, prefix string, s *Schema) error {
	fname := nameFromID(s.ID, prefix)

	params := "ctx context.Context, c *xrpc.Client"
	inpvar := "nil"
	if s.Input != nil {
		params = fmt.Sprintf("%s, input %s_Input", params, fname)
		inpvar = "input"
	}

	out := "error"
	if s.Output != nil {
		out = fmt.Sprintf("(*%s_Output, error)", fname)
	}

	fmt.Fprintf(w, "func %s(%s) %s {\n", fname, params, out)

	outvar := "nil"
	errRet := "err"
	outRet := "nil"
	if s.Output != nil {
		fmt.Fprintf(w, "\tvar out %s_Output\n", fname)
		outvar = "&out"
		errRet = "nil, err"
		outRet = "&out, nil"
	}

	queryparams := "nil"

	var reqtype string
	switch s.Type {
	case "procedure":
		reqtype = "xrpc.Procedure"
	case "query":
		reqtype = "xrpc.Query"
	default:
		return fmt.Errorf("can only generate RPC for Query or Procedure (got %s)", s.Type)
	}

	fmt.Fprintf(w, "\tif err := c.Do(ctx, %s, \"%s\", %s, %s, %s); err != nil {\n", reqtype, s.ID, queryparams, inpvar, outvar)
	fmt.Fprintf(w, "\t\treturn %s\n", errRet)
	fmt.Fprintf(w, "\t}\n\n")

	fmt.Fprintf(w, "\treturn %s\n", outRet)
	fmt.Fprintf(w, "}\n\n")

	return nil
}

func typeNameFromRef(r string) string {
	p := strings.Split(r, "/")
	return strings.Title(p[len(p)-1])
}

func typeNameForField(name, k string, v TypeSchema) (string, error) {
	switch v.Type {
	case "string":
		return "string", nil
	case "number":
		return "int64", nil
	case "boolean":
		return "bool", nil
	case "object":
		return name + "_" + strings.Title(k), nil
	case "":
		if v.Ref != "" {
			return typeNameFromRef(v.Ref), nil
		}

		if len(v.OneOf) > 0 {
			return name + "_" + strings.Title(k), nil
		}

		if v.Const != nil {
			return "string", nil
		}

		return "", fmt.Errorf("field %q in %s does not have discernable type name", k, name)
	case "array":
		subt, err := typeNameForField(name+"_"+strings.Title(k), "", *v.Items)
		if err != nil {
			return "", err
		}

		return "[]" + subt, nil
	default:
		return "", fmt.Errorf("field %q in %s has unsupported type name", k, name)
	}
}

func WriteType(name string, t TypeSchema, w io.Writer) error {
	name = strings.Title(name)
	if err := writeTypeDefinition(name, t, w); err != nil {
		return err
	}

	if err := writeTypeMethods(name, t, w); err != nil {
		return err
	}

	return nil
}

func writeTypeDefinition(name string, t TypeSchema, w io.Writer) error {
	switch t.Type {
	case "string":
		// TODO: deal with max length
		fmt.Fprintf(w, "type %s string\n", name)
	case "number":
		fmt.Fprintf(w, "type %s int64\n", name)
	case "boolean":
		fmt.Fprintf(w, "type %s bool\n", name)
	case "object":
		if len(t.Properties) == 0 {
			fmt.Fprintf(w, "type %s interface{}\n", name)
			return nil
		}

		fmt.Fprintf(w, "type %s struct {\n", name)

		for k, v := range t.Properties {
			goname := strings.Title(k)

			tname, err := typeNameForField(name, k, v)
			if err != nil {
				return err
			}

			fmt.Fprintf(w, "\t%s %s `json:\"%s\"`\n", goname, tname, k)
		}
		fmt.Fprintf(w, "}\n\n")

	case "array":
		tname, err := typeNameForField(name, "elem", *t.Items)
		if err != nil {
			return err
		}

		fmt.Fprintf(w, "type %s []%s\n", name, tname)

	case "":
		if len(t.OneOf) > 0 {
			fmt.Fprintf(w, "type %s struct {\n", name)
			for _, e := range t.OneOf {
				// TODO: for now, asserting that all enum options are refs
				if e.Ref == "" {
					return fmt.Errorf("Enums must only contain refs")
				}
				tname := typeNameFromRef(e.Ref)
				fmt.Fprintf(w, "\t%s *%s\n", tname, tname)
			}

			fmt.Fprintf(w, "}\n\n")
		}
	default:
		return fmt.Errorf("%s has unrecognized type type %s", name, t.Type)
	}

	return nil
}

func writeTypeMethods(name string, t TypeSchema, w io.Writer) error {
	switch t.Type {
	case "string", "number", "array", "boolean":
		return nil
	case "object":
		if err := writeJsonMarshalerObject(name, t, w); err != nil {
			return err
		}

		if err := writeJsonUnmarshalerObject(name, t, w); err != nil {
			return err
		}

		return nil
	case "":
		if len(t.OneOf) > 0 {
			if err := writeJsonMarshalerEnum(name, t, w); err != nil {
				return err
			}

			if err := writeJsonUnmarshalerEnum(name, t, w); err != nil {
				return err
			}

			return nil
		}

		return fmt.Errorf("%q unsupported for marshaling", name)
	default:
		return fmt.Errorf("%q has unrecognized type type %s", name, t.Type)
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

		if err := cb(k, subv); err != nil {
			return err
		}
	}
	return nil
}

func writeJsonMarshalerObject(name string, t TypeSchema, w io.Writer) error {
	if len(t.Properties) == 0 {
		// TODO: this is a hacky special casing of record types...
		return nil
	}

	fmt.Fprintf(w, "func (t *%s) MarshalJSON() ([]byte, error) {\n", name)

	if err := forEachProp(t, func(k string, ts TypeSchema) error {
		if ts.Const != nil {
			// TODO: maybe check for mutations before overwriting? mutations would mean bad code
			fmt.Fprintf(w, "\tt.%s = %q\n", strings.Title(k), *ts.Const)
		}

		return nil
	}); err != nil {
		return err
	}

	// TODO: this is ugly since i can't just pass things through to json.Marshal without causing an infinite recursion...
	fmt.Fprintf(w, "\tout := make(map[string]interface{})\n")
	if err := forEachProp(t, func(k string, ts TypeSchema) error {
		fmt.Fprintf(w, "\tout[%q] = t.%s\n", k, strings.Title(k))
		return nil
	}); err != nil {
		return err
	}

	fmt.Fprintf(w, "\treturn json.Marshal(out)\n}\n\n")
	return nil
}

func writeJsonMarshalerEnum(name string, t TypeSchema, w io.Writer) error {
	fmt.Fprintf(w, "func (t *%s) MarshalJSON() ([]byte, error) {\n", name)

	for _, e := range t.OneOf {
		tname := typeNameFromRef(e.Ref)
		fmt.Fprintf(w, "\tif t.%s != nil {\n", tname)
		fmt.Fprintf(w, "\t\treturn json.Marshal(t.%s)\n\t}\n", tname)
	}

	fmt.Fprintf(w, "\treturn nil, fmt.Errorf(\"cannot marshal empty enum\")\n}\n")
	return nil
}

func writeJsonUnmarshalerObject(name string, t TypeSchema, w io.Writer) error {
	// TODO: would be nice to add some validation...
	return nil
	//fmt.Fprintf(w, "func (t *%s) UnmarshalJSON(b []byte) (error) {\n", name)
}

func getTypeConstValueForType(t TypeSchema) (string, error) {
	// TODO: this is WRONG
	parts := strings.Split(t.Ref, "/")
	return parts[len(parts)-1], nil
}

func writeJsonUnmarshalerEnum(name string, t TypeSchema, w io.Writer) error {
	fmt.Fprintf(w, "func (t *%s) UnmarshalJSON(b []byte) (error) {\n", name)
	fmt.Fprintf(w, "\ttyp, err := util.EnumTypeExtract(b)\n")
	fmt.Fprintf(w, "\tif err != nil {\n\t\treturn err\n\t}\n\n")
	fmt.Fprintf(w, "\tswitch typ {\n")
	for _, e := range t.OneOf {
		tc, err := getTypeConstValueForType(e)
		if err != nil {
			return err
		}

		goname := typeNameFromRef(e.Ref)

		fmt.Fprintf(w, "\t\tcase \"%s\":\n", tc)
		fmt.Fprintf(w, "\t\t\tt.%s = new(%s)\n", goname, goname)
		fmt.Fprintf(w, "\t\t\treturn json.Unmarshal(b, t.%s)\n", goname)
	}
	fmt.Fprintf(w, "\t\tdefault:\n")
	fmt.Fprintf(w, "\t\treturn fmt.Errorf(\"%%q not valid enum member type\", typ)\n")
	fmt.Fprintf(w, "\t}\n")
	fmt.Fprintf(w, "}\n\n")

	return nil
}
