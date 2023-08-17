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
