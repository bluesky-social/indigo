package syntax

import (
	"bufio"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInteropTIDsValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/tid_syntax_valid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseTID(line)
		if err != nil {
			fmt.Println("GOOD: " + line)
		}
		assert.NoError(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropTIDsInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/tid_syntax_invalid.txt")
	assert.NoError(err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseTID(line)
		if err == nil {
			fmt.Println("BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestTIDParts(t *testing.T) {
	assert := assert.New(t)

	raw := "3kao2cl6lyj2p"
	tid, err := ParseTID(raw)
	assert.NoError(err)
	// TODO: assert.Equal(uint64(0x181a8044491f3bec), tid.Integer())
	// TODO: assert.Equal(uint(1004), tid.ClockID())
	assert.Equal(2023, tid.Time().Year())

	out := NewTID(tid.Time().UnixMicro(), tid.ClockID())
	assert.Equal(raw, out.String())
	assert.Equal(tid.ClockID(), out.ClockID())
	assert.Equal(tid.Time(), out.Time())
	assert.Equal(tid.Integer(), out.Integer())

	out2 := NewTIDFromInteger(tid.Integer())
	assert.Equal(tid.Integer(), out2.Integer())
}

func TestTIDExamples(t *testing.T) {
	assert := assert.New(t)
	// TODO: seems like TS code might be wrong? "242k52k4kg3s2"
	assert.Equal("242k52k4kg3sc", NewTIDFromInteger(0x0102030405060708).String())
	assert.Equal(uint64(0x0102030405060708), TID("242k52k4kg3sc").Integer())
	//assert.Equal("2222222222222", NewTIDFromInteger(0x0000000000000000).String())
	//assert.Equal(uint64(), TID("242k52k4kg3s2").Integer())
	assert.Equal("2222222222223", NewTIDFromInteger(0x0000000000000001).String())
	assert.Equal(uint64(0x0000000000000001), TID("2222222222223").Integer())

	assert.Equal("6222222222222", NewTIDFromInteger(0x4000000000000000).String())
	assert.Equal(uint64(0x4000000000000000), TID("6222222222222").Integer())

	// ignoring type byte
	assert.Equal("2222222222222", NewTIDFromInteger(0x8000000000000000).String())
}

func TestTIDNoPanic(t *testing.T) {
	for _, s := range []string{"", "3jzfcijpj2z2aa", "3jzfcijpj2z2", ".."} {
		bad := TID(s)
		_ = bad.ClockID()
		_ = bad.Integer()
		_ = bad.Time()
		_ = bad.String()
	}
}

func TestTIDConstruction(t *testing.T) {
	assert := assert.New(t)

	zero := NewTID(0, 0)
	assert.Equal("2222222222222", zero.String())
	assert.Equal(uint64(0), zero.Integer())
	assert.Equal(uint(0), zero.ClockID())
	assert.Equal(time.UnixMilli(0).UTC(), zero.Time())

	now := NewTIDNow(1011)
	assert.Equal(uint(1011), now.ClockID())
	assert.True(time.Since(now.Time()) < time.Minute)

	over := NewTIDNow(4096)
	assert.Equal(uint(0), over.ClockID())
}

func TestTIDClock(t *testing.T) {
	assert := assert.New(t)

	clk := NewTIDClock(0)
	last := NewTID(0, 0)
	for i := 0; i < 100; i++ {
		next := clk.Next()
		assert.Greater(next, last)
		last = next
	}
}
