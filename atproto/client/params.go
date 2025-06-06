package client

import (
	"encoding"
	"fmt"
	"net/url"
	"reflect"
)

// Flexibly parses an input map to URL query params (strings)
func ParseParams(raw map[string]any) (url.Values, error) {
	out := make(url.Values)
	for k := range raw {
		switch v := raw[k].(type) {
		case nil:
			out.Set(k, "")
		case bool, string, int, uint, int8, int16, int32, int64, uint8, uint16, uint32, uint64, uintptr:
			out.Set(k, fmt.Sprint(v))
		case encoding.TextMarshaler:
			out.Set(k, fmt.Sprint(v))
		default:
			ref := reflect.ValueOf(v)
			if ref.Kind() == reflect.Slice {
				for i := 0; i < ref.Len(); i++ {
					switch elem := ref.Index(i).Interface().(type) {
					case nil:
						out.Add(k, "")
					case bool, string, int, uint, int8, int16, int32, int64, uint8, uint16, uint32, uint64, uintptr:
						out.Add(k, fmt.Sprint(elem))
					case encoding.TextMarshaler:
						out.Add(k, fmt.Sprint(elem))
					default:
						return nil, fmt.Errorf("can't marshal query param '%s' with type: %T", k, v)
					}
				}
			} else {
				return nil, fmt.Errorf("can't marshal query param '%s' with type: %T", k, v)
			}
		}
	}
	return out, nil
}
