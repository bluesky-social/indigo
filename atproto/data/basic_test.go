package data

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSimpleValidation(t *testing.T) {
	assert := assert.New(t)

	s := "a string"
	assert.NoError(Validate(map[string]interface{} {
		"a": 5,
		"b": 123,
		"c": s,
		"d": &s,
	}))
	assert.NoError(Validate(map[string]interface{} {
		"$type": "com.example.thing",
		"a": 5,
	}))
	assert.Error(Validate(map[string]interface{} {
		"$type": 123,
		"a": 5,
	}))
	assert.Error(Validate(map[string]interface{} {
		"$type": "",
		"a": 5,
	}))
}
