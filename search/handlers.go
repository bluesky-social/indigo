package search

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	api "github.com/bluesky-social/indigo/api"
	bsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/labstack/echo/v4"
	otel "go.opentelemetry.io/otel"
)

type ActorSearchResp struct {
	bsky.ActorProfile
	DID string `json:"did"`
}

func (s *Server) handleFromDid(ctx context.Context, did string) (string, error) {
	handle, _, err := api.ResolveDidToHandle(ctx, s.xrpcc, s.plc, did)
	if err != nil {
		return "", err
	}

	return handle, nil
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

	out, err := s.SearchProfiles(ctx, q)
	if err != nil {
		return err
	}

	return e.JSON(200, out)
}
