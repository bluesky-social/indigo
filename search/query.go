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
	"github.com/bluesky-social/indigo/atproto/syntax"

	es "github.com/opensearch-project/opensearch-go/v2"
	"go.opentelemetry.io/otel/attribute"
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

type PostSearchParams struct {
	Query    string           `json:"q"`
	Sort     string           `json:"sort"`
	Author   *syntax.DID      `json:"author"`
	Since    *syntax.Datetime `json:"since"`
	Until    *syntax.Datetime `json:"until"`
	Mentions *syntax.DID      `json:"mentions"`
	Lang     *syntax.Language `json:"lang"`
	Domain   string           `json:"domain"`
	URL      string           `json:"url"`
	Tags     []string         `json:"tag"`
	Viewer   *syntax.DID      `json:"viewer"`
	Offset   int              `json:"offset"`
	Size     int              `json:"size"`
}

type ActorSearchParams struct {
	Query     string       `json:"q"`
	Typeahead bool         `json:"typeahead"`
	Follows   []syntax.DID `json:"follows"`
	Viewer    *syntax.DID  `json:"viewer"`
	Offset    int          `json:"offset"`
	Size      int          `json:"size"`
}

// Merges params from another param object in to this one. Intended to meld parsed query with HTTP query params, so not all functionality is supported, and priority is with the "current" object
func (p *PostSearchParams) Update(other *PostSearchParams) {
	p.Query = other.Query
	if p.Author == nil {
		p.Author = other.Author
	}
	if p.Since == nil {
		p.Since = other.Since
	}
	if p.Until == nil {
		p.Until = other.Until
	}
	if p.Mentions == nil {
		p.Mentions = other.Mentions
	}
	if p.Lang == nil {
		p.Lang = other.Lang
	}
	if p.Domain == "" {
		p.Domain = other.Domain
	}
	if p.URL == "" {
		p.URL = other.URL
	}
	if len(p.Tags) == 0 {
		p.Tags = other.Tags
	}
}

// Filters turns search params in to actual elasticsearch/opensearch filter DSL
func (p *PostSearchParams) Filters() []map[string]interface{} {
	var filters []map[string]interface{}

	if p.Author != nil {
		filters = append(filters, map[string]interface{}{
			"term": map[string]interface{}{"did": map[string]interface{}{
				"value":            p.Author.String(),
				"case_insensitive": true,
			}},
		})
	}

	if p.Mentions != nil {
		filters = append(filters, map[string]interface{}{
			"term": map[string]interface{}{"mention_did": map[string]interface{}{
				"value":            p.Mentions.String(),
				"case_insensitive": true,
			}},
		})
	}

	if p.Lang != nil {
		// TODO: extracting just the 2-char code would be good
		filters = append(filters, map[string]interface{}{
			"term": map[string]interface{}{"lang_code_iso2": map[string]interface{}{
				"value":            p.Lang.String(),
				"case_insensitive": true,
			}},
		})
	}

	if p.Since != nil {
		filters = append(filters, map[string]interface{}{
			"range": map[string]interface{}{
				"created_at": map[string]interface{}{
					"gte": p.Since.String(),
				},
			},
		})
	}

	if p.Until != nil {
		filters = append(filters, map[string]interface{}{
			"range": map[string]interface{}{
				"created_at": map[string]interface{}{
					"lt": p.Until.String(),
				},
			},
		})
	}

	if p.URL != "" {
		filters = append(filters, map[string]interface{}{
			"term": map[string]interface{}{"url": map[string]interface{}{
				"value":            NormalizeLossyURL(p.URL),
				"case_insensitive": true,
			}},
		})
	}

	if p.Domain != "" {
		filters = append(filters, map[string]interface{}{
			"term": map[string]interface{}{"domain": map[string]interface{}{
				"value":            p.Domain,
				"case_insensitive": true,
			}},
		})
	}

	for _, tag := range p.Tags {
		filters = append(filters, map[string]interface{}{
			"term": map[string]interface{}{
				"tag": map[string]interface{}{
					"value":            tag,
					"case_insensitive": true,
				},
			},
		})
	}

	return filters
}

// Filters turns search params in to actual elasticsearch/opensearch filter DSL
func (p *ActorSearchParams) Filters() []map[string]interface{} {
	var filters []map[string]interface{}

	if p.Follows != nil && len(p.Follows) > 0 {
		follows := make([]string, len(p.Follows))
		for i, did := range p.Follows {
			follows[i] = did.String()
		}
		filters = append(filters, map[string]interface{}{
			"terms": map[string]interface{}{
				"did": follows,
			},
		})
	}

	return filters
}

func checkParams(offset, size int) error {
	if offset+size > 10000 || size > 250 || offset > 10000 || offset < 0 || size < 0 {
		return fmt.Errorf("disallowed size/offset parameters")
	}
	return nil
}

func DoSearchPosts(ctx context.Context, dir identity.Directory, escli *es.Client, index string, params *PostSearchParams) (*EsSearchResponse, error) {
	ctx, span := tracer.Start(ctx, "DoSearchPosts")
	defer span.End()

	if err := checkParams(params.Offset, params.Size); err != nil {
		return nil, err
	}
	queryStringParams := ParsePostQuery(ctx, dir, params.Query, params.Viewer)
	params.Update(&queryStringParams)
	idx := "everything"
	if containsJapanese(params.Query) {
		idx = "everything_ja"
	}
	basic := map[string]interface{}{
		"simple_query_string": map[string]interface{}{
			"query":            params.Query,
			"fields":           []string{idx},
			"flags":            "AND|NOT|OR|PHRASE|PRECEDENCE|WHITESPACE",
			"default_operator": "and",
			"lenient":          true,
			"analyze_wildcard": false,
		},
	}
	filters := params.Filters()
	// filter out future posts (TODO: temporary hack)
	now := syntax.DatetimeNow()
	filters = append(filters, map[string]interface{}{
		"range": map[string]interface{}{
			"created_at": map[string]interface{}{
				"lte": now,
			},
		},
	})
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
		"size": params.Size,
		"from": params.Offset,
	}

	return doSearch(ctx, escli, index, query)
}

func DoSearchProfiles(ctx context.Context, dir identity.Directory, escli *es.Client, index string, params *ActorSearchParams) (*EsSearchResponse, error) {
	ctx, span := tracer.Start(ctx, "DoSearchProfiles")
	defer span.End()

	if err := checkParams(params.Offset, params.Size); err != nil {
		return nil, err
	}

	filters := params.Filters()

	fulltext := map[string]interface{}{
		"simple_query_string": map[string]interface{}{
			"query":            params.Query,
			"fields":           []string{"everything"},
			"flags":            "AND|NOT|OR|PHRASE|PRECEDENCE|WHITESPACE",
			"default_operator": "and",
			"lenient":          true,
			"analyze_wildcard": false,
		},
	}
	primary := fulltext

	// if the query string is just a single token (after parsing out filter
	// syntax), then have the primary query be an "OR" of the basic fulltext
	// query and the typeahead query
	if len(strings.Split(params.Query, " ")) == 1 {
		typeahead := map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":    params.Query,
				"type":     "bool_prefix",
				"operator": "and",
				"fields": []string{
					"typeahead",
					"typeahead._2gram",
					"typeahead._3gram",
				},
			},
		}
		primary = map[string]interface{}{
			"bool": map[string]interface{}{
				"should": []interface{}{
					fulltext,
					typeahead,
				},
			},
		}
	}

	query := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": primary,
				"should": []interface{}{
					map[string]interface{}{"term": map[string]interface{}{"has_avatar": true}},
					map[string]interface{}{"term": map[string]interface{}{"has_banner": true}},
				},
				"minimum_should_match": 0,
				"boost":                0.5,
			},
		},
		"size": params.Size,
		"from": params.Offset,
	}

	if len(filters) > 0 {
		query["query"].(map[string]interface{})["bool"].(map[string]interface{})["filter"] = filters
	}

	return doSearch(ctx, escli, index, query)
}

func DoSearchProfilesTypeahead(ctx context.Context, escli *es.Client, index string, params *ActorSearchParams) (*EsSearchResponse, error) {
	ctx, span := tracer.Start(ctx, "DoSearchProfilesTypeahead")
	defer span.End()

	if err := checkParams(0, params.Size); err != nil {
		return nil, err
	}

	filters := params.Filters()

	query := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": map[string]interface{}{
					"multi_match": map[string]interface{}{
						"query":    params.Query,
						"type":     "bool_prefix",
						"operator": "and",
						"fields": []string{
							"typeahead",
							"typeahead._2gram",
							"typeahead._3gram",
						},
					},
				},
			},
		},
		"size": params.Size,
		"from": params.Offset,
	}

	if len(filters) > 0 {
		query["query"].(map[string]interface{})["bool"].(map[string]interface{})["filter"] = filters
	}

	return doSearch(ctx, escli, index, query)
}

// helper to do a full-featured Lucene query parser (query_string) search, with all possible facets. Not safe to expose publicly.
func DoSearchGeneric(ctx context.Context, escli *es.Client, index, q string) (*EsSearchResponse, error) {
	ctx, span := tracer.Start(ctx, "DoSearchGeneric")
	defer span.End()

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
	ctx, span := tracer.Start(ctx, "doSearch")
	defer span.End()

	span.SetAttributes(attribute.String("index", index), attribute.String("query", fmt.Sprintf("%+v", query)))

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
