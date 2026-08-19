package util

import (
	"encoding/json"
	"testing"
)

func TestLTDMarshal(t *testing.T) {

	var empty *LexiconTypeDecoder

	_, err := empty.MarshalJSON()
	if err == nil {
		t.Fatal("expected an error marshalling a nil (but not a panic)")
	}

	emptyVal := LexiconTypeDecoder{}

	_, err = emptyVal.MarshalJSON()
	if err == nil {
		t.Fatal("expected an error marshalling a nil (but not a panic)")
	}
}

func TestNewFromType(t *testing.T) {

	raw, err := NewFromType("blob")
	if err != nil {
		t.Fatal(err)
	}
	blob := raw.(*LexBlob)
	if blob.Size != 0 {
		t.Fatal("expect default/nil LexBlob")
	}

	_, err = NewFromType("bogus.type")
	if err == nil {
		t.Fatal("expect bogus generation to fail")
	}
}

func TestMissingTypeField(t *testing.T) {
	var out struct {
		Field *LexiconTypeDecoder
	}
	const input = `{"Field":{"Some_stuff":"but $type is missing"}}`

	if err := json.Unmarshal([]byte(input), &out); err != nil {
		t.Fatalf("failed to unmarshal: %s", err)
	}
}
