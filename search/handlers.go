package search

import (
	"context"
	"fmt"
	"net/http"
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

	// Set a limit on the number of posts that can be returned.
	maxCount := 100

	query := strings.TrimSpace(e.QueryParam("q"))
	if query == "" {
		return e.JSON(http.StatusBadRequest, map[string]any{
			"error": "must pass non-empty search query",
		})
	}

	offset := 0
	if offsetFromQuery := strings.TrimSpace(e.QueryParam("offset")); offsetFromQuery != "" {
		v, err := strconv.Atoi(offsetFromQuery)
		if err != nil {
			return &echo.HTTPError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("invalid value for 'offset': %s", err),
			}
		}

		offset = v
	}

	count := 30
	if countFromQuery := strings.TrimSpace(e.QueryParam("count")); countFromQuery != "" {
		v, err := strconv.Atoi(countFromQuery)
		if err != nil {
			return &echo.HTTPError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("invalid value for 'count': %s", err),
			}
		}

		if v > maxCount {
			v = maxCount
		}

		count = v
	}

	out, err := s.SearchPosts(ctx, query, offset, count)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, out)
}

func (s *Server) handleSearchRequestProfiles(e echo.Context) error {
	ctx, span := otel.Tracer("search").Start(e.Request().Context(), "handleSearchRequestProfiles")
	defer span.End()

	q := strings.TrimSpace(e.QueryParam("q"))
	if q == "" {
		return e.JSON(http.StatusBadRequest, map[string]any{
			"error": "must pass non-empty search query",
		})
	}

	out, err := s.SearchProfiles(ctx, q)
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, out)
}
