package lexicon

import (
	"encoding/json"
	"fmt"
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

func (s SchemaDef) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Inner)
}

func (s *SchemaDef) UnmarshalJSON(b []byte) error {
	t, err := ExtractTypeJSON(b)
	if err != nil {
		return err
	}
	switch t {
	case "record":
		v := new(SchemaRecord)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "query":
		v := new(SchemaQuery)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "procedure":
		v := new(SchemaProcedure)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "subscription":
		v := new(SchemaSubscription)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "null":
		v := new(SchemaNull)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "boolean":
		v := new(SchemaBoolean)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "integer":
		v := new(SchemaInteger)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "string":
		v := new(SchemaString)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "bytes":
		v := new(SchemaBytes)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "cid-link":
		v := new(SchemaCIDLink)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "array":
		v := new(SchemaArray)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "object":
		v := new(SchemaObject)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "blob":
		v := new(SchemaBlob)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "params":
		v := new(SchemaParams)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "token":
		v := new(SchemaToken)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "ref":
		v := new(SchemaRef)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "union":
		v := new(SchemaUnion)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
		return nil
	case "unknown":
		v := new(SchemaUnknown)
		if err = json.Unmarshal(b, v); err != nil {
			return err
		}
		s.Inner = v
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

type SchemaQuery struct {
	Type        string        `json:"type,const=query"`
	Description *string       `json:"description,omitempty"`
	Parameters  SchemaParams  `json:"parameters"`
	Output      *SchemaBody   `json:"output"`
	Errors      []SchemaError `json:"errors,omitempty"` // optional
}

type SchemaProcedure struct {
	Type        string        `json:"type,const=procedure"`
	Description *string       `json:"description,omitempty"`
	Parameters  SchemaParams  `json:"parameters"`
	Output      *SchemaBody   `json:"output"`           // optional
	Errors      []SchemaError `json:"errors,omitempty"` // optional
	Input       *SchemaBody   `json:"input"`            // optional
}

type SchemaSubscription struct {
	Type        string         `json:"type,const=subscription"`
	Description *string        `json:"description,omitempty"`
	Parameters  SchemaParams   `json:"parameters"`
	Message     *SchemaMessage `json:"message,omitempty"` // TODO(specs): is this really optional?
}

type SchemaBody struct {
	Description *string    `json:"description,omitempty"`
	Encoding    string     `json:"encoding"` // required, mimetype
	Schema      *SchemaDef `json:"schema"`   // optional; type:object, type:ref, or type:union
}

type SchemaMessage struct {
	Description *string   `json:"description,omitempty"`
	Schema      SchemaDef `json:"schema"` // required; type:union only
}

type SchemaError struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type SchemaNull struct {
	Type        string  `json:"type,const=null"`
	Description *string `json:"description,omitempty"`
}

type SchemaBoolean struct {
	Type        string  `json:"type,const=bool"`
	Description *string `json:"description,omitempty"`
	Default     *bool   `json:"default,omitempty"`
	Const       *bool   `json:"const,omitempty"`
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
	Default      *int     `json:"default,omitempty"`
	Const        *int     `json:"const,omitempty"`
}

type SchemaBytes struct {
	Type        string  `json:"type,const=bytes"`
	Description *string `json:"description,omitempty"`
	MinLength   *int    `json:"minLength,omitempty"`
	MaxLength   *int    `json:"maxLength,omitempty"`
}

type SchemaCIDLink struct {
	Type        string  `json:"type,const=cid-link"`
	Description *string `json:"description,omitempty"`
}

type SchemaArray struct {
	Type        string    `json:"type,const=array"`
	Description *string   `json:"description,omitempty"`
	Items       SchemaDef `json:"items"`
	MinLength   *int      `json:"minLength,omitempty"`
	MaxLength   *int      `json:"maxLength,omitempty"`
}

type SchemaObject struct {
	Type        string               `json:"type,const=object"`
	Description *string              `json:"description,omitempty"`
	Properties  map[string]SchemaDef `json:"properties"`
	Required    []string             `json:"required,omitempty"`
	Nullable    []string             `json:"nullable,omitempty"`
}

type SchemaBlob struct {
	Type        string   `json:"type,const=blob"`
	Description *string  `json:"description,omitempty"`
	Accept      []string `json:"accept,omitempty"`
	MaxSize     *int     `json:"maxSize,omitempty"`
}

type SchemaParams struct {
	Type        string               `json:"type,const=params"`
	Description *string              `json:"description,omitempty"`
	Properties  map[string]SchemaDef `json:"properties"` // boolean, integer, string, or unknown; or an array of these types
	Required    []string             `json:"required,omitempty"`
}

type SchemaToken struct {
	Type        string  `json:"type,const=token"`
	Description *string `json:"description,omitempty"`
}

type SchemaRef struct {
	Type        string  `json:"type,const=ref"`
	Description *string `json:"description,omitempty"`
	Ref         string  `json:"ref"`
}

type SchemaUnion struct {
	Type        string   `json:"type,const=union"`
	Description *string  `json:"description,omitempty"`
	Refs        []string `json:"refs"`
	Closed      *bool    `json:"closed,omitempty"`
}

type SchemaUnknown struct {
	Type        string  `json:"type,const=unknown"`
	Description *string `json:"description,omitempty"`
}
