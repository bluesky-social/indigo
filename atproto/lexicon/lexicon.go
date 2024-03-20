package lexicon

import (
	"fmt"
	"reflect"
)

type Schema struct {
	ID       string
	Revision *int
	Def      any
}

func ValidateRecord(cat Catalog, recordData any, ref string) error {
	return validateRecordConfig(cat, recordData, ref, false)
}

// Variation of ValidateRecord which allows "legacy" blob format, and flexible string datetimes.
//
// Hope is to deprecate this lenient variation in the near future!
func ValidateRecordLenient(cat Catalog, recordData any, ref string) error {
	return validateRecordConfig(cat, recordData, ref, true)
}

func validateRecordConfig(cat Catalog, recordData any, ref string, lenient bool) error {
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
	return validateObject(cat, s.Record, d, lenient)
}

func validateData(cat Catalog, def any, d any, lenient bool) error {
	// TODO:
	switch v := def.(type) {
	case SchemaNull:
		return v.Validate(d)
	case SchemaBoolean:
		return v.Validate(d)
	case SchemaInteger:
		return v.Validate(d)
	case SchemaString:
		return v.Validate(d, lenient)
	case SchemaBytes:
		return v.Validate(d)
	case SchemaCIDLink:
		return v.Validate(d)
	case SchemaArray:
		arr, ok := d.([]any)
		if !ok {
			return fmt.Errorf("expected an array, got: %s", reflect.TypeOf(d))
		}
		return validateArray(cat, v, arr, lenient)
	case SchemaObject:
		obj, ok := d.(map[string]any)
		if !ok {
			return fmt.Errorf("expected an object, got: %s", reflect.TypeOf(d))
		}
		return validateObject(cat, v, obj, lenient)
	case SchemaBlob:
		return v.Validate(d, lenient)
	case SchemaRef:
		// recurse
		next, err := cat.Resolve(v.fullRef)
		if err != nil {
			return err
		}
		return validateData(cat, next.Def, d, lenient)
	case SchemaUnion:
		return validateUnion(cat, v, d, lenient)
	case SchemaUnknown:
		return v.Validate(d)
	case SchemaToken:
		return v.Validate(d)
	default:
		return fmt.Errorf("unhandled schema type: %s", reflect.TypeOf(v))
	}
}

func validateObject(cat Catalog, s SchemaObject, d map[string]any, lenient bool) error {
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
			err := validateData(cat, def.Inner, v, lenient)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func validateArray(cat Catalog, s SchemaArray, arr []any, lenient bool) error {
	if (s.MinLength != nil && len(arr) < *s.MinLength) || (s.MaxLength != nil && len(arr) > *s.MaxLength) {
		return fmt.Errorf("array length out of bounds: %d", len(arr))
	}
	for _, v := range arr {
		err := validateData(cat, s.Items.Inner, v, lenient)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateUnion(cat Catalog, s SchemaUnion, d any, lenient bool) error {
	closed := s.Closed != nil && *s.Closed == true
	for _, ref := range s.fullRefs {
		def, err := cat.Resolve(ref)
		if err != nil {
			// TODO: how to actually handle unknown defs?
			return err
		}
		if err = validateData(cat, def.Def, d, lenient); nil == err { // if success
			return nil
		}
	}
	if closed {
		return fmt.Errorf("data did not match any variant of closed union")
	}
	// TODO: anything matches if an open union?
	return nil
}
