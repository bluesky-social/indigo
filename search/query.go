package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	es "github.com/opensearch-project/opensearch-go/v2"
)

type EsSearchHit struct {
	Index  string          `json:"_index"`
	ID     string          `json:"_id"`
	Score  float64         `json:"_score"`
	Source json.RawMessage `json:"_source"`
}

type EsSearchHits struct {
	Total struct {
		Value    int
		Relation string
	} `json:"total"`
	MaxScore float64       `json:"max_score"`
	Hits     []EsSearchHit `json:"hits"`
}

type EsSearchResponse struct {
	Took     int  `json:"took"`
	TimedOut bool `json:"timed_out"`
	// Shards ???
	Hits EsSearchHits `json:"hits"`
}

type UserResult struct {
	Did    string `json:"did"`
	Handle string `json:"handle"`
}

type PostSearchResult struct {
	Tid  string     `json:"tid"`
	Cid  string     `json:"cid"`
	User UserResult `json:"user"`
	Post any        `json:"post"`
}

func doSearchPosts(
	ctx context.Context,
	escli *es.Client,
	searchQuery SearchQuery,
) (*EsSearchResponse, error) {
	var musts []map[string]interface{}
	if len(searchQuery.QueryString) > 0 {
		musts = append(musts, map[string]interface{}{
			"match": map[string]interface{}{
				"text": map[string]any{
					"query":    searchQuery.QueryString,
					"operator": "and",
				},
			},
		})
	}
	if searchQuery.FromUser != nil {
		fromHandle := searchQuery.FromUser.Handle
		if len(fromHandle) > 0 {
			musts = append(musts, map[string]interface{}{
				"term": map[string]interface{}{
					"user": fromHandle,
				},
			})
		}
	}

	query := map[string]interface{}{
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

	return doSearch(ctx, escli, "posts", query)
}

func doSearchProfiles(ctx context.Context, escli *es.Client, q string) (*EsSearchResponse, error) {
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":    q,
				"fields":   []string{"description", "displayName", "handle"},
				"operator": "or",
			},
		},
	}

	return doSearch(ctx, escli, "profiles", query)
}

func doSearch(ctx context.Context, escli *es.Client, index string, query interface{}) (*EsSearchResponse, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(query); err != nil {
		log.Fatalf("Error encoding query: %s", err)
	}

	// Perform the search request.
	res, err := escli.Search(
		escli.Search.WithContext(ctx),
		escli.Search.WithIndex(index),
		escli.Search.WithBody(&buf),
		escli.Search.WithTrackTotalHits(true),
		escli.Search.WithSize(30),
	)
	if err != nil {
		log.Fatalf("Error getting response: %s", err)
	}
	defer res.Body.Close()

	var out EsSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}

	return &out, nil
}
