package syntax

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInteropDIDsValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/did_syntax_valid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseDID(line)
		if err != nil {
			fmt.Println("GOOD: " + line)
		}
		assert.NoError(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropDIDsInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/did_syntax_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseDID(line)
		if err == nil {
			fmt.Println("BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestDIDParts(t *testing.T) {
	assert := assert.New(t)
	d, err := ParseDID("did:example:123456789abcDEFghi")
	assert.NoError(err)
	assert.Equal("example", d.Method())
	assert.Equal("123456789abcDEFghi", d.Identifier())
	assert.Equal(d.String(), d.AtIdentifier().String())
}

func TestDIDNoPanic(t *testing.T) {
	for _, s := range []string{"", ":", "::"} {
		bad := DID(s)
		_ = bad.Identifier()
		_ = bad.Method()
		_ = bad.AtIdentifier()
		_ = bad.AtIdentifier().String()
	}
}

func TestParseDIDRef(t *testing.T) {
	assert := assert.New(t)

	invalidRefs := []string{
		"#",
		"#name",
		"name",
		"did:web:example.com#",
		"did:web:example.com##name",
		"did:web:example.com#9090",
		"did:web:example.com#_name",
		"did:web:example.com#-name",
		"did:web:example.com#one.two",
		"did:web:example.com#one:two",
		"did:web:example.com#one/two",
		"did:web:example.com#näme",
	}

	validRefs := [][]string{
		{"did:web:example.com#name", "did:web:example.com", "name"},
		{"did:web:example.com#name123", "did:web:example.com", "name123"},
		{"did:web:example.com#one_two", "did:web:example.com", "one_two"},
		{"did:web:example.com#one-two", "did:web:example.com", "one-two"},
		{"did:web:example.com#one-", "did:web:example.com", "one-"},
		{"did:web:example.com#one_", "did:web:example.com", "one_"},
		{"did:web:example.com", "did:web:example.com", ""},
	}

	for _, row := range invalidRefs {
		_, _, err := ParseDIDRef(row)
		assert.Error(err, row)
	}

	for _, row := range validRefs {
		did, ref, err := ParseDIDRef(row[0])
		assert.Equal(row[1], did.String())
		assert.Equal(row[2], ref)
		assert.NoError(err)
	}
}
