package lexicon

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/rivo/uniseg"
)

// Serialization helper type for top-level Lexicon schema JSON objects (files)
type SchemaFile struct {
	Lexicon     int                  `json:"lexicon,const=1"`
	ID          string               `json:"id"`
	Description *string              `json:"description,omitempty"`
	Defs        map[string]SchemaDef `json:"defs"`
}

// enum type to represent any of the schema fields
type SchemaDef struct {
	Inner any
}

// Checks that the schema definition itself is valid (recursively).
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
		return fmt.Errorf("unhandled schema type: %v", reflect.TypeOf(v))
	}
}

// Helper to recurse down the definition tree and set full references on any sub-schemas which need to embed that metadata
func (s *SchemaDef) SetBase(base string) {
	switch v := s.Inner.(type) {
	case SchemaRecord:
		for i, val := range v.Record.Properties {
			val.SetBase(base)
			v.Record.Properties[i] = val
		}
		s.Inner = v
	case SchemaQuery:
		for i, val := range v.Parameters.Properties {
			val.SetBase(base)
			v.Parameters.Properties[i] = val
		}
		if v.Output != nil && v.Output.Schema != nil {
			v.Output.Schema.SetBase(base)
		}
		s.Inner = v
	case SchemaProcedure:
		for i, val := range v.Parameters.Properties {
			val.SetBase(base)
			v.Parameters.Properties[i] = val
		}
		if v.Input != nil && v.Input.Schema != nil {
			v.Input.Schema.SetBase(base)
		}
		if v.Output != nil && v.Output.Schema != nil {
			v.Output.Schema.SetBase(base)
		}
		s.Inner = v
	case SchemaSubscription:
		for i, val := range v.Parameters.Properties {
			val.SetBase(base)
			v.Parameters.Properties[i] = val
		}
		if v.Message != nil {
			v.Message.Schema.SetBase(base)
		}
		s.Inner = v
	case SchemaArray:
		v.Items.SetBase(base)
		s.Inner = v
	case SchemaObject:
		for i, val := range v.Properties {
			val.SetBase(base)
			v.Properties[i] = val
		}
		s.Inner = v
	case SchemaParams:
		for i, val := range v.Properties {
			val.SetBase(base)
			v.Properties[i] = val
		}
		s.Inner = v
	case SchemaRef:
		// add fully-qualified name
		if strings.HasPrefix(v.Ref, "#") {
			v.fullRef = base + v.Ref
		} else {
			v.fullRef = v.Ref
		}
		s.Inner = v
	case SchemaUnion:
		// add fully-qualified name
		for _, ref := range v.Refs {
			if strings.HasPrefix(ref, "#") {
				ref = base + ref
			}
			v.fullRefs = append(v.fullRefs, ref)
		}
		s.Inner = v
	}
	return
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
}

type SchemaRecord struct {
	Type        string       `json:"type,const=record"`
	Description *string      `json:"description,omitempty"`
	Key         string       `json:"key"`
	Record      SchemaObject `json:"record"`
}

func (s *SchemaRecord) CheckSchema() error {
	switch s.Key {
	case "tid", "nsid", "any":
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
func (s *SchemaError) Validate(d any) error {
	e, ok := d.(map[string]any)
	if !ok {
		return fmt.Errorf("expected an object in error position")
	}
	n, ok := e["error"]
	if !ok {
		return fmt.Errorf("expected error type")
	}
	if n != s.Name {
		return fmt.Errorf("error type mis-match: %s", n)
	}
	return nil
}

type SchemaNull struct {
	Type        string  `json:"type,const=null"`
	Description *string `json:"description,omitempty"`
}

func (s *SchemaNull) CheckSchema() error {
	return nil
}

func (s *SchemaNull) Validate(d any) error {
	if d != nil {
		return fmt.Errorf("expected null data, got: %s", reflect.TypeOf(d))
	}
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

func (s *SchemaBoolean) Validate(d any) error {
	v, ok := d.(bool)
	if !ok {
		return fmt.Errorf("expected a boolean")
	}
	if s.Const != nil && v != *s.Const {
		return fmt.Errorf("boolean val didn't match constant (%v): %v", *s.Const, v)
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

func (s *SchemaInteger) Validate(d any) error {
	v64, ok := d.(int64)
	if !ok {
		return fmt.Errorf("expected an integer")
	}
	v := int(v64)
	if s.Const != nil && v != *s.Const {
		return fmt.Errorf("integer val didn't match constant (%d): %d", *s.Const, v)
	}
	if (s.Minimum != nil && v < *s.Minimum) || (s.Maximum != nil && v > *s.Maximum) {
		return fmt.Errorf("integer val outside specified range: %d", v)
	}
	if len(s.Enum) != 0 {
		inEnum := false
		for _, e := range s.Enum {
			if e == v {
				inEnum = true
				break
			}
		}
		if !inEnum {
			return fmt.Errorf("integer val not in required enum: %d", v)
		}
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
	if s.Format != nil {
		switch *s.Format {
		case "at-identifier", "at-uri", "cid", "datetime", "did", "handle", "nsid", "uri", "language", "tid", "record-key":
			// pass
		default:
			return fmt.Errorf("unknown string format: %s", *s.Format)
		}
	}
	return nil
}

func (s *SchemaString) Validate(d any, flags ValidateFlags) error {
	v, ok := d.(string)
	if !ok {
		return fmt.Errorf("expected a string: %v", reflect.TypeOf(d))
	}
	if s.Const != nil && v != *s.Const {
		return fmt.Errorf("string val didn't match constant (%s): %s", *s.Const, v)
	}
	// TODO: is this actually counting UTF-8 length?
	if (s.MinLength != nil && len(v) < *s.MinLength) || (s.MaxLength != nil && len(v) > *s.MaxLength) {
		return fmt.Errorf("string length outside specified range: %d", len(v))
	}
	if len(s.Enum) != 0 {
		inEnum := false
		for _, e := range s.Enum {
			if e == v {
				inEnum = true
				break
			}
		}
		if !inEnum {
			return fmt.Errorf("string val not in required enum: %s", v)
		}
	}
	if s.MinGraphemes != nil || s.MaxGraphemes != nil {
		lenG := uniseg.GraphemeClusterCount(v)
		if (s.MinGraphemes != nil && lenG < *s.MinGraphemes) || (s.MaxGraphemes != nil && lenG > *s.MaxGraphemes) {
			return fmt.Errorf("string length (graphemes) outside specified range: %d", lenG)
		}
	}
	if s.Format != nil {
		switch *s.Format {
		case "at-identifier":
			if _, err := syntax.ParseAtIdentifier(v); err != nil {
				return err
			}
		case "at-uri":
			if _, err := syntax.ParseATURI(v); err != nil {
				return err
			}
		case "cid":
			if _, err := syntax.ParseCID(v); err != nil {
				return err
			}
		case "datetime":
			if flags&AllowLenientDatetime != 0 {
				if _, err := syntax.ParseDatetimeLenient(v); err != nil {
					return err
				}
			} else {
				if _, err := syntax.ParseDatetime(v); err != nil {
					return err
				}
			}
		case "did":
			if _, err := syntax.ParseDID(v); err != nil {
				return err
			}
		case "handle":
			if _, err := syntax.ParseHandle(v); err != nil {
				return err
			}
		case "nsid":
			if _, err := syntax.ParseNSID(v); err != nil {
				return err
			}
		case "uri":
			if _, err := syntax.ParseURI(v); err != nil {
				return err
			}
		case "language":
			if _, err := syntax.ParseLanguage(v); err != nil {
				return err
			}
		case "tid":
			if _, err := syntax.ParseTID(v); err != nil {
				return err
			}
		case "record-key":
			if _, err := syntax.ParseRecordKey(v); err != nil {
				return err
			}
		}
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

func (s *SchemaBytes) Validate(d any) error {
	v, ok := d.(data.Bytes)
	if !ok {
		return fmt.Errorf("expecting bytes")
	}
	if (s.MinLength != nil && len(v) < *s.MinLength) || (s.MaxLength != nil && len(v) > *s.MaxLength) {
		return fmt.Errorf("bytes size out of bounds: %d", len(v))
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

func (s *SchemaCIDLink) Validate(d any) error {
	_, ok := d.(data.CIDLink)
	if !ok {
		return fmt.Errorf("expecting a cid-link")
	}
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
			return fmt.Errorf("object 'required' field not in properties: %s", k)
		}
	}
	for _, k := range s.Nullable {
		if _, ok := s.Properties[k]; !ok {
			return fmt.Errorf("object 'nullable' field not in properties: %s", k)
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

// Checks if a field name 'k' is one of the Nullable fields for this object
func (s *SchemaObject) IsNullable(k string) bool {
	for _, el := range s.Nullable {
		if el == k {
			return true
		}
	}
	return false
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

func (s *SchemaBlob) Validate(d any, flags ValidateFlags) error {
	v, ok := d.(data.Blob)
	if !ok {
		return fmt.Errorf("expected a blob")
	}
	if !(flags&AllowLegacyBlob != 0) && v.Size < 0 {
		return fmt.Errorf("legacy blobs not allowed")
	}
	if len(s.Accept) > 0 {
		typeOk := false
		for _, pat := range s.Accept {
			if acceptableMimeType(pat, v.MimeType) {
				typeOk = true
				break
			}
		}
		if !typeOk {
			return fmt.Errorf("blob mimetype doesn't match accepted: %s", v.MimeType)
		}
	}
	if s.MaxSize != nil && int(v.Size) > *s.MaxSize {
		return fmt.Errorf("blob size too large: %d", v.Size)
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
			return fmt.Errorf("object 'required' field not in properties: %s", k)
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
	// the fully-qualified identifier of this token
	fullName string
}

func (s *SchemaToken) CheckSchema() error {
	if s.fullName == "" {
		return fmt.Errorf("expected fully-qualified token name")
	}
	return nil
}

func (s *SchemaToken) Validate(d any) error {
	str, ok := d.(string)
	if !ok {
		return fmt.Errorf("expected a string for token, got: %s", reflect.TypeOf(d))
	}
	if s.fullName == "" {
		return fmt.Errorf("token name was not populated at parse time")
	}
	if str != s.fullName {
		return fmt.Errorf("token name did not match expected: %s", str)
	}
	return nil
}

type SchemaRef struct {
	Type        string  `json:"type,const=ref"`
	Description *string `json:"description,omitempty"`
	Ref         string  `json:"ref"`
	// full path of reference
	fullRef string
}

func (s *SchemaRef) CheckSchema() error {
	// TODO: more validation of ref string?
	if len(s.Ref) == 0 {
		return fmt.Errorf("empty schema ref")
	}
	if len(s.fullRef) == 0 {
		return fmt.Errorf("empty full schema ref")
	}
	return nil
}

type SchemaUnion struct {
	Type        string   `json:"type,const=union"`
	Description *string  `json:"description,omitempty"`
	Refs        []string `json:"refs"`
	Closed      *bool    `json:"closed,omitempty"`
	// fully qualified
	fullRefs []string
}

func (s *SchemaUnion) CheckSchema() error {
	// TODO: uniqueness check on refs
	for _, ref := range s.Refs {
		// TODO: more validation of ref string?
		if len(ref) == 0 {
			return fmt.Errorf("empty schema ref")
		}
	}
	if len(s.fullRefs) != len(s.Refs) {
		return fmt.Errorf("union refs were not expanded")
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

func (s *SchemaUnknown) Validate(d any) error {
	_, ok := d.(map[string]any)
	if !ok {
		return fmt.Errorf("'unknown' data must an object")
	}
	return nil
}
