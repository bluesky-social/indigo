package util

import (
	"reflect"
	"testing"

	cbg "github.com/whyrusleeping/cbor-gen"
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

func TestTypesHaveCBORGen(t *testing.T) {
	for name, rt := range lexTypesMap {
		v := reflect.New(rt).Interface()
		_, typeOk := v.(cbg.CBORMarshaler)
		if !typeOk {
			t.Errorf("%s %T is not CBORMarshaler", name, v)
		}
		_, typeOk = v.(cbg.CBORUnmarshaler)
		if !typeOk {
			t.Errorf("%s %T is not CBORUnmarshaler", name, v)
		}
	}
}
