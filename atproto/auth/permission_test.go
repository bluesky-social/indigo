package auth

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoundTrip(t *testing.T) {
	assert := assert.New(t)

	// NOTE: this escapes colons and slashes, which aren't strictly necessary
	testScopes := []string{
		"repo:com.example.record?action=delete",
		"repo?action=delete&collection=com.example.record&collection=com.example.other",
		"rpc:com.example.query?aud=did%3Aweb%3Aapi.example.com%23frag",
		"rpc?aud=did%3Aweb%3Aapi.example.com%23frag&lxm=com.example.query&lxm=com.example.procedure",
		"blob:image/*",
		"blob?accept=image%2Fpng&accept=image%2Fjpeg",
		"account:email?action=manage",
		"identity:handle",
		"include:app.example.authBasics",
	}

	for _, scope := range testScopes {
		p, err := ParsePermissionString(scope)
		assert.NoError(err)
		if err != nil {
			fmt.Println("BAD: " + scope)
			continue
		}
		assert.Equal(scope, p.ScopeString())
	}
}

type GenericExample struct {
	Scope   string            `json:"scope"`
	Generic GenericPermission `json:"generic"`
}

func TestGenericGenericScopesValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/generic_scopes_valid.json")
	if err != nil {
		assert.NoError(err)
		t.Fail()
	}
	defer file.Close()

	var fixtures []GenericExample
	if err := json.NewDecoder(file).Decode(&fixtures); err != nil {
		assert.NoError(err)
		t.Fail()
	}

	for _, fix := range fixtures {
		gp, err := ParseGenericScope(fix.Scope)
		if err != nil {
			fmt.Println("BAD: " + fix.Scope)
			assert.NoError(err)
			continue
		}
		assert.Equal(fix.Generic, *gp)
	}
}

func TestGenericScopesInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/generic_scopes_invalid.txt")
	if err != nil {
		assert.NoError(err)
		t.Fail()
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseGenericScope(line)
		if err != nil {
			fmt.Println("BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestInteropPermissionValid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/permission_scopes_valid.txt")
	if err != nil {
		assert.NoError(err)
		t.Fail()
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParseGenericScope(line)
		if err != nil {
			fmt.Println("BAD: " + line)
		}
		assert.NoError(err)
		p, err := ParsePermissionString(line)
		if err != nil {
			fmt.Println("BAD: " + line)
		}
		assert.NoError(err)
		if p != nil {
			assert.False(p.ScopeString() == "")
		}
	}
	assert.NoError(scanner.Err())
}

func TestInteropPermissionInvalid(t *testing.T) {
	assert := assert.New(t)
	file, err := os.Open("testdata/permission_scopes_invalid.txt")
	if err != nil {
		assert.NoError(err)
		t.Fail()
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		_, err := ParsePermissionString(line)
		if err == nil {
			fmt.Println("BAD: " + line)
		}
		assert.Error(err)
	}
	assert.NoError(scanner.Err())
}

func TestValidBlobAccept(t *testing.T) {
	assert := assert.New(t)

	validAccepts := []string{
		"text/plain",
		"text/*",
	}
	invalidAccepts := []string{
		"",
		"*/png",
		"/plain",
		"text/",
		"text/**",
	}

	for _, val := range validAccepts {
		assert.True(validBlobAccept(val), val)
	}

	for _, val := range invalidAccepts {
		assert.False(validBlobAccept(val), val)
	}

}
