package search

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/labstack/echo/v4"
	otel "go.opentelemetry.io/otel"
)

type SearchPostsSkeletonResp struct {
	Cursor    string         `json:"cursor,omitempty"`
	HitsTotal *int           `json:"hits_total,omitempty"`
	Posts     []syntax.ATURI `json:"posts"`
}

type SearchActorsSkeletonResp struct {
	Cursor    string       `json:"cursor,omitempty"`
	HitsTotal *int         `json:"hits_total,omitempty"`
	Actors    []syntax.DID `json:"actors"`
}

func parseCursorLimit(e echo.Context) (int, int, error) {
	offset := 0
	if c := strings.TrimSpace(e.QueryParam("cursor")); c != "" {
		v, err := strconv.Atoi(c)
		if err != nil {
			return 0, 0, &echo.HTTPError{
				Code:    400,
				Message: fmt.Sprintf("invalid value for 'cursor': %s", err),
			}
		}
		offset = v
	}

	if offset < 0 {
		offset = 0
	}
	if offset > 10000 {
		return 0, 0, &echo.HTTPError{
			Code:    400,
			Message: fmt.Sprintf("invalid value for 'cursor' (can't paginate so deep)"),
		}
	}

	limit := 25
	if l := strings.TrimSpace(e.QueryParam("limit")); l != "" {
		v, err := strconv.Atoi(l)
		if err != nil {
			return 0, 0, &echo.HTTPError{
				Code:    400,
				Message: fmt.Sprintf("invalid value for 'count': %s", err),
			}
		}

		limit = v
	}

	if limit > 100 {
		limit = 100
	}
	if limit < 0 {
		limit = 0
	}
	return offset, limit, nil
}

func (s *Server) handleSearchPostsSkeleton(e echo.Context) error {
	ctx, span := otel.Tracer("search").Start(e.Request().Context(), "handleSearchPostsSkeleton")
	defer span.End()

	q := strings.TrimSpace(e.QueryParam("q"))
	if q == "" {
		return e.JSON(400, map[string]any{
			"error": "must pass non-empty search query",
		})
	}

	offset, limit, err := parseCursorLimit(e)
	if err != nil {
		return err
	}

	out, err := s.SearchPosts(ctx, q, offset, limit)
	if err != nil {
		return err
	}

	return e.JSON(200, out)
}

func (s *Server) handleSearchActorsSkeleton(e echo.Context) error {
	ctx, span := otel.Tracer("search").Start(e.Request().Context(), "handleSearchActorsSkeleton")
	defer span.End()

	q := strings.TrimSpace(e.QueryParam("q"))
	if q == "" {
		return e.JSON(400, map[string]any{
			"error": "must pass non-empty search query",
		})
	}

	offset, limit, err := parseCursorLimit(e)
	if err != nil {
		return err
	}

	typeahead := false
	if q := strings.TrimSpace(e.QueryParam("typeahead")); q == "true" || q == "1" || q == "y" {
		typeahead = true
	}

	out, err := s.SearchProfiles(ctx, q, typeahead, offset, limit)
	if err != nil {
		return err
	}

	return e.JSON(200, out)
}

func (s *Server) SearchPosts(ctx context.Context, q string, offset, size int) (*SearchPostsSkeletonResp, error) {
	resp, err := DoSearchPosts(ctx, s.dir, s.escli, s.postIndex, q, offset, size)
	if err != nil {
		return nil, err
	}

	posts := []syntax.ATURI{}
	for _, r := range resp.Hits.Hits {
		var doc PostDoc
		if err := json.Unmarshal(r.Source, &doc); err != nil {
			return nil, fmt.Errorf("decoding post doc from search response: %w", err)
		}

		did, err := syntax.ParseDID(doc.DID)
		if err != nil {
			return nil, fmt.Errorf("invalid DID in indexed document: %w", err)
		}

		posts = append(posts, syntax.ATURI(fmt.Sprintf("at://%s/app.bsky.feed.post/%s", did, doc.RecordRkey)))
	}

	out := SearchPostsSkeletonResp{Posts: posts}
	if len(posts) == size && (offset+size) < 10000 {
		out.Cursor = fmt.Sprintf("%d", offset+size)
	}
	fmt.Println(resp.Hits.Total)
	if resp.Hits.Total.Relation == "eq" {
		out.HitsTotal = &resp.Hits.Total.Value
	}
	return &out, nil
}

func (s *Server) SearchProfiles(ctx context.Context, q string, typeahead bool, offset, size int) (*SearchActorsSkeletonResp, error) {
	var resp *EsSearchResponse
	var err error
	if typeahead {
		resp, err = DoSearchProfilesTypeahead(ctx, s.escli, s.profileIndex, q, size)
	} else {
		resp, err = DoSearchProfiles(ctx, s.dir, s.escli, s.profileIndex, q, offset, size)
	}
	if err != nil {
		return nil, err
	}

	actors := []syntax.DID{}
	for _, r := range resp.Hits.Hits {
		var doc ProfileDoc
		if err := json.Unmarshal(r.Source, &doc); err != nil {
			return nil, fmt.Errorf("decoding profile doc from search response: %w", err)
		}

		did, err := syntax.ParseDID(doc.DID)
		if err != nil {
			return nil, fmt.Errorf("invalid DID in indexed document: %w", err)
		}

		actors = append(actors, did)
	}

	out := SearchActorsSkeletonResp{Actors: actors}
	if len(actors) == size && (offset+size) < 10000 {
		out.Cursor = fmt.Sprintf("%d", offset+size)
	}
	if resp.Hits.Total.Relation == "eq" {
		out.HitsTotal = &resp.Hits.Total.Value
	}
	return &out, nil
}
