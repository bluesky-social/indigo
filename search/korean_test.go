package search

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKoreanDetection(t *testing.T) {
	assert := assert.New(t)

	assert.False(containsKorean(""))
	assert.False(containsKorean("basic english"))
	assert.False(containsKorean("basic english"))

	assert.True(containsKorean("하교 후에 뜨거운 물로 샤워를 하고 열심히 공부한다"))
	assert.True(containsKorean("파리"))
	assert.True(containsKorean("해리 포터"))
	assert.True(containsKorean("some korean 파리 and some english"))

	// CJK, but not Korean-specific
	assert.False(containsKorean("熱力学"))
}
