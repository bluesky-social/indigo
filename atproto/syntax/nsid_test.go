package syntax

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInteropNSIDsValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/nsid_syntax_valid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseNSID(line)
		if err != nil {
			fmt.Println("FAILED, GOOD: " + line)
		}
		assert.NoError(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropNSIDsInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/nsid_syntax_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseNSID(line)
		if err == nil {
			fmt.Println("FAILED, BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestNSIDParts(t *testing.T) {
	assert := assert.New(t)
	d, err := ParseNSID("cOm.ExAmple.blahFunc")
	assert.NoError(err)
	assert.Equal("example.com", d.Authority())
	assert.Equal("blahFunc", d.Name())
}

func TestNSIDNormalize(t *testing.T) {
	assert := assert.New(t)

	nsid, err := ParseNSID("cOm.ExAmple.blahFunc")
	assert.NoError(err)
	assert.Equal("com.example.blahFunc", string(nsid.Normalize()))
	assert.NoError(err)
}

func TestNSIDNoPanic(t *testing.T) {
	for _, s := range []string{"", ".", ".."} {
		bad := NSID(s)
		_ = bad.Authority()
		_ = bad.Name()
		_ = bad.Normalize()
	}
}
