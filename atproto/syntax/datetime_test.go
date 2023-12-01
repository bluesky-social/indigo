package syntax

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInteropDatetimeValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/datetime_syntax_valid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseDatetimeTime(line)
		if err != nil {
			fmt.Println("GOOD: " + line)
		}
		assert.NoError(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropDatetimeInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/datetime_syntax_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseDatetime(line)
		if err == nil {
			fmt.Println("BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropDatetimeTimeInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/datetime_parse_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseDatetime(line)
		if err == nil {
			fmt.Println("BAD: " + line)
		}
		assert.Error(err)
		_, err = ParseDatetimeTime(line)
		if err == nil {
			fmt.Println("BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestParseDatetimeLenient(t *testing.T) {
	assert := assert.New(t)

	valid := []string{
		"1985-04-12T23:20:50.123Z",
		"1985-04-12T23:20:50.123",
		"2023-08-27T19:07:00.186173",
		"1985-04-12T23:20:50.123-00:00",
		"1985-04-12T23:20:50.123+00:00",
		"1985-04-12T23:20:50.123+0000",
		"1985-04-12T23:20:50.123-0000",
		"2023-11-12T11:20:01+0000",
	}
	for _, s := range valid {
		_, err := ParseDatetimeLenient(s)
		assert.NoError(err)
		if err != nil {
			fmt.Println(s)
		}
	}

	invalid := []string{
		"1985-04-",
		"",
		"blah",
	}
	for _, s := range invalid {
		_, err := ParseDatetimeLenient(s)
		assert.Error(err)
	}
}

func TestDatetimeNow(t *testing.T) {
	assert := assert.New(t)

	dt := DatetimeNow()
	_, err := ParseDatetimeTime(dt.String())
	assert.NoError(err)
}
