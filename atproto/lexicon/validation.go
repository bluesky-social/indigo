package lexicon

import (
	"fmt"
	"reflect"
)

// Boolean flags tweaking how Lexicon validation rules are interpreted.
type ValidateFlags int

const (
	// Flag which allows legacy "blob" data to pass validation.
	AllowLegacyBlob = 1 << iota
	// Flag which loosens "datetime" string syntax validation. String must still be an ISO datetime, but might be missing timezone (for example)
	AllowLenientDatetime
	// Flag which requires validation of nested data in open unions. By default nested union types are only validated optimistically (if the type is known in catatalog) for unlisted types. This flag will result in a validation error if the Lexicon can't be resolved from the catalog.
	StrictRecursiveValidation
)

// Combination of argument flags for less formal validation. Recommended for, eg, working with old/legacy data from 2023.
var LenientMode ValidateFlags = AllowLegacyBlob | AllowLenientDatetime

// Represents a Lexicon schema definition
type Schema struct {
	ID  string
	Def any
}

// Checks Lexicon schema (fetched from the catalog) for the given record, with optional flags tweaking default validation rules.
//
// 'recordData' is typed as 'any', but is expected to be 'map[string]any'
// 'ref' is a reference to the schema type, as an NSID with optional fragment. For records, the '$type' must match 'ref'
// 'flags' are parameters tweaking Lexicon validation rules. Zero value is default.
func ValidateRecord(cat Catalog, recordData any, ref string, flags ValidateFlags) error {
	return validateRecordConfig(cat, recordData, ref, flags)
}

func validateRecordConfig(cat Catalog, recordData any, ref string, flags ValidateFlags) error {
	def, err := cat.Resolve(ref)
	if err != nil {
		return err
	}
	s, ok := def.Def.(SchemaRecord)
	if !ok {
		return fmt.Errorf("schema is not of record type: %s", ref)
	}
	d, ok := recordData.(map[string]any)
	if !ok {
		return fmt.Errorf("record data is not object type")
	}
	t, ok := d["$type"]
	if !ok || t != ref {
		return fmt.Errorf("record data missing $type, or didn't match expected NSID")
	}
	return validateObject(cat, s.Record, d, flags)
}

func validateData(cat Catalog, def any, d any, flags ValidateFlags) error {
	switch v := def.(type) {
	case SchemaNull:
		return v.Validate(d)
	case SchemaBoolean:
		return v.Validate(d)
	case SchemaInteger:
		return v.Validate(d)
	case SchemaString:
		return v.Validate(d, flags)
	case SchemaBytes:
		return v.Validate(d)
	case SchemaCIDLink:
		return v.Validate(d)
	case SchemaArray:
		arr, ok := d.([]any)
		if !ok {
			return fmt.Errorf("expected an array, got: %s", reflect.TypeOf(d))
		}
		return validateArray(cat, v, arr, flags)
	case SchemaObject:
		obj, ok := d.(map[string]any)
		if !ok {
			return fmt.Errorf("expected an object, got: %s", reflect.TypeOf(d))
		}
		return validateObject(cat, v, obj, flags)
	case SchemaBlob:
		return v.Validate(d, flags)
	case SchemaRef:
		// recurse
		next, err := cat.Resolve(v.fullRef)
		if err != nil {
			return err
		}
		return validateData(cat, next.Def, d, flags)
	case SchemaUnion:
		return validateUnion(cat, v, d, flags)
	case SchemaUnknown:
		return v.Validate(d)
	case SchemaToken:
		return v.Validate(d)
	default:
		return fmt.Errorf("unhandled schema type: %s", reflect.TypeOf(v))
	}
}

func validateObject(cat Catalog, s SchemaObject, d map[string]any, flags ValidateFlags) error {
	for _, k := range s.Required {
		if _, ok := d[k]; !ok {
			return fmt.Errorf("required field missing: %s", k)
		}
	}
	for k, def := range s.Properties {
		if v, ok := d[k]; ok {
			if v == nil && s.IsNullable(k) {
				continue
			}
			err := validateData(cat, def.Inner, v, flags)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func validateArray(cat Catalog, s SchemaArray, arr []any, flags ValidateFlags) error {
	if (s.MinLength != nil && len(arr) < *s.MinLength) || (s.MaxLength != nil && len(arr) > *s.MaxLength) {
		return fmt.Errorf("array length out of bounds: %d", len(arr))
	}
	for _, v := range arr {
		err := validateData(cat, s.Items.Inner, v, flags)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateUnion(cat Catalog, s SchemaUnion, d any, flags ValidateFlags) error {
	closed := s.Closed != nil && *s.Closed == true

	obj, ok := d.(map[string]any)
	if !ok {
		return fmt.Errorf("union data is not object type")
	}
	typeVal, ok := obj["$type"]
	if !ok {
		return fmt.Errorf("union data must have $type")
	}
	t, ok := typeVal.(string)
	if !ok {
		return fmt.Errorf("union data must have string $type")
	}

	for _, ref := range s.fullRefs {
		if ref != t {
			continue
		}
		def, err := cat.Resolve(ref)
		if err != nil {
			return fmt.Errorf("could not resolve known union variant $type: %s", ref)
		}
		return validateData(cat, def.Def, d, flags)
	}
	if closed {
		return fmt.Errorf("data did not match any variant of closed union: %s", t)
	}

	// eagerly attempt validation of the open union type
	// TODO: validate reference as NSID with optional fragment
	def, err := cat.Resolve(t)
	if err != nil {
		if flags&StrictRecursiveValidation != 0 {
			return fmt.Errorf("could not strictly validate open union variant $type: %s", t)
		}
		// by default, ignore validation of unknown open union data
		return nil
	}
	return validateData(cat, def.Def, d, flags)
}
