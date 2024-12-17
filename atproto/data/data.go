package data

import (
	"encoding/json"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"

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
	if len(b) > MAX_JSON_RECORD_SIZE {
		return nil, fmt.Errorf("exceeded max JSON record size: %d", len(b))
	}
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
	if len(b) > MAX_CBOR_RECORD_SIZE {
		return nil, fmt.Errorf("exceeded max CBOR record size: %d", len(b))
	}
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

// Recursively finds all the "blob" objects from generic atproto data (which has already been parsed).
//
// Returns an array with all Blob instances; does not de-dupe.
func ExtractBlobs(obj map[string]any) []Blob {
	return extractBlobsAtom(obj)
}

func extractBlobsAtom(atom any) []Blob {
	out := []Blob{}
	switch v := atom.(type) {
	case Blob:
		out = append(out, v)
	case []any:
		for _, el := range v {
			out = append(out, extractBlobsAtom(el)...)
		}
	case map[string]any:
		for _, val := range v {
			out = append(out, extractBlobsAtom(val)...)
		}
	default:
	}
	return out
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
		case syntax.AtIdentifier:
			out[k] = v.String()
		case *syntax.AtIdentifier:
			out[k] = v.String()
		case map[string]any:
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
		case syntax.AtIdentifier:
			out[i] = v.String()
		case *syntax.AtIdentifier:
			out[i] = v.String()
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
