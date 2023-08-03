package search

import (
	"github.com/stretchr/testify/assert"
	"strconv"
	"testing"
)

func TestParamsToSearchQuery(t *testing.T) {
	searchQuery, _ := paramsToSearchQuery("foo", "0", "10")
	assert.Equal(t, "foo", searchQuery.QueryString)
}

func TestParamsToSearchQueryTrimsSpaces(t *testing.T) {
	searchQuery, _ := paramsToSearchQuery("   foo     ", "0", "10")
	assert.Equal(t, "foo", searchQuery.QueryString)
}

func TestParamsExtractFromUserHandle(t *testing.T) {
	searchQuery, _ := paramsToSearchQuery("from:bob.tld foo", "0", "10")
	assert.Equal(t, "foo", searchQuery.QueryString)
	assert.Equal(t, &DidHandle{
		Handle: "bob.tld",
		DID:    "",
	}, searchQuery.FromUser)
}

func TestParamsExtractFromOperatorCaseInsensitive(t *testing.T) {
	searchQuery, _ := paramsToSearchQuery("FROM:bob.tld foo", "0", "10")
	assert.Equal(t, "foo", searchQuery.QueryString)
	assert.Equal(t, &DidHandle{
		Handle: "bob.tld",
		DID:    "",
	}, searchQuery.FromUser)

	searchQuery, _ = paramsToSearchQuery("froM:bob.tld foo", "0", "10")
	assert.Equal(t, "foo", searchQuery.QueryString)
	assert.Equal(t, &DidHandle{
		Handle: "bob.tld",
		DID:    "",
	}, searchQuery.FromUser)
}

func TestParamsExtractFromUserDid(t *testing.T) {
	searchQuery, _ := paramsToSearchQuery("from:did:plc:testing foo", "0", "10")
	assert.Equal(t, "foo", searchQuery.QueryString)
	assert.Equal(t, &DidHandle{
		Handle: "",
		DID:    "did:plc:testing",
	}, searchQuery.FromUser)
}

func TestParamsMultipleFromOperatorsPassThroughAsQuery(t *testing.T) {
	searchQuery, _ := paramsToSearchQuery("from:bob.tld from:alice.tld", "0", "10")
	assert.Equal(t, "from:bob.tld from:alice.tld", searchQuery.QueryString)
	assert.Nil(t, searchQuery.FromUser)
}

func TestParamsExtractAllowEmptyOffsetCount(t *testing.T) {
	searchQuery, _ := paramsToSearchQuery("foobar", "", "")
	assert.Equal(t, DefaultCount, searchQuery.Count)
	assert.Equal(t, DefaultOffset, searchQuery.Offset)
}

func TestParamsExtractEnforceMinOffset(t *testing.T) {
	searchQuery, _ := paramsToSearchQuery("foobar", "-100", "")
	assert.Equal(t, MinOffset, searchQuery.Offset)

	searchQuery, _ = paramsToSearchQuery("foobar", "-1", "")
	assert.Equal(t, MinOffset, searchQuery.Offset)
}

func TestParamsExtractResetsDefaultCountWhenExceedsMaxCount(t *testing.T) {
	countParamValue := 101
	countParam := strconv.Itoa(countParamValue)
	searchQuery, _ := paramsToSearchQuery("foobar", "", countParam)
	assert.True(t, countParamValue > MaxCount)
	assert.Equal(t, DefaultCount, searchQuery.Count)
}

func TestParamsExtractAllowsMaxCount(t *testing.T) {
	countParamValue := MaxCount
	countParam := strconv.Itoa(countParamValue)
	searchQuery, _ := paramsToSearchQuery("foobar", "", countParam)
	assert.Equal(t, MaxCount, searchQuery.Count)
}

func TestParamsExtractReturnsErrorForBunkCountAndOffset(t *testing.T) {
	_, err := paramsToSearchQuery("foobar", "abc", "")
	assert.Error(t, err)

	_, err = paramsToSearchQuery("foobar", "", "abc")
	assert.Error(t, err)
}
