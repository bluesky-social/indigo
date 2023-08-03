package search

import "fmt"

var /* const */ DidEsField = "did"
var /* const */ TextEsField = "text"
var /* const */ UserEsField = "user"

func ToPostsEsQuery(searchQuery SearchQuery) (map[string]interface{}, error) {
	var musts []map[string]interface{}
	if len(searchQuery.QueryString) > 0 {
		musts = append(musts, map[string]interface{}{
			"match": map[string]interface{}{
				TextEsField: map[string]any{
					"query":    searchQuery.QueryString,
					"operator": "and",
				},
			},
		})
	}
	if fromUser := searchQuery.FromUser; fromUser != nil {
		if len(fromUser.DID) > 0 {
			musts = append(musts, map[string]interface{}{
				"term": map[string]interface{}{
					DidEsField: fromUser.DID,
				},
			})
		} else if len(fromUser.Handle) > 0 {
			musts = append(musts, map[string]interface{}{
				"term": map[string]interface{}{
					UserEsField: fromUser.Handle,
				},
			})
		}
	}

	if len(musts) == 0 {
		return nil, fmt.Errorf("requires at least one must in bool query")
	}

	esQuery := map[string]interface{}{
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
		"size": searchQuery.Count,
		"from": searchQuery.Offset,
	}

	return esQuery, nil
}
