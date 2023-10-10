package syntax

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInteropAtIdentifiersValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/atidentifier_syntax_valid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseAtIdentifier(line)
		if err != nil {
			fmt.Println("FAILED, GOOD: " + line)
		}
		assert.NoError(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropAtIdentifiersInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/atidentifier_syntax_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseAtIdentifier(line)
		if err == nil {
			fmt.Println("FAILED, BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestDowncase(t *testing.T) {
	assert := assert.New(t)

	aidh, err := ParseAtIdentifier("example.com")
	assert.NoError(err)
	assert.True(aidh.IsHandle())
	assert.False(aidh.IsDID())
	_, err = aidh.AsHandle()
	assert.NoError(err)
	_, err = aidh.AsDID()
	assert.Error(err)

	aidd, err := ParseAtIdentifier("did:web:example.com")
	assert.NoError(err)
	assert.False(aidd.IsHandle())
	assert.True(aidd.IsDID())
	_, err = aidd.AsHandle()
	assert.Error(err)
	_, err = aidd.AsDID()
	assert.NoError(err)
}

func TestEmpty(t *testing.T) {
	assert := assert.New(t)

	atid := AtIdentifier{}

	assert.False(atid.IsHandle())
	assert.False(atid.IsDID())
	assert.Equal(atid, atid.Normalize())
	atid.AsHandle()
	atid.AsDID()
	assert.Empty(atid.String())
}
