package syntax

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSpaceRefBasic(t *testing.T) {
	assert := assert.New(t)

	_, err := ParseSpaceRef("at://did:plc:asdf123/space/com.example.space/skey")
	assert.NoError(err)

	_, err = ParseSpaceRef("at://did:plc:asdf123/com.example.space/skey")
	assert.Error(err)

	ref := NewSpaceRef(DID("did:plc:asdf123"), NSID("com.example.space"), RecordKey("skey"))
	_, err = ParseSpaceRef(ref.String())
	assert.NoError(err)
}

func TestInteropSpaceRefsValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/spaceref_syntax_valid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseSpaceRef(line)
		if err != nil {
			fmt.Println("FAILED, GOOD: " + line)
		}
		assert.NoError(err)

		// valid SpaceRef are also valid SpaceURI
		_, err = ParseSpaceURI(line)
		if err != nil {
			fmt.Println("FAILED, GOOD: " + line)
		}
		assert.NoError(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropSpaceRefsInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/spaceref_syntax_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseSpaceRef(line)
		if err == nil {
			fmt.Println("FAILED, BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestSpaceRefParts(t *testing.T) {
	assert := assert.New(t)

	testVec := [][]string{
		{"at://did:abc:123/space/io.nsid.someType/space-key", "did:abc:123", "io.nsid.someType", "space-key"},
	}

	for _, parts := range testVec {
		uri, err := ParseSpaceRef(parts[0])
		assert.NoError(err)
		auth := uri.SpaceAuthority()
		assert.Equal(parts[1], auth.String())
		stype := uri.SpaceType()
		assert.Equal(parts[2], stype.String())
		skey := uri.SpaceKey()
		assert.Equal(parts[3], skey.String())
	}
}

func TestSpaceRefNoPanic(t *testing.T) {
	for _, s := range []string{"", ".", "at://", "at:///", "at://e.com", "at://e.com/", "at://e.com//"} {
		bad := SpaceRef(s)
		_ = bad.SpaceAuthority()
		_ = bad.SpaceType()
		_ = bad.SpaceKey()
		_ = bad.String()
	}
}
