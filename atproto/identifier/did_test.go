package identifier

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
		_, err := NewDID(line)
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
		_, err := NewDID(line)
		if err == nil {
			fmt.Println("BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}
