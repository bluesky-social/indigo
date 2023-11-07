package lexicon

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Serialization helper for top-level Lexicon schema JSON objects (files)
type SchemaFile struct {
	Lexicon     int                  `json:"lexicon,const=1"`
	ID          string               `json:"id"`
	Revision    *int                 `json:"revision,omitempty"`
	Description *string              `json:"description,omitempty"`
	Defs        map[string]SchemaDef `json:"defs"`
}

// enum type to represent any of the schema fields
type SchemaDef struct {
	Inner any
}

func (s *SchemaDef) CheckSchema() error {
	switch v := s.Inner.(type) {
	case SchemaRecord:
		return v.CheckSchema()
	case SchemaQuery:
		return v.CheckSchema()
	case SchemaProcedure:
		return v.CheckSchema()
	case SchemaSubscription:
		return v.CheckSchema()
	case SchemaNull:
		return v.CheckSchema()
	case SchemaBoolean:
		return v.CheckSchema()
	case SchemaInteger:
		return v.CheckSchema()
	case SchemaString:
		return v.CheckSchema()
	case SchemaBytes:
		return v.CheckSchema()
	case SchemaCIDLink:
		return v.CheckSchema()
	case SchemaArray:
		return v.CheckSchema()
	case SchemaObject:
		return v.CheckSchema()
	case SchemaBlob:
		return v.CheckSchema()
	case SchemaParams:
		return v.CheckSchema()
	case SchemaToken:
		return v.CheckSchema()
	case SchemaRef:
		return v.CheckSchema()
	case SchemaUnion:
		return v.CheckSchema()
	case SchemaUnknown:
		return v.CheckSchema()
	default:
		return fmt.Errorf("unhandled schema type: %s", reflect.TypeOf(v))
	}
}

func (s SchemaDef) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Inner)
}

func (s *SchemaDef) UnmarshalJSON(b []byte) error {
	t, err := ExtractTypeJSON(b)
	if err != nil {
		return err
	}
	// TODO: should we call CheckSchema here, instead of in lexicon loading?
	switch t {
	case "record":
		v := new(SchemaRecord)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "query":
		v := new(SchemaQuery)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "procedure":
		v := new(SchemaProcedure)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "subscription":
		v := new(SchemaSubscription)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "null":
		v := new(SchemaNull)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "boolean":
		v := new(SchemaBoolean)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "integer":
		v := new(SchemaInteger)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "string":
		v := new(SchemaString)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "bytes":
		v := new(SchemaBytes)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "cid-link":
		v := new(SchemaCIDLink)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "array":
		v := new(SchemaArray)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "object":
		v := new(SchemaObject)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "blob":
		v := new(SchemaBlob)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "params":
		v := new(SchemaParams)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "token":
		v := new(SchemaToken)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "ref":
		v := new(SchemaRef)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "union":
		v := new(SchemaUnion)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	case "unknown":
		v := new(SchemaUnknown)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = *v
		return nil
	default:
		return fmt.Errorf("unexpected schema type: %s", t)
	}
	return fmt.Errorf("unexpected schema type: %s", t)
}

type SchemaRecord struct {
	Type        string       `json:"type,const=record"`
	Description *string      `json:"description,omitempty"`
	Key         string       `json:"key"`
	Record      SchemaObject `json:"record"`
}

func (s *SchemaRecord) CheckSchema() error {
	switch s.Key {
	case "tid", "any":
		// pass
	default:
		if !strings.HasPrefix(s.Key, "literal:") {
			return fmt.Errorf("invalid record key specifier: %s", s.Key)
		}
	}
	return s.Record.CheckSchema()
}

type SchemaQuery struct {
	Type        string        `json:"type,const=query"`
	Description *string       `json:"description,omitempty"`
	Parameters  SchemaParams  `json:"parameters"`
	Output      *SchemaBody   `json:"output"`
	Errors      []SchemaError `json:"errors,omitempty"` // optional
}

func (s *SchemaQuery) CheckSchema() error {
	if s.Output != nil {
		if err := s.Output.CheckSchema(); err != nil {
			return err
		}
	}
	for _, e := range s.Errors {
		if err := e.CheckSchema(); err != nil {
			return err
		}
	}
	return s.Parameters.CheckSchema()
}

type SchemaProcedure struct {
	Type        string        `json:"type,const=procedure"`
	Description *string       `json:"description,omitempty"`
	Parameters  SchemaParams  `json:"parameters"`
	Output      *SchemaBody   `json:"output"`           // optional
	Errors      []SchemaError `json:"errors,omitempty"` // optional
	Input       *SchemaBody   `json:"input"`            // optional
}

func (s *SchemaProcedure) CheckSchema() error {
	if s.Input != nil {
		if err := s.Input.CheckSchema(); err != nil {
			return err
		}
	}
	if s.Output != nil {
		if err := s.Output.CheckSchema(); err != nil {
			return err
		}
	}
	for _, e := range s.Errors {
		if err := e.CheckSchema(); err != nil {
			return err
		}
	}
	return s.Parameters.CheckSchema()
}

type SchemaSubscription struct {
	Type        string         `json:"type,const=subscription"`
	Description *string        `json:"description,omitempty"`
	Parameters  SchemaParams   `json:"parameters"`
	Message     *SchemaMessage `json:"message,omitempty"` // TODO(specs): is this really optional?
}

func (s *SchemaSubscription) CheckSchema() error {
	if s.Message != nil {
		if err := s.Message.CheckSchema(); err != nil {
			return err
		}
	}
	return s.Parameters.CheckSchema()
}

type SchemaBody struct {
	Description *string    `json:"description,omitempty"`
	Encoding    string     `json:"encoding"` // required, mimetype
	Schema      *SchemaDef `json:"schema"`   // optional; type:object, type:ref, or type:union
}

func (s *SchemaBody) CheckSchema() error {
	// TODO: any validation of encoding?
	if s.Schema != nil {
		switch s.Schema.Inner.(type) {
		case SchemaObject, SchemaRef, SchemaUnion:
			// pass
		default:
			return fmt.Errorf("body type can only have object, ref, or union schema")
		}
		if err := s.Schema.CheckSchema(); err != nil {
			return err
		}
	}
	return nil
}

type SchemaMessage struct {
	Description *string   `json:"description,omitempty"`
	Schema      SchemaDef `json:"schema"` // required; type:union only
}

func (s *SchemaMessage) CheckSchema() error {
	if _, ok := s.Schema.Inner.(SchemaUnion); !ok {
		return fmt.Errorf("message must have schema type union")
	}
	return s.Schema.CheckSchema()
}

type SchemaError struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

func (s *SchemaError) CheckSchema() error {
	return nil
}

type SchemaNull struct {
	Type        string  `json:"type,const=null"`
	Description *string `json:"description,omitempty"`
}

func (s *SchemaNull) CheckSchema() error {
	return nil
}

type SchemaBoolean struct {
	Type        string  `json:"type,const=bool"`
	Description *string `json:"description,omitempty"`
	Default     *bool   `json:"default,omitempty"`
	Const       *bool   `json:"const,omitempty"`
}

func (s *SchemaBoolean) CheckSchema() error {
	if s.Default != nil && s.Const != nil {
		return fmt.Errorf("schema can't have both 'default' and 'const'")
	}
	return nil
}

type SchemaInteger struct {
	Type        string  `json:"type,const=integer"`
	Description *string `json:"description,omitempty"`
	Minimum     *int    `json:"minimum,omitempty"`
	Maximum     *int    `json:"maximum,omitempty"`
	Enum        []int   `json:"enum,omitempty"`
	Default     *int    `json:"default,omitempty"`
	Const       *int    `json:"const,omitempty"`
}

func (s *SchemaInteger) CheckSchema() error {
	// TODO: enforce min/max against enum, default, const
	if s.Default != nil && s.Const != nil {
		return fmt.Errorf("schema can't have both 'default' and 'const'")
	}
	if s.Minimum != nil && s.Maximum != nil && *s.Maximum < *s.Minimum {
		return fmt.Errorf("schema max < min")
	}
	return nil
}

type SchemaString struct {
	Type         string   `json:"type,const=string"`
	Description  *string  `json:"description,omitempty"`
	Format       *string  `json:"format,omitempty"`
	MinLength    *int     `json:"minLength,omitempty"`
	MaxLength    *int     `json:"maxLength,omitempty"`
	MinGraphemes *int     `json:"minGraphemes,omitempty"`
	MaxGraphemes *int     `json:"maxGraphemes,omitempty"`
	KnownValues  []string `json:"knownValues,omitempty"`
	Enum         []string `json:"enum,omitempty"`
	Default      *string  `json:"default,omitempty"`
	Const        *string  `json:"const,omitempty"`
}

func (s *SchemaString) CheckSchema() error {
	// TODO: enforce min/max against enum, default, const
	if s.Default != nil && s.Const != nil {
		return fmt.Errorf("schema can't have both 'default' and 'const'")
	}
	if s.MinLength != nil && s.MaxLength != nil && *s.MaxLength < *s.MinLength {
		return fmt.Errorf("schema max < min")
	}
	if s.MinGraphemes != nil && s.MaxGraphemes != nil && *s.MaxGraphemes < *s.MinGraphemes {
		return fmt.Errorf("schema max < min")
	}
	if (s.MinLength != nil && *s.MinLength < 0) ||
		(s.MaxLength != nil && *s.MaxLength < 0) ||
		(s.MinGraphemes != nil && *s.MinGraphemes < 0) ||
		(s.MaxGraphemes != nil && *s.MaxGraphemes < 0) {
		return fmt.Errorf("string schema min or max below zero")
	}
	return nil
}

type SchemaBytes struct {
	Type        string  `json:"type,const=bytes"`
	Description *string `json:"description,omitempty"`
	MinLength   *int    `json:"minLength,omitempty"`
	MaxLength   *int    `json:"maxLength,omitempty"`
}

func (s *SchemaBytes) CheckSchema() error {
	if s.MinLength != nil && s.MaxLength != nil && *s.MaxLength < *s.MinLength {
		return fmt.Errorf("schema max < min")
	}
	if (s.MinLength != nil && *s.MinLength < 0) ||
		(s.MaxLength != nil && *s.MaxLength < 0) {
		return fmt.Errorf("bytes schema min or max below zero")
	}
	return nil
}

type SchemaCIDLink struct {
	Type        string  `json:"type,const=cid-link"`
	Description *string `json:"description,omitempty"`
}

func (s *SchemaCIDLink) CheckSchema() error {
	return nil
}

type SchemaArray struct {
	Type        string    `json:"type,const=array"`
	Description *string   `json:"description,omitempty"`
	Items       SchemaDef `json:"items"`
	MinLength   *int      `json:"minLength,omitempty"`
	MaxLength   *int      `json:"maxLength,omitempty"`
}

func (s *SchemaArray) CheckSchema() error {
	if s.MinLength != nil && s.MaxLength != nil && *s.MaxLength < *s.MinLength {
		return fmt.Errorf("schema max < min")
	}
	if (s.MinLength != nil && *s.MinLength < 0) ||
		(s.MaxLength != nil && *s.MaxLength < 0) {
		return fmt.Errorf("array schema min or max below zero")
	}
	return s.Items.CheckSchema()
}

type SchemaObject struct {
	Type        string               `json:"type,const=object"`
	Description *string              `json:"description,omitempty"`
	Properties  map[string]SchemaDef `json:"properties"`
	Required    []string             `json:"required,omitempty"`
	Nullable    []string             `json:"nullable,omitempty"`
}

func (s *SchemaObject) CheckSchema() error {
	// TODO: check for set intersection between required and nullable
	// TODO: check for set uniqueness of required and nullable
	for _, k := range s.Required {
		if _, ok := s.Properties[k]; !ok {
			fmt.Errorf("object 'required' field not in properties: %s", k)
		}
	}
	for _, k := range s.Nullable {
		if _, ok := s.Properties[k]; !ok {
			fmt.Errorf("object 'nullable' field not in properties: %s", k)
		}
	}
	for k, def := range s.Properties {
		// TODO: more checks on field name?
		if len(k) == 0 {
			return fmt.Errorf("empty object schema field name not allowed")
		}
		if err := def.CheckSchema(); err != nil {
			return err
		}
	}
	return nil
}

type SchemaBlob struct {
	Type        string   `json:"type,const=blob"`
	Description *string  `json:"description,omitempty"`
	Accept      []string `json:"accept,omitempty"`
	MaxSize     *int     `json:"maxSize,omitempty"`
}

func (s *SchemaBlob) CheckSchema() error {
	// TODO: validate Accept (mimetypes)?
	if s.MaxSize != nil && *s.MaxSize <= 0 {
		return fmt.Errorf("blob max size less or equal to zero")
	}
	return nil
}

type SchemaParams struct {
	Type        string               `json:"type,const=params"`
	Description *string              `json:"description,omitempty"`
	Properties  map[string]SchemaDef `json:"properties"` // boolean, integer, string, or unknown; or an array of these types
	Required    []string             `json:"required,omitempty"`
}

func (s *SchemaParams) CheckSchema() error {
	// TODO: check for set uniqueness of required
	for _, k := range s.Required {
		if _, ok := s.Properties[k]; !ok {
			fmt.Errorf("object 'required' field not in properties: %s", k)
		}
	}
	for k, def := range s.Properties {
		// TODO: more checks on field name?
		if len(k) == 0 {
			return fmt.Errorf("empty object schema field name not allowed")
		}
		switch v := def.Inner.(type) {
		case SchemaBoolean, SchemaInteger, SchemaString, SchemaUnknown:
			// pass
		case SchemaArray:
			switch v.Items.Inner.(type) {
			case SchemaBoolean, SchemaInteger, SchemaString, SchemaUnknown:
				// pass
			default:
				return fmt.Errorf("params array item type must be boolean, integer, string, or unknown")
			}
		default:
			return fmt.Errorf("params field type must be boolean, integer, string, or unknown")
		}
		if err := def.CheckSchema(); err != nil {
			return err
		}
	}
	return nil
}

type SchemaToken struct {
	Type        string  `json:"type,const=token"`
	Description *string `json:"description,omitempty"`
}

func (s *SchemaToken) CheckSchema() error {
	return nil
}

type SchemaRef struct {
	Type        string  `json:"type,const=ref"`
	Description *string `json:"description,omitempty"`
	Ref         string  `json:"ref"`
}

func (s *SchemaRef) CheckSchema() error {
	// TODO: more validation of ref string?
	if len(s.Ref) == 0 {
		return fmt.Errorf("empty schema ref")
	}
	return nil
}

type SchemaUnion struct {
	Type        string   `json:"type,const=union"`
	Description *string  `json:"description,omitempty"`
	Refs        []string `json:"refs"`
	Closed      *bool    `json:"closed,omitempty"`
}

func (s *SchemaUnion) CheckSchema() error {
	// TODO: uniqueness check on refs
	for _, ref := range s.Refs {
		// TODO: more validation of ref string?
		if len(ref) == 0 {
			return fmt.Errorf("empty schema ref")
		}
	}
	return nil
}

type SchemaUnknown struct {
	Type        string  `json:"type,const=unknown"`
	Description *string `json:"description,omitempty"`
}

func (s *SchemaUnknown) CheckSchema() error {
	return nil
}
