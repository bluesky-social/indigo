package lexlint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckSchemaName(t *testing.T) {
	assert := assert.New(t)

	goodNames := []string{
		"blahFunc",
		"blahFuncV2",
	}

	badNames := []string{
		"",
		" ",
		"blah Func",
		"blah_Func",
		"blah-Func",
		" blahFunc",
		"one.two",
		".",
		"2blahFunc",
	}

	for _, name := range goodNames {
		assert.NoError(CheckSchemaName(name))
	}

	for _, name := range badNames {
		assert.Error(CheckSchemaName(name))
	}
}
