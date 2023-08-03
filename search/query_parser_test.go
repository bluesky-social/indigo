package search

import (
	"github.com/stretchr/testify/assert"
	"strconv"
	"testing"
)

func TestParamsToPostsSearchQuery(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToPostsSearchQuery("foo", "0", "10")
	assert.Equal("foo", searchQuery.QueryString)
}

func TestParamsToPostsSearchQueryTrimsSpaces(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToPostsSearchQuery("   foo     ", "0", "10")
	assert.Equal("foo", searchQuery.QueryString)
}

func TestParamsToPostsSearchQueryExtractFromUserHandle(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToPostsSearchQuery("from:bob.tld foo", "0", "10")
	assert.Equal("foo", searchQuery.QueryString)
	assert.Equal(&DidHandle{
		Handle: "bob.tld",
		DID:    "",
	}, searchQuery.FromUser)
}

func TestParamsToPostsSearchQueryExtractFromOperatorCaseInsensitive(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToPostsSearchQuery("FROM:bob.tld foo", "0", "10")
	assert.Equal("foo", searchQuery.QueryString)
	assert.Equal(&DidHandle{
		Handle: "bob.tld",
		DID:    "",
	}, searchQuery.FromUser)

	searchQuery, _ = paramsToPostsSearchQuery("froM:bob.tld foo", "0", "10")
	assert.Equal("foo", searchQuery.QueryString)
	assert.Equal(&DidHandle{
		Handle: "bob.tld",
		DID:    "",
	}, searchQuery.FromUser)
}

func TestParamsToPostsSearchQueryMultipleFromOperatorsPassThroughAsQuery(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToPostsSearchQuery("from:bob.tld from:alice.tld", "0", "10")
	assert.Equal("from:bob.tld from:alice.tld", searchQuery.QueryString)
	assert.Nil(searchQuery.FromUser)
}

func TestParamsToPostsSearchQueryExtractAllowEmptyOffsetCount(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToPostsSearchQuery("foobar", "", "")
	assert.Equal(SearchQueryCountDefault, searchQuery.Count)
	assert.Equal(SearchQueryOffsetDefault, searchQuery.Offset)
}

func TestParamsToPostsSearchQueryExtractEnforceMinOffset(t *testing.T) {
	assert := assert.New(t)
	searchQuery, _ := paramsToPostsSearchQuery("foobar", "-100", "")
	assert.Equal(SearchQueryOffsetMin, searchQuery.Offset)

	searchQuery, _ = paramsToPostsSearchQuery("foobar", "-1", "")
	assert.Equal(SearchQueryOffsetMin, searchQuery.Offset)
}

func TestParamsToPostsSearchQueryExtractResetsDefaultCountWhenExceedsMaxCount(t *testing.T) {
	assert := assert.New(t)
	countParamValue := 101
	countParam := strconv.Itoa(countParamValue)
	searchQuery, _ := paramsToPostsSearchQuery("foobar", "", countParam)
	assert.True(countParamValue > SearchQueryCountMax)
	assert.Equal(SearchQueryCountDefault, searchQuery.Count)
}

func TestParamsToPostsSearchQueryExtractAllowsMaxCount(t *testing.T) {
	assert := assert.New(t)
	countParamValue := SearchQueryCountMax
	countParam := strconv.Itoa(countParamValue)
	searchQuery, _ := paramsToPostsSearchQuery("foobar", "", countParam)
	assert.Equal(SearchQueryCountMax, searchQuery.Count)
}

func TestParamsToPostsSearchQueryExtractReturnsErrorForBunkCountAndOffset(t *testing.T) {
	assert := assert.New(t)
	_, err := paramsToPostsSearchQuery("foobar", "abc", "")
	assert.Error(err)

	_, err = paramsToPostsSearchQuery("foobar", "", "abc")
	assert.Error(err)
}
