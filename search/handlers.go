package search

import (
	"context"
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

	out, err := s.SearchPosts(ctx, q)
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
