package mst

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHeight(t *testing.T) {
	msg := "MST 'depth' computation (SHA-256 leading zeros)"

	testVec := []struct {
		Key    []byte
		Height int
	}{
		{[]byte(""), 0},
		{[]byte(""), 0},
		{[]byte("asdf"), 0},
		{[]byte("blue"), 1},
		{[]byte("2653ae71"), 0},
		{[]byte("88bfafc7"), 2},
		{[]byte("2a92d355"), 4},
		{[]byte("884976f5"), 6},
		{[]byte("app.bsky.feed.post/454397e440ec"), 4},
		{[]byte("app.bsky.feed.post/9adeb165882c"), 8},
	}

	for _, c := range testVec {
		assert.Equal(t, c.Height, HeightForKey(c.Key), msg)
	}

}

func TestPrefixLen(t *testing.T) {
	msg := "length of common prefix between strings"

	testVec := []struct {
		Left  []byte
		Right []byte
		Len   int
	}{
		{[]byte(""), []byte(""), 0},
		{[]byte("abc"), []byte("abc"), 3},
		{[]byte(""), []byte("abc"), 0},
		{[]byte("abc"), []byte(""), 0},
		{[]byte("ab"), []byte("abc"), 2},
		{[]byte("abc"), []byte("ab"), 2},
		{[]byte("abcde"), []byte("abc"), 3},
		{[]byte("abc"), []byte("abcde"), 3},
		{[]byte("abcde"), []byte("abc1"), 3},
		{[]byte("abcde"), []byte("abb"), 2},
		{[]byte("abcde"), []byte("qbb"), 0},
		{[]byte("abc"), []byte("abc\x00"), 3},
		{[]byte("abc\x00"), []byte("abc"), 3},
	}

	for _, c := range testVec {
		assert.Equal(t, c.Len, CountPrefixLen(c.Left, c.Right), msg)
	}
}

func TestPrefixLenWide(t *testing.T) {
	// NOTE: these are not cross-language consistent!
	msg := "length of common prefix between strings (wide chars)"

	assert.Equal(t, 9, len("jalapeño"), msg) // 8 in javascript
	assert.Equal(t, 4, len("💩"), msg)        // 2 in javascript
	assert.Equal(t, 18, len("👩‍👧‍👧"), msg)   // 8 in javascript

	testVec := []struct {
		Left  []byte
		Right []byte
		Len   int
	}{
		{[]byte(""), []byte(""), 0},
		{[]byte("jalapeño"), []byte("jalapeno"), 6},
		{[]byte("jalapeñoA"), []byte("jalapeñoB"), 9},
		{[]byte("coöperative"), []byte("coüperative"), 3},
		{[]byte("abc💩abc"), []byte("abcabc"), 3},
		{[]byte("💩abc"), []byte("💩ab"), 6},
		{[]byte("abc👩‍👦‍👦de"), []byte("abc👩‍👧‍👧de"), 13},
	}

	for _, c := range testVec {
		assert.Equal(t, c.Len, CountPrefixLen(c.Left, c.Right), msg)
	}
}
