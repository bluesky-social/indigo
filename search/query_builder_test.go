package search

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func buildEsQueryWith(musts interface{}, size int, from int) interface{} {
	return map[string]interface{}{
		"sort": map[string]any{
			"createdAt": map[string]any{
				"order": "desc",
			},
		},
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": musts,
			},
		},
		"size": size,
		"from": from,
	}
}

func TestToPostsEsQueryJustQueryString(t *testing.T) {
	assert := assert.New(t)
	searchQuery := SearchQuery{
		QueryString: "foo",
		FromUser:    nil,
		Count:       10,
		Offset:      0,
	}
	esQuery, _ := ToPostsEsQuery(searchQuery)
	expected := buildEsQueryWith([]map[string]interface{}{
		{
			"match": map[string]interface{}{
				"text": map[string]any{
					"query":    "foo",
					"operator": "and",
				},
			},
		},
	}, 10, 0)
	assert.Equal(expected, esQuery)
}

func TestToPostsEsQueryFromUserHandle(t *testing.T) {
	assert := assert.New(t)
	searchQuery := SearchQuery{
		QueryString: "",
		FromUser: &DidHandle{
			DID:    "",
			Handle: "alice",
		},
		Count:  10,
		Offset: 0,
	}
	esQuery, _ := ToPostsEsQuery(searchQuery)
	expected := buildEsQueryWith([]map[string]interface{}{
		{
			"term": map[string]interface{}{
				"user": "alice",
			},
		},
	}, 10, 0)
	assert.Equal(expected, esQuery)
}

func TestToPostsEsQueryQueryStringAndHandle(t *testing.T) {
	assert := assert.New(t)
	searchQuery := SearchQuery{
		QueryString: "tacos",
		FromUser: &DidHandle{
			DID:    "",
			Handle: "alice",
		},
		Count:  10,
		Offset: 0,
	}
	esQuery, _ := ToPostsEsQuery(searchQuery)
	expected := buildEsQueryWith([]map[string]interface{}{
		{
			"match": map[string]interface{}{
				"text": map[string]any{
					"query":    "tacos",
					"operator": "and",
				},
			},
		},
		{
			"term": map[string]interface{}{
				"user": "alice",
			},
		},
	}, 10, 0)
	assert.Equal(expected, esQuery)
}

func TestToPostsEsQueryCountToSize(t *testing.T) {
	assert := assert.New(t)
	countSize := 123
	searchQuery := SearchQuery{
		QueryString: "tacos",
		FromUser:    nil,
		Count:       countSize,
		Offset:      0,
	}
	esQuery, _ := ToPostsEsQuery(searchQuery)
	assert.Equal(countSize, esQuery["size"])
}

func TestToPostsEsQueryOffsetToFrom(t *testing.T) {
	assert := assert.New(t)
	offsetValue := 123
	searchQuery := SearchQuery{
		QueryString: "tacos",
		FromUser:    nil,
		Count:       10,
		Offset:      offsetValue,
	}
	esQuery, _ := ToPostsEsQuery(searchQuery)
	assert.Equal(offsetValue, esQuery["from"])
}

func TestErrorWhenZeroMusts(t *testing.T) {
	assert := assert.New(t)
	searchQuery := SearchQuery{
		QueryString: "",
		FromUser:    nil,
		Count:       10,
		Offset:      0,
	}
	_, err := ToPostsEsQuery(searchQuery)
	assert.Error(err)
}
