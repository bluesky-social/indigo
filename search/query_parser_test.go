package search

import (
	"github.com/stretchr/testify/assert"
	"strconv"
	"testing"
)

func TestParamsToSearchQuery(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToSearchQuery("foo", "0", "10")
	assert.Equal("foo", searchQuery.QueryString)
}

func TestParamsToSearchQueryTrimsSpaces(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToSearchQuery("   foo     ", "0", "10")
	assert.Equal("foo", searchQuery.QueryString)
}

func TestParamsExtractFromUserHandle(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToSearchQuery("from:bob.tld foo", "0", "10")
	assert.Equal("foo", searchQuery.QueryString)
	assert.Equal(&DidHandle{
		Handle: "bob.tld",
		DID:    "",
	}, searchQuery.FromUser)
}

func TestParamsExtractFromOperatorCaseInsensitive(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToSearchQuery("FROM:bob.tld foo", "0", "10")
	assert.Equal("foo", searchQuery.QueryString)
	assert.Equal(&DidHandle{
		Handle: "bob.tld",
		DID:    "",
	}, searchQuery.FromUser)

	searchQuery, _ = paramsToSearchQuery("froM:bob.tld foo", "0", "10")
	assert.Equal("foo", searchQuery.QueryString)
	assert.Equal(&DidHandle{
		Handle: "bob.tld",
		DID:    "",
	}, searchQuery.FromUser)
}

func TestParamsMultipleFromOperatorsPassThroughAsQuery(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToSearchQuery("from:bob.tld from:alice.tld", "0", "10")
	assert.Equal("from:bob.tld from:alice.tld", searchQuery.QueryString)
	assert.Nil(searchQuery.FromUser)
}

func TestParamsExtractAllowEmptyOffsetCount(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToSearchQuery("foobar", "", "")
	assert.Equal(DefaultCount, searchQuery.Count)
	assert.Equal(DefaultOffset, searchQuery.Offset)
}

func TestParamsExtractEnforceMinOffset(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToSearchQuery("foobar", "-100", "")
	assert.Equal(MinOffset, searchQuery.Offset)

	searchQuery, _ = paramsToSearchQuery("foobar", "-1", "")
	assert.Equal(MinOffset, searchQuery.Offset)
}

func TestParamsExtractResetsDefaultCountWhenExceedsMaxCount(t *testing.T) {
	assert := assert.New(t)
	countParamValue := 101
	countParam := strconv.Itoa(countParamValue)
	searchQuery, _ := paramsToSearchQuery("foobar", "", countParam)
	assert.True(countParamValue > MaxCount)
	assert.Equal(DefaultCount, searchQuery.Count)
}

func TestParamsExtractAllowsMaxCount(t *testing.T) {
	assert := assert.New(t)
	countParamValue := MaxCount
	countParam := strconv.Itoa(countParamValue)
	searchQuery, _ := paramsToSearchQuery("foobar", "", countParam)
	assert.Equal(MaxCount, searchQuery.Count)
}

func TestParamsExtractReturnsErrorForBunkCountAndOffset(t *testing.T) {
	assert := assert.New(t)
	_, err := paramsToSearchQuery("foobar", "abc", "")
	assert.Error(err)

	_, err = paramsToSearchQuery("foobar", "", "abc")
	assert.Error(err)
}
