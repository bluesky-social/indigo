package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"

	cbg "github.com/whyrusleeping/cbor-gen"
)

var lexTypesMap map[string]reflect.Type

func init() {
	lexTypesMap = make(map[string]reflect.Type)
}

func RegisterType(id string, val any) {
	t := reflect.TypeOf(val)

	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if _, ok := lexTypesMap[id]; ok {
		panic(fmt.Sprintf("already registered type for %q", id))
	}

	lexTypesMap[id] = t
}

func JsonDecodeValue(b []byte) (any, error) {
	tstr, err := TypeExtract(b)
	if err != nil {
		return nil, err
	}

	t, ok := lexTypesMap[tstr]
	if !ok {
		return nil, fmt.Errorf("unrecognized type: %q", tstr)
	}

	val := reflect.New(t)

	ival := val.Interface()
	if err := json.Unmarshal(b, ival); err != nil {
		return nil, err
	}

	return ival, nil
}

func CborDecodeValue(b []byte) (any, error) {
	tstr, err := CborTypeExtract(b)
	if err != nil {
		return nil, fmt.Errorf("cbor type extract: %w", err)
	}

	t, ok := lexTypesMap[tstr]
	if !ok {
		return nil, fmt.Errorf("unrecognized type: %q", tstr)
	}

	val := reflect.New(t)

	ival, ok := val.Interface().(cbg.CBORUnmarshaler)
	if !ok {
		return nil, fmt.Errorf("registered type did not have proper cbor hooks")
	}

	if err := ival.UnmarshalCBOR(bytes.NewReader(b)); err != nil {
		return nil, err
	}

	return ival, nil
}
