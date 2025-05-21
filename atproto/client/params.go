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
		case bool:
			if v {
				out.Set(k, "true")
			} else {
				out.Set(k, "false")
			}
		case string:
			out.Set(k, v)
		case int, uint, int8, int16, int32, int64, uint8, uint16, uint32, uint64:
			out.Set(k, fmt.Sprintf("%d", v))
		case encoding.TextMarshaler:
			out.Set(k, fmt.Sprintf("%s", v))
		default:
			ref := reflect.ValueOf(v)
			if ref.Kind() == reflect.Slice {
				for i := 0; i < ref.Len(); i++ {
					switch elem := ref.Index(i).Interface().(type) {
					case nil:
						out.Add(k, "")
					case bool:
						if elem {
							out.Add(k, "true")
						} else {
							out.Add(k, "false")
						}
					case string:
						out.Add(k, elem)
					case int, uint, int8, int16, int32, int64, uint8, uint16, uint32, uint64:
						out.Add(k, fmt.Sprintf("%d", elem))
					case encoding.TextMarshaler:
						out.Add(k, fmt.Sprintf("%s", elem))
					default:
						return nil, fmt.Errorf("can't marshal query param type: %T", v)
					}
				}
			} else {
				return nil, fmt.Errorf("can't marshal query param type: %T", v)
			}
		}
	}
	return out, nil
}
