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

type ActorSearchResp struct {
	ActorProfile ProfileDoc
	DID          string `json:"did"`
}

func (s *Server) handleSearchRequestPosts(e echo.Context) error {
	ctx, span := otel.Tracer("search").Start(e.Request().Context(), "handleSearchRequestPosts")
	defer span.End()

	q := strings.TrimSpace(e.QueryParam("q"))
	if q == "" {
		return e.JSON(400, map[string]any{
			"error": "must pass non-empty search query",
		})
	}

	offset := 0
	if q := strings.TrimSpace(e.QueryParam("offset")); q != "" {
		v, err := strconv.Atoi(q)
		if err != nil {
			return &echo.HTTPError{
				Code:    400,
				Message: fmt.Sprintf("invalid value for 'offset': %s", err),
			}
		}

		offset = v
	}

	count := 30
	if q := strings.TrimSpace(e.QueryParam("count")); q != "" {
		v, err := strconv.Atoi(q)
		if err != nil {
			return &echo.HTTPError{
				Code:    400,
				Message: fmt.Sprintf("invalid value for 'count': %s", err),
			}
		}

		count = v
	}

	out, err := s.SearchPosts(ctx, q, offset, count)
	if err != nil {
		return err
	}

	return e.JSON(200, out)
}

func (s *Server) handleSearchRequestProfiles(e echo.Context) error {
	ctx, span := otel.Tracer("search").Start(e.Request().Context(), "handleSearchRequestProfiles")
	defer span.End()

	q := strings.TrimSpace(e.QueryParam("q"))
	if q == "" {
		return e.JSON(400, map[string]any{
			"error": "must pass non-empty search query",
		})
	}

	offset := 0
	if q := strings.TrimSpace(e.QueryParam("offset")); q != "" {
		v, err := strconv.Atoi(q)
		if err != nil {
			return &echo.HTTPError{
				Code:    400,
				Message: fmt.Sprintf("invalid value for 'offset': %s", err),
			}
		}

		offset = v
	}

	count := 30
	if q := strings.TrimSpace(e.QueryParam("count")); q != "" {
		v, err := strconv.Atoi(q)
		if err != nil {
			return &echo.HTTPError{
				Code:    400,
				Message: fmt.Sprintf("invalid value for 'count': %s", err),
			}
		}

		count = v
	}

	typeahead := false
	if q := strings.TrimSpace(e.QueryParam("typeahead")); q == "true" || q == "1" || q == "y" {
		typeahead = true
	}

	out, err := s.SearchProfiles(ctx, q, typeahead, offset, count)
	if err != nil {
		return err
	}

	return e.JSON(200, out)
}

func (s *Server) SearchPosts(ctx context.Context, q string, offset, size int) ([]PostSearchResult, error) {
	resp, err := DoSearchPosts(ctx, s.dir, s.escli, s.postIndex, q, offset, size)
	if err != nil {
		return nil, err
	}

	out := []PostSearchResult{}
	for _, r := range resp.Hits.Hits {
		if err != nil {
			return nil, fmt.Errorf("decoding document id: %w", err)
		}

		var doc PostDoc
		if err := json.Unmarshal(r.Source, &doc); err != nil {
			return nil, err
		}

		did, err := syntax.ParseDID(doc.DID)
		if err != nil {
			s.logger.Warn("invalid DID in indexed document", "did", doc.DID, "err", err)
			continue
		}
		handle := ""
		ident, err := s.dir.LookupDID(ctx, did)
		if err != nil {
			s.logger.Warn("could not resolve identity", "did", doc.DID)
			continue
		} else {
			handle = ident.Handle.String()
		}

		out = append(out, PostSearchResult{
			Tid: doc.RecordRkey,
			Cid: doc.RecordCID,
			User: UserResult{
				Did:    doc.DID,
				Handle: handle,
			},
			Post: &doc,
		})
	}

	return out, nil
}

func (s *Server) SearchProfiles(ctx context.Context, q string, typeahead bool, offset, size int) ([]*ActorSearchResp, error) {
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

	out := []*ActorSearchResp{}
	for _, r := range resp.Hits.Hits {
		var doc ProfileDoc
		if err := json.Unmarshal(r.Source, &doc); err != nil {
			return nil, err
		}

		out = append(out, &ActorSearchResp{
			ActorProfile: doc,
			DID:          doc.DID,
		})
	}

	return out, nil
}
