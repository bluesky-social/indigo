package identifier

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInteropHandlesValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/handle_syntax_valid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := NewHandle(line)
		if err != nil {
			fmt.Println("GOOD: " + line)
		}
		assert.NoError(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropHandlesInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/handle_syntax_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := NewHandle(line)
		if err == nil {
			fmt.Println("BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestHandleNormalize(t *testing.T) {
	assert := assert.New(t)

	handle, err := NewHandle("JoHn.TeST")
	assert.NoError(err)
	assert.Equal(string(handle.Normalize()), "john.test")
	assert.NoError(err)

	_, err = NewHandle("JoH!n.TeST")
	assert.Error(err)
}
