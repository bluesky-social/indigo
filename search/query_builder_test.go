package search

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func esQueryWith(musts interface{}, size int, from int) interface{} {
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

func TestToEsQueryJustQueryString(t *testing.T) {
	assert := assert.New(t)
	searchQuery := SearchQuery{
		QueryString: "foo",
		FromUser:    nil,
		Count:       10,
		Offset:      0,
	}
	esQuery, _ := ToPostsEsQuery(searchQuery)
	expected := esQueryWith([]map[string]interface{}{
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

func TestToEsQueryFromUserHandle(t *testing.T) {
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
	expected := esQueryWith([]map[string]interface{}{
		{
			"term": map[string]interface{}{
				"user": "alice",
			},
		},
	}, 10, 0)
	assert.Equal(expected, esQuery)
}

func TestToEsQueryQueryStringAndHandle(t *testing.T) {
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
	expected := esQueryWith([]map[string]interface{}{
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

func TestToEsQueryCountToSize(t *testing.T) {
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

func TestToEsQueryOffsetToFrom(t *testing.T) {
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
