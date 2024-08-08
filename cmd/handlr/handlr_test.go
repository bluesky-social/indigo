package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseHandle(t *testing.T) {
	assert := assert.New(t)
	hr := HTTPResolver{}

	fixtures := []struct {
		domain string
		valid  bool
		hdl    string
	}{
		{domain: "asdf", valid: false, hdl: ""},
		{domain: "", valid: false, hdl: ""},
		{domain: "_atproto.blah", valid: false, hdl: ""},
		{domain: "_atproto.handle.example.com.", valid: true, hdl: "handle.example.com"},
		{domain: "_atproto.handle.example.com", valid: true, hdl: "handle.example.com"},
	}

	for _, fix := range fixtures {
		hdl, err := hr.parseDomain(fix.domain)
		if fix.valid {
			assert.NoError(err)
			assert.Equal(fix.hdl, hdl.String())
		} else {
			assert.Error(err)
		}
	}
}
