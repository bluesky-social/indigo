package main

import (
	"fmt"
	"sort"

	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Re-structured version of `lexicon.SchemaFile` which is much easier to render as documentation.
type Def struct {
	NSID        syntax.NSID
	Type        string
	Description *string
	Name        string // def fragment; or field name

	//ViaRef string // if this was included via reference

	// objects, records, API endpoints, etc
	Fields []Def
	Errors []lexicon.SchemaError

	// API methods: query, procedure
	QueryParams    []Def
	Output         *Def
	OutputEncoding string
	Input          *Def
	InputEncoding  string

	// record
	KeyType string

	// unions
	// TODO: transclude these in? with some depth limit?
	Options []string
	Closed  bool

	// array
	Items *Def

	// fields
	Required bool
	Nullable bool

	// ref
	Ref string

	Min *int
	Max *int

	// concrete types
	SchemaBoolean *lexicon.SchemaBoolean
	SchemaInteger *lexicon.SchemaInteger
	SchemaString  *lexicon.SchemaString
	SchemaBytes   *lexicon.SchemaBytes
	SchemaBlob    *lexicon.SchemaBlob
}

func sortedKeys(m map[string]lexicon.SchemaDef) []string {
	keys := make([]string, len(m))
	i := 0
	for k, _ := range m {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	return keys
}

func ParseSchemaFile(sf *lexicon.SchemaFile, nsid syntax.NSID) ([]Def, error) {
	out := make([]Def, len(sf.Defs))
	i := 0
	mainIndex := -1
	for _, name := range sortedKeys(sf.Defs) {
		sd := sf.Defs[name]
		if name == "main" {
			mainIndex = i
		}
		doc, err := ParseSchemaDef(sd.Inner, nsid, name)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", name, err)
		}
		out[i] = *doc
		i++
	}
	// sort 'main' to front (if it exists)
	// TODO: have this not mess up other sort order
	if mainIndex > 0 {
		out[0], out[mainIndex] = out[mainIndex], out[0]
	}
	return out, nil
}

func stringInList(list []string, val string) bool {
	for _, v := range list {
		if v == val {
			return true
		}
	}
	return false
}

func ParseFields(properties map[string]lexicon.SchemaDef, required []string, nullable []string, nsid syntax.NSID) ([]Def, error) {
	out := []Def{}
	// TODO: sort keys
	for _, name := range sortedKeys(properties) {
		def, err := ParseSchemaDef(properties[name].Inner, nsid, name)
		if err != nil {
			return nil, err
		}
		if stringInList(required, name) {
			def.Required = true
		}
		if stringInList(nullable, name) {
			def.Nullable = true
		}
		out = append(out, *def)
	}
	return out, nil
}

func ParseSchemaDef(raw any, nsid syntax.NSID, name string) (*Def, error) {
	def := Def{
		NSID: nsid,
		Name: name,
	}
	if raw == nil {
		return nil, fmt.Errorf("nil SchemaDef")
	}
	switch s := raw.(type) {
	case lexicon.SchemaRecord:
		def.Type = "record"
		def.Description = s.Description
		def.KeyType = s.Key
		fields, err := ParseFields(s.Record.Properties, s.Record.Required, s.Record.Nullable, nsid)
		if err != nil {
			return nil, err
		}
		def.Fields = fields
	case lexicon.SchemaQuery:
		def.Type = "query"
		def.Description = s.Description
		def.Errors = s.Errors
		qp, err := ParseFields(s.Parameters.Properties, s.Parameters.Required, []string{}, nsid)
		if err != nil {
			return nil, err
		}
		def.Fields = qp
		if s.Output != nil {
			def.OutputEncoding = s.Output.Encoding
			if s.Output.Schema != nil {
				def.Output, err = ParseSchemaDef(s.Output.Schema.Inner, nsid, "")
				if err != nil {
					return nil, err
				}
			}
		}
	case lexicon.SchemaProcedure:
		def.Type = "procedure"
		def.Description = s.Description
		def.Errors = s.Errors
		qp, err := ParseFields(s.Parameters.Properties, s.Parameters.Required, []string{}, nsid)
		if err != nil {
			return nil, err
		}
		def.Fields = qp
		if s.Output != nil {
			def.OutputEncoding = s.Output.Encoding
			if s.Output.Schema != nil {
				def.Output, err = ParseSchemaDef(s.Output.Schema.Inner, nsid, "")
				if err != nil {
					return nil, err
				}
			}
		}
		if s.Input != nil {
			def.InputEncoding = s.Input.Encoding
			if s.Input.Schema != nil {
				def.Input, err = ParseSchemaDef(s.Input.Schema.Inner, nsid, "")
				if err != nil {
					return nil, err
				}
			}
		}
	case lexicon.SchemaSubscription:
		def.Type = "subscription"
		def.Description = s.Description
		qp, err := ParseFields(s.Parameters.Properties, s.Parameters.Required, []string{}, nsid)
		if err != nil {
			return nil, err
		}
		def.Fields = qp
		if s.Message == nil {
			return nil, fmt.Errorf("empty subscription message type")
		}
		u, ok := s.Message.Schema.Inner.(lexicon.SchemaUnion)
		if !ok {
			return nil, fmt.Errorf("subscription message must be a union")
		}
		def.Closed = u.Closed != nil && *u.Closed
		def.Options = u.Refs

	case lexicon.SchemaBoolean:
		def.Type = "boolean"
		def.Description = s.Description
		def.SchemaBoolean = &s
	case lexicon.SchemaInteger:
		def.Type = "integer"
		def.Description = s.Description
		def.SchemaInteger = &s
	case lexicon.SchemaString:
		def.Type = "string"
		def.Description = s.Description
		def.SchemaString = &s
	case lexicon.SchemaBytes:
		def.Type = "bytes"
		def.Description = s.Description
		def.SchemaBytes = &s
	case lexicon.SchemaBlob:
		def.Type = "blob"
		def.Description = s.Description
		def.SchemaBlob = &s
	case lexicon.SchemaNull:
		def.Type = "null"
		def.Description = s.Description
	case lexicon.SchemaCIDLink:
		def.Type = "cid-link"
		def.Description = s.Description
	case lexicon.SchemaArray:
		def.Type = "array"
		def.Description = s.Description
		def.Min = s.MinLength
		def.Max = s.MaxLength
		d, err := ParseSchemaDef(s.Items.Inner, nsid, "")
		if err != nil {
			return nil, err
		}
		def.Items = d
	case lexicon.SchemaObject:
		def.Type = "object"
		def.Description = s.Description
		f, err := ParseFields(s.Properties, s.Required, s.Nullable, nsid)
		if err != nil {
			return nil, err
		}
		def.Fields = f

	case lexicon.SchemaToken:
		def.Type = "token"
		def.Description = s.Description
	case lexicon.SchemaRef:
		def.Type = "ref"
		def.Description = s.Description
		def.Ref = s.Ref
	case lexicon.SchemaUnion:
		def.Type = "union"
		def.Description = s.Description
		def.Closed = s.Closed != nil && *s.Closed
		def.Options = s.Refs
	case lexicon.SchemaUnknown:
		def.Type = "unknown"
		def.Description = s.Description
	default:
		return nil, fmt.Errorf("unhandled SchemaDef type: %T", s)
	}
	return &def, nil
}
