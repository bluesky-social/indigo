package data

import (
	"encoding"
	"encoding/base64"
	"fmt"
	"reflect"

	"github.com/ipfs/go-cid"
)

func parseFloat(f float64) (int64, error) {
	if f != float64(int64(f)) {
		return 0, fmt.Errorf("number was is not a safe integer: %f", f)
	}
	return int64(f), nil
}

func parseAtom(atom any) (any, error) {
	switch v := atom.(type) {
	case nil:
		return v, nil
	case bool:
		return v, nil
	case *bool:
		return *v, nil
	case int64:
		return v, nil
	case *int64:
		return *v, nil
	case int:
		return int64(v), nil
	case *int:
		return int64(*v), nil
	case float64:
		return parseFloat(v)
	case *float64:
		return parseFloat(*v)
	case string:
		if len(v) > MAX_RECORD_STRING_LEN {
			return nil, fmt.Errorf("string too long: %d", len(v))
		}
		return v, nil
	case *string:
		return parseAtom(*v)
	case cid.Cid:
		return CIDLink(v), nil
	case *cid.Cid:
		return CIDLink(*v), nil
	case []byte:
		return Bytes(v), nil
	case *[]byte:
		return Bytes(*v), nil
	case []any:
		return parseArray(v)
	case *[]any:
		return parseArray(*v)
	case map[string]any:
		return parseMap(v)
	case *map[string]any:
		return parseMap(*v)
	case encoding.TextMarshaler:
		s, err := v.MarshalText()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal text (%s): %w", reflect.TypeOf(v), err)
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unexpected type: %s", reflect.TypeOf(v))
	}
}

func parseArray(l []any) ([]any, error) {
	if len(l) > MAX_CBOR_CONTAINER_LEN {
		return nil, fmt.Errorf("data array length too long: %d", len(l))
	}
	out := make([]any, len(l))
	for i, v := range l {
		p, err := parseAtom(v)
		if err != nil {
			return nil, err
		}
		out[i] = p
	}
	return out, nil
}

func parseMap(obj map[string]any) (any, error) {
	if len(obj) > MAX_CBOR_CONTAINER_LEN {
		return nil, fmt.Errorf("data object has too many fields: %d", len(obj))
	}
	if _, ok := obj["$link"]; ok {
		return parseLink(obj)
	}
	if _, ok := obj["$bytes"]; ok {
		return parseBytes(obj)
	}
	if typeVal, ok := obj["$type"]; ok {
		if typeStr, ok := typeVal.(string); ok {
			if typeStr == "blob" {
				b, err := parseBlob(obj)
				if err != nil {
					return nil, err
				}
				return *b, nil
			}
			if len(typeStr) == 0 {
				return nil, fmt.Errorf("$type field must contain a non-empty string")
			}
		} else {
			return nil, fmt.Errorf("$type field must contain a non-empty string")
		}
	}
	// legacy blob type
	if len(obj) == 2 {
		if _, ok := obj["mimeType"]; ok {
			if _, ok := obj["cid"]; ok {
				b, err := parseLegacyBlob(obj)
				if err != nil {
					return nil, err
				}
				return *b, nil
			}
		}
	}
	out := make(map[string]any, len(obj))
	for k, val := range obj {
		if len(k) > MAX_OBJECT_KEY_LEN {
			return nil, fmt.Errorf("data object key too long: %d", len(k))
		}
		atom, err := parseAtom(val)
		if err != nil {
			return nil, err
		}
		out[k] = atom
	}
	return out, nil
}

func parseLink(obj map[string]any) (CIDLink, error) {
	var zero CIDLink
	if len(obj) != 1 {
		return zero, fmt.Errorf("$link objects must have a single field")
	}
	v, ok := obj["$link"].(string)
	if !ok {
		return zero, fmt.Errorf("$link field missing or not a string")
	}
	c, err := cid.Parse(v)
	if err != nil {
		return zero, fmt.Errorf("invalid $link CID: %w", err)
	}
	if !c.Defined() {
		return zero, fmt.Errorf("undefined (null) CID in $link")
	}
	return CIDLink(c), nil
}

func parseBytes(obj map[string]any) (Bytes, error) {
	if len(obj) != 1 {
		return nil, fmt.Errorf("$bytes objects must have a single field")
	}
	v, ok := obj["$bytes"].(string)
	if !ok {
		return nil, fmt.Errorf("$bytes field missing or not a string")
	}
	b, err := base64.RawStdEncoding.DecodeString(v)
	if err != nil {
		return nil, fmt.Errorf("decoding $byte value: %w", err)
	}
	return Bytes(b), nil
}

// NOTE: doesn't handle legacy blobs yet!
func parseBlob(obj map[string]any) (*Blob, error) {
	if len(obj) != 4 {
		return nil, fmt.Errorf("blobs expected to have 4 fields")
	}
	if obj["$type"] != "blob" {
		return nil, fmt.Errorf("blobs expected to have $type=blob")
	}
	var size int64
	var err error
	switch v := obj["size"].(type) {
	case int:
		size = int64(v)
	case int64:
		size = v
	case float64:
		size, err = parseFloat(v)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("blob 'size' missing or not a number")
	}
	mimeType, ok := obj["mimeType"].(string)
	if !ok {
		return nil, fmt.Errorf("blob 'mimeType' missing or not a string")
	}
	rawRef, ok := obj["ref"]
	if !ok {
		return nil, fmt.Errorf("blob 'ref' missing")
	}
	var ref CIDLink
	switch v := rawRef.(type) {
	case map[string]any:
		cl, err := parseLink(v)
		if err != nil {
			return nil, err
		}
		ref = cl
	case cid.Cid:
		ref = CIDLink(v)
	case CIDLink:
		ref = v
	default:
		return nil, fmt.Errorf("blob 'ref' unexpected type")
	}

	return &Blob{
		Size:     size,
		MimeType: mimeType,
		Ref:      ref,
	}, nil
}

func parseLegacyBlob(obj map[string]any) (*Blob, error) {
	if len(obj) != 2 {
		return nil, fmt.Errorf("legacy blobs expected to have 2 fields")
	}
	var err error
	mimeType, ok := obj["mimeType"].(string)
	if !ok {
		return nil, fmt.Errorf("blob 'mimeType' missing or not a string")
	}
	cidStr, ok := obj["cid"]
	if !ok {
		return nil, fmt.Errorf("blob 'cid' missing")
	}
	c, err := cid.Parse(cidStr)
	if err != nil {
		return nil, fmt.Errorf("invalid CID: %w", err)
	}
	return &Blob{
		Size:     -1,
		MimeType: mimeType,
		Ref:      CIDLink(c),
	}, nil
}

func parseObject(obj map[string]any) (map[string]any, error) {
	out, err := parseMap(obj)
	if err != nil {
		return nil, err
	}
	if outObj, ok := out.(map[string]any); ok {
		return outObj, nil
	}
	return nil, fmt.Errorf("top-level datum was not an object")
}
