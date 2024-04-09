package search

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJapaneseDetection(t *testing.T) {
	assert := assert.New(t)

	assert.False(containsJapanese(""))
	assert.False(containsJapanese("basic english"))
	assert.False(containsJapanese("basic english"))

	assert.True(containsJapanese("学校から帰って熱いお風呂に入ったら力一杯がんばる"))
	assert.True(containsJapanese("パリ"))
	assert.True(containsJapanese("ハリー・ポッター"))
	assert.True(containsJapanese("some japanese パリ and some english"))

	// CJK, but not japanese-specific
	assert.False(containsJapanese("熱力学"))
}
