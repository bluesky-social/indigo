package util

import (
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
