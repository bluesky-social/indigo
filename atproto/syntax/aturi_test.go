package syntax

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInteropATURIsValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/aturi_syntax_valid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		aturi, err := ParseATURI(line)
		if err != nil {
			fmt.Println("FAILED, GOOD: " + line)
		}
		assert.NoError(err)

		// check that Path() is working
		col := aturi.Collection()
		rkey := aturi.RecordKey()
		if rkey != "" {
			assert.Equal(col.String()+"/"+rkey.String(), aturi.Path())
		} else if col != "" {
			assert.Equal(col.String(), aturi.Path())
		}
	}
	assert.NoError(scanner.Err())
}

func TestInteropATURIsInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/aturi_syntax_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseATURI(line)
		if err == nil {
			fmt.Println("FAILED, BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestATURIParts(t *testing.T) {
	assert := assert.New(t)

	testVec := [][]string{
		{"at://did:abc:123/io.nsid.someFunc/record-key", "did:abc:123", "io.nsid.someFunc", "record-key"},
		{"at://e.com", "e.com", "", ""},
	}

	for _, parts := range testVec {
		uri, err := ParseATURI(parts[0])
		assert.NoError(err)
		auth := uri.Authority()
		assert.Equal(parts[1], auth.String())
		col := uri.Collection()
		assert.Equal(parts[2], col.String())
		rkey := uri.RecordKey()
		assert.Equal(parts[3], rkey.String())
	}
}

func TestATURIPath(t *testing.T) {
	assert := assert.New(t)

	uri1, err := ParseATURI("at://did:abc:123/io.nsid.someFunc/record-key")
	assert.NoError(err)
	assert.Equal("io.nsid.someFunc/record-key", uri1.Path())

	uri2, err := ParseATURI("at://did:abc:123/io.nsid.someFunc")
	assert.NoError(err)
	assert.Equal("io.nsid.someFunc", uri2.Path())

	uri3, err := ParseATURI("at://did:abc:123")
	assert.NoError(err)
	assert.Equal("", uri3.Path())
}

func TestATURINormalize(t *testing.T) {
	assert := assert.New(t)

	testVec := [][]string{
		{"at://did:abc:123/io.NsId.someFunc/record-KEY", "at://did:abc:123/io.nsid.someFunc/record-KEY"},
		{"at://E.com", "at://e.com"},
	}

	for _, parts := range testVec {
		uri, err := ParseATURI(parts[0])
		assert.NoError(err)
		assert.Equal(parts[1], uri.Normalize().String())
	}
}

func TestATURINoPanic(t *testing.T) {
	for _, s := range []string{"", ".", "at://", "at:///", "at://e.com", "at://e.com/", "at://e.com//"} {
		bad := ATURI(s)
		_ = bad.Authority()
		_ = bad.Collection()
		_ = bad.RecordKey()
		_ = bad.Normalize()
		_ = bad.String()
		_ = bad.Path()
	}
}
