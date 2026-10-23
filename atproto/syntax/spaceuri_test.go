package syntax

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSpaceURIBasic(t *testing.T) {
	assert := assert.New(t)

	_, err := ParseSpaceURI("at://did:plc:asdf123/space/com.example.space/skey")
	assert.NoError(err)

	_, err = ParseSpaceURI("at://did:plc:asdf123/com.example.space/skey")
	assert.Error(err)
}

func TestInteropSpaceURIsValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/spaceuri_syntax_valid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseSpaceURI(line)
		if err != nil {
			fmt.Println("FAILED, GOOD: " + line)
		}
		assert.NoError(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropSpaceURIsInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/spaceuri_syntax_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseSpaceURI(line)
		if err == nil {
			fmt.Println("FAILED, BAD: " + line)
		}
		assert.Error(err)

		// invalid space URIs are also invalid space refs
		_, err = ParseSpaceRef(line)
		if err == nil {
			fmt.Println("FAILED, BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestSpaceURIParts(t *testing.T) {
	assert := assert.New(t)

	testVec := [][]string{
		{"at://did:abc:123/space/io.nsid.someType/space-key", "did:abc:123", "io.nsid.someType", "space-key", "", "", "", "at://did:abc:123/space/io.nsid.someType/space-key"},
		{"at://did:abc:123/space/io.nsid.someType/space-key/did:abc:456/io.nsid.someRecord/record-key", "did:abc:123", "io.nsid.someType", "space-key", "did:abc:456", "io.nsid.someRecord", "record-key", "at://did:abc:123/space/io.nsid.someType/space-key"},
	}

	for _, parts := range testVec {
		uri, err := ParseSpaceURI(parts[0])
		assert.NoError(err)
		auth := uri.SpaceAuthority()
		assert.Equal(parts[1], auth.String())
		stype := uri.SpaceType()
		assert.Equal(parts[2], stype.String())
		skey := uri.SpaceKey()
		assert.Equal(parts[3], skey.String())
		repo := uri.Repo()
		assert.Equal(parts[4], repo.String())
		col := uri.Collection()
		assert.Equal(parts[5], col.String())
		rkey := uri.RecordKey()
		assert.Equal(parts[6], rkey.String())
		sref := uri.SpaceRef()
		assert.Equal(parts[7], sref.String())
	}
}

func TestSpaceURINoPanic(t *testing.T) {
	for _, s := range []string{"", ".", "at://", "at:///", "at://e.com", "at://e.com/", "at://e.com//"} {
		bad := SpaceURI(s)
		_ = bad.SpaceAuthority()
		_ = bad.SpaceType()
		_ = bad.SpaceKey()
		_ = bad.SpaceRef()
		_ = bad.String()
	}
}
