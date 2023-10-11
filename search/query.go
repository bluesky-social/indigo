package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log/slog"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"

	es "github.com/opensearch-project/opensearch-go/v2"
)

type EsSearchHit struct {
	Index  string          `json:"_index"`
	ID     string          `json:"_id"`
	Score  float64         `json:"_score"`
	Source json.RawMessage `json:"_source"`
}

type EsSearchHits struct {
	Total struct { // not used
		Value    int
		Relation string
	} `json:"total"`
	MaxScore float64       `json:"max_score"`
	Hits     []EsSearchHit `json:"hits"`
}

type EsSearchResponse struct {
	Took     int          `json:"took"`
	TimedOut bool         `json:"timed_out"`
	Hits     EsSearchHits `json:"hits"`
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

func checkParams(offset, size int) error {
	if offset+size > 10000 || size > 250 || offset > 10000 || offset < 0 || size < 0 {
		return fmt.Errorf("disallowed size/offset parameters")
	}
	return nil
}

func DoSearchPosts(ctx context.Context, dir identity.Directory, escli *es.Client, index, q string, offset, size int) (*EsSearchResponse, error) {
	if err := checkParams(offset, size); err != nil {
		return nil, err
	}
	queryStr, filters := ParseQuery(ctx, dir, q)
	basic := map[string]interface{}{
		"simple_query_string": map[string]interface{}{
			"query":            queryStr,
			"fields":           []string{"everything"},
			"flags":            "AND|NOT|OR|PHRASE|PRECEDENCE|WHITESPACE",
			"default_operator": "and",
			"lenient":          true,
			"analyze_wildcard": false,
		},
	}
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must":   basic,
				"filter": filters,
			},
		},
		"sort": map[string]any{
			"created_at": map[string]any{
				"order": "desc",
			},
		},
		"size": size,
		"from": offset,
	}

	return doSearch(ctx, escli, index, query)
}

func DoSearchProfiles(ctx context.Context, dir identity.Directory, escli *es.Client, index, q string, offset, size int) (*EsSearchResponse, error) {
	if err := checkParams(offset, size); err != nil {
		return nil, err
	}

	queryStr, filters := ParseQuery(ctx, dir, q)
	basic := map[string]interface{}{
		"simple_query_string": map[string]interface{}{
			"query":            queryStr,
			"fields":           []string{"everything"},
			"flags":            "AND|NOT|OR|PHRASE|PRECEDENCE|WHITESPACE",
			"default_operator": "and",
			"lenient":          true,
			"analyze_wildcard": false,
		},
	}

	query := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": basic,
				"should": []interface{}{
					map[string]interface{}{"term": map[string]interface{}{"has_avatar": true}},
					map[string]interface{}{"term": map[string]interface{}{"has_banner": true}},
				},
				"minimum_should_match": 0,
				"filter":               filters,
				"boost":                0.5,
			},
		},
		"size": size,
		"from": offset,
	}

	// special-case: exact string match of handle
	if strings.HasPrefix(q, "@") && !strings.Contains(q, " ") {
		q = q[1:]
		query["query"] = map[string]interface{}{
			"prefix": map[string]interface{}{
				"handle": map[string]interface{}{
					"value": q,
				},
			},
		}
	}

	// boost exact handle prefix match, if q is single simple term
	if len(q) >= 3 && !strings.ContainsAny(q, " .") {
		query["rescore"] = map[string]interface{}{
			"window_size": 100,
			"query": map[string]interface{}{
				"rescore_query": map[string]interface{}{
					"boosting": map[string]interface{}{
						"positive": map[string]interface{}{
							"prefix": map[string]interface{}{
								"handle": map[string]interface{}{
									"value": q + ".",
								},
							},
						},
						// downrank *.bsky.social (vs custom domain)
						// wildcard is expensive, so only in rescore
						"negative": map[string]interface{}{
							"wildcard": map[string]interface{}{
								"handle": map[string]interface{}{
									"value": "*.bsky.social",
								},
							},
						},
						"negative_boost": 0.5,
					},
				},
				"query_weight":         0.5,
				"rescore_query_weight": 2.0,
			},
		}
	}

	return doSearch(ctx, escli, index, query)
}

func DoSearchProfilesTypeahead(ctx context.Context, escli *es.Client, index, q string, size int) (*EsSearchResponse, error) {
	if err := checkParams(0, size); err != nil {
		return nil, err
	}

	query := map[string]interface{}{
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":    q,
				"type":     "bool_prefix",
				"operator": "and",
				"fields": []string{
					"typeahead",
					"typeahead._2gram",
					"typeahead._3gram",
				},
			},
		},
		"size": size,
	}

	// special-case: exact string match of handle
	if strings.HasPrefix(q, "@") && !strings.Contains(q, " ") {
		q = q[1:]
		query["query"] = map[string]interface{}{
			"prefix": map[string]interface{}{
				"handle": map[string]interface{}{
					"value": q,
				},
			},
		}
	}

	// boost exact handle prefix match, if q is single simple term
	if len(q) >= 3 && !strings.ContainsAny(q, " .") {
		query["rescore"] = map[string]interface{}{
			"window_size": 100,
			"query": map[string]interface{}{
				"rescore_query": map[string]interface{}{
					"boosting": map[string]interface{}{
						"positive": map[string]interface{}{
							"prefix": map[string]interface{}{
								"handle": map[string]interface{}{
									"value": q,
								},
							},
							// additional boost if it is the full first name
							"prefix": map[string]interface{}{
								"handle": map[string]interface{}{
									"value": q + ".",
								},
							},
						},
						// downrank *.bsky.social (vs custom domain)
						// wildcard is expensive, so only in rescore
						"negative": map[string]interface{}{
							"wildcard": map[string]interface{}{
								"handle": map[string]interface{}{
									"value": "*.bsky.social",
								},
							},
						},
						"negative_boost": 0.5,
					},
				},
				"query_weight":         1.0,
				"rescore_query_weight": 1.0,
			},
		}
	}

	return doSearch(ctx, escli, index, query)
}

// helper to do a full-featured Lucene query parser (query_string) search, with all possible facets. Not safe to expose publicly.
func DoSearchGeneric(ctx context.Context, escli *es.Client, index, q string) (*EsSearchResponse, error) {
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"query_string": map[string]interface{}{
				"query":                  q,
				"default_operator":       "and",
				"analyze_wildcard":       true,
				"allow_leading_wildcard": false,
				"lenient":                true,
				"default_field":          "everything",
			},
		},
	}

	return doSearch(ctx, escli, index, query)
}

func doSearch(ctx context.Context, escli *es.Client, index string, query interface{}) (*EsSearchResponse, error) {
	b, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize query: %w", err)
	}
	slog.Info("sending query", "index", index, "query", string(b))

	// Perform the search request.
	res, err := escli.Search(
		escli.Search.WithContext(ctx),
		escli.Search.WithIndex(index),
		escli.Search.WithBody(bytes.NewBuffer(b)),
	)
	if err != nil {
		return nil, fmt.Errorf("search query error: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		raw, err := ioutil.ReadAll(res.Body)
		if nil == err {
			slog.Warn("search query error", "resp", string(raw), "status_code", res.StatusCode)
		}
		return nil, fmt.Errorf("search query error, code=%d", res.StatusCode)
	}

	var out EsSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}

	return &out, nil
}
