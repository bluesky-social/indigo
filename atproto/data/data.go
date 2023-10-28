package data

import (
	"encoding/json"

	cbor "github.com/ipfs/go-ipld-cbor"
)

func Validate(obj map[string]any) error {
	_, err := parseObject(obj)
	return err
}

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

func MarshalCBOR(obj map[string]any) ([]byte, error) {
	return cbor.DumpObject(obj)
}
