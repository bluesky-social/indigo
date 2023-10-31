package data

import (
	"encoding/json"

	"github.com/ipfs/go-cid"
	cbor "github.com/ipfs/go-ipld-cbor"
)

// Checks that generic data (object) complies with the atproto data model.
func Validate(obj map[string]any) error {
	_, err := parseObject(obj)
	return err
}

// Parses generic data (object) in JSON, validating against the atproto data model at the same time.
//
// The standard library's MarshalJSON can be used to invert this function.
func UnmarshalJSON(b []byte) (map[string]any, error) {
	var rawObj map[string]any
	err := json.Unmarshal(b, &rawObj)
	if err != nil {
		return nil, err
	}
	out, err := parseObject(rawObj)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Parses generic data (object) in CBOR (specifically, IPLD dag-cbor), validating against the atproto data model at the same time.
func UnmarshalCBOR(b []byte) (map[string]any, error) {
	var rawObj map[string]any
	err := cbor.DecodeInto(b, &rawObj)
	if err != nil {
		return nil, err
	}
	out, err := parseObject(rawObj)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Serializes generic atproto data (object) to DAG-CBOR bytes
//
// Does not re-validate that data conforms to atproto data model, but does handle Blob, Bytes, and CIDLink as expected.
func MarshalCBOR(obj map[string]any) ([]byte, error) {
	return cbor.DumpObject(forCBOR(obj))
}

// helper to get generic data in the correct "shape" for serialization with ipfs/go-ipld-cbor
func forCBOR(obj map[string]any) map[string]any {
	// NOTE: a faster version might mutate the map in-place instead of copying (many allocations)?
	out := make(map[string]any, len(obj))
	for k, val := range obj {
		switch v := val.(type) {
		case CIDLink:
			out[k] = cid.Cid(v)
		case Bytes:
			out[k] = []byte(v)
		case Blob:
			out[k] = map[string]interface{}{
				"$type":    "blob",
				"mimeType": v.MimeType,
				"ref":      cid.Cid(v.Ref),
				"size":     v.Size,
			}
		case map[string]any:
			out[k] = forCBOR(v)
			out[k] = forCBOR(v)
		case []any:
			out[k] = forCBORArray(v)
		default:
			out[k] = v
		}
	}
	return out
}

// recursive helper for forCBOR
func forCBORArray(arr []any) []any {
	// NOTE: a faster version might mutate the array in-place instead of copying (many allocations)?
	out := make([]any, len(arr))
	for i, val := range arr {
		switch v := val.(type) {
		case CIDLink:
			out[i] = cid.Cid(v)
		case Bytes:
			out[i] = []byte(v)
		case Blob:
			out[i] = map[string]interface{}{
				"$type":    "blob",
				"mimeType": v.MimeType,
				"ref":      cid.Cid(v.Ref),
				"size":     v.Size,
			}
		case map[string]any:
			out[i] = forCBOR(v)
		case []any:
			out[i] = forCBORArray(v)
		default:
			out[i] = v
		}
	}
	return out
}
