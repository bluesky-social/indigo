package syntax

import (
	"bufio"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInteropRecordKeysValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/recordkey_syntax_valid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseRecordKey(line)
		if err != nil {
			t.Log("FAILED, GOOD: " + line)
		}
		assert.NoError(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropRecordKeysInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/recordkey_syntax_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseRecordKey(line)
		if err == nil {
			t.Log("FAILED, BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestRecordKeyNoPanic(t *testing.T) {
	for _, s := range []string{"", "a", ".", ".."} {
		bad := RecordKey(s)
		_ = bad.String()
	}
}
