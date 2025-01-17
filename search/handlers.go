package search

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/labstack/echo/v4"
	otel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var tracer = otel.Tracer("search")

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
			Message: "invalid value for 'cursor' (can't paginate so deep)",
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
	ctx, span := tracer.Start(e.Request().Context(), "handleSearchPostsSkeleton")
	defer span.End()

	span.SetAttributes(attribute.String("query", e.QueryParam("q")))

	q := strings.TrimSpace(e.QueryParam("q"))
	if q == "" {
		return e.JSON(400, map[string]any{
			"error": "must pass non-empty search query",
		})
	}

	params := PostSearchParams{
		Query: q,
		// TODO: parse/validate the sort options here?
		Sort:   e.QueryParam("sort"),
		Domain: e.QueryParam("domain"),
		URL:    e.QueryParam("url"),
	}

	viewerStr := e.QueryParam("viewer")
	if viewerStr != "" {
		d, err := syntax.ParseDID(viewerStr)
		if err != nil {
			return e.JSON(400, map[string]any{
				"error":   "BadRequest",
				"message": fmt.Sprintf("invalid DID for 'viewer': %s", err),
			})
		}
		params.Viewer = &d
	}
	authorStr := e.QueryParam("author")
	if authorStr != "" {
		atid, err := syntax.ParseAtIdentifier(authorStr)
		if err != nil {
			return &echo.HTTPError{
				Code:    400,
				Message: fmt.Sprintf("invalid DID for 'author': %s", err),
			}
		}
		if atid.IsHandle() {
			ident, err := s.dir.Lookup(e.Request().Context(), *atid)
			if err != nil {
				return e.JSON(400, map[string]any{
					"error":   "BadRequest",
					"message": fmt.Sprintf("invalid Handle for 'author': %s", err),
				})
			}
			params.Author = &ident.DID
		} else {
			d, err := atid.AsDID()
			if err != nil {
				return err
			}
			params.Author = &d
		}
	}

	mentionsStr := e.QueryParam("mentions")
	if mentionsStr != "" {
		atid, err := syntax.ParseAtIdentifier(mentionsStr)
		if err != nil {
			return &echo.HTTPError{
				Code:    400,
				Message: fmt.Sprintf("invalid DID for 'mentions': %s", err),
			}
		}
		if atid.IsHandle() {
			ident, err := s.dir.Lookup(e.Request().Context(), *atid)
			if err != nil {
				return e.JSON(400, map[string]any{
					"error":   "BadRequest",
					"message": fmt.Sprintf("invalid Handle for 'mentions': %s", err),
				})
			}
			params.Mentions = &ident.DID
		} else {
			d, err := atid.AsDID()
			if err != nil {
				return err
			}
			params.Mentions = &d
		}
	}

	sinceStr := e.QueryParam("since")
	if sinceStr != "" {
		dt, err := syntax.ParseDatetime(sinceStr)
		if err != nil {
			return e.JSON(400, map[string]any{
				"error":   "BadRequest",
				"message": fmt.Sprintf("invalid Datetime for 'since': %s", err),
			})
		}
		params.Since = &dt
	}

	untilStr := e.QueryParam("until")
	if untilStr != "" {
		dt, err := syntax.ParseDatetime(untilStr)
		if err != nil {
			return e.JSON(400, map[string]any{
				"error":   "BadRequest",
				"message": fmt.Sprintf("invalid Datetime for 'until': %s", err),
			})
		}
		params.Until = &dt
	}

	langStr := e.QueryParam("lang")
	if langStr != "" {
		l, err := syntax.ParseLanguage(langStr)
		if err != nil {
			return e.JSON(400, map[string]any{
				"error":   "BadRequest",
				"message": fmt.Sprintf("invalid Language for 'lang': %s", err),
			})
		}
		params.Lang = &l
	}
	// TODO: could be multiple tag params; guess we should "bind"?
	tags := e.Request().URL.Query()["tags"]
	if len(tags) > 0 {
		params.Tags = tags
	}

	offset, limit, err := parseCursorLimit(e)
	if err != nil {
		span.SetAttributes(attribute.String("error", fmt.Sprintf("invalid cursor/limit: %s", err)))
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	params.Offset = offset
	params.Size = limit
	span.SetAttributes(attribute.Int("offset", offset), attribute.Int("limit", limit))

	out, err := s.SearchPosts(ctx, &params)
	if err != nil {
		span.SetAttributes(attribute.String("error", fmt.Sprintf("failed to SearchPosts: %s", err)))
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(attribute.Int("posts.length", len(out.Posts)))

	return e.JSON(200, out)
}

func (s *Server) handleSearchActorsSkeleton(e echo.Context) error {
	ctx, span := tracer.Start(e.Request().Context(), "handleSearchActorsSkeleton")
	defer span.End()

	span.SetAttributes(attribute.String("query", e.QueryParam("q")))

	q := strings.TrimSpace(e.QueryParam("q"))
	if q == "" {
		return e.JSON(400, map[string]any{
			"error":   "BadRequest",
			"message": "must pass non-empty search query",
		})
	}

	offset, limit, err := parseCursorLimit(e)
	if err != nil {
		span.SetAttributes(attribute.String("error", fmt.Sprintf("invalid cursor/limit: %s", err)))
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	typeahead := false
	if q := strings.TrimSpace(e.QueryParam("typeahead")); q == "true" || q == "1" || q == "y" {
		typeahead = true
	}

	params := ActorSearchParams{
		Query:     q,
		Typeahead: typeahead,
		Offset:    offset,
		Size:      limit,
	}

	viewerStr := e.QueryParam("viewer")
	if viewerStr != "" {
		d, err := syntax.ParseDID(viewerStr)
		if err != nil {
			return e.JSON(400, map[string]any{
				"error":   "BadRequest",
				"message": fmt.Sprintf("invalid DID for 'viewer': %s", err),
			})
		}
		params.Viewer = &d
	}

	span.SetAttributes(
		attribute.Int("offset", offset),
		attribute.Int("limit", limit),
		attribute.Bool("typeahead", typeahead),
	)

	out, err := s.SearchProfiles(ctx, &params)
	if err != nil {
		span.SetAttributes(attribute.String("error", fmt.Sprintf("failed to SearchProfiles: %s", err)))
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.SetAttributes(attribute.Int("actors.length", len(out.Actors)))

	return e.JSON(200, out)
}

func (s *Server) SearchPosts(ctx context.Context, params *PostSearchParams) (*appbsky.UnspeccedSearchPostsSkeleton_Output, error) {
	ctx, span := tracer.Start(ctx, "SearchPosts")
	defer span.End()

	resp, err := DoSearchPosts(ctx, s.dir, s.escli, s.postIndex, params)
	if err != nil {
		return nil, err
	}

	posts := []*appbsky.UnspeccedDefs_SkeletonSearchPost{}
	for _, r := range resp.Hits.Hits {
		var doc PostDoc
		if err := json.Unmarshal(r.Source, &doc); err != nil {
			return nil, fmt.Errorf("decoding post doc from search response: %w", err)
		}

		did, err := syntax.ParseDID(doc.DID)
		if err != nil {
			return nil, fmt.Errorf("invalid DID in indexed document: %w", err)
		}

		posts = append(posts, &appbsky.UnspeccedDefs_SkeletonSearchPost{
			Uri: fmt.Sprintf("at://%s/app.bsky.feed.post/%s", did, doc.RecordRkey),
		})
	}

	out := appbsky.UnspeccedSearchPostsSkeleton_Output{Posts: posts}
	if len(posts) == params.Size && (params.Offset+params.Size) < 10000 {
		s := fmt.Sprintf("%d", params.Offset+params.Size)
		out.Cursor = &s
	}
	if resp.Hits.Total.Relation == "eq" {
		i := int64(resp.Hits.Total.Value)
		out.HitsTotal = &i
	}
	return &out, nil
}

func (s *Server) SearchProfiles(ctx context.Context, params *ActorSearchParams) (*appbsky.UnspeccedSearchActorsSkeleton_Output, error) {
	ctx, span := tracer.Start(ctx, "SearchProfiles")
	defer span.End()
	span.SetAttributes(
		attribute.String("query", params.Query),
		attribute.Bool("typeahead", params.Typeahead),
		attribute.Int("offset", params.Offset),
		attribute.Int("size", params.Size),
	)

	var globalResp *EsSearchResponse
	var personalizedResp *EsSearchResponse
	var globalErr error
	var personalizedErr error

	wg := sync.WaitGroup{}

	wg.Add(1)
	// Conduct the global search
	go func(myQ ActorSearchParams) {
		defer wg.Done()
		// Clear out the following list to conduct the global search
		myQ.Follows = nil

		if myQ.Typeahead {
			globalResp, globalErr = DoSearchProfilesTypeahead(ctx, s.escli, s.profileIndex, &myQ)
		} else {
			globalResp, globalErr = DoSearchProfiles(ctx, s.dir, s.escli, s.profileIndex, &myQ)
		}
	}(*params)

	// If we have a following list, conduct a second search to filter the results
	if len(params.Follows) > 0 {
		wg.Add(1)
		go func(myQ ActorSearchParams) {
			defer wg.Done()
			if myQ.Typeahead {
				personalizedResp, personalizedErr = DoSearchProfilesTypeahead(ctx, s.escli, s.profileIndex, &myQ)
			} else {
				personalizedResp, personalizedErr = DoSearchProfiles(ctx, s.dir, s.escli, s.profileIndex, &myQ)
			}
		}(*params)
	}

	wg.Wait()

	if globalErr != nil {
		return nil, globalErr
	}

	if len(params.Follows) > 0 {
		if personalizedErr != nil {
			return nil, personalizedErr
		}

		followingBoost := 0.1

		// Insert the personalized results into the global results, deduping as we go and maintaining score-order
		followingSeen := map[string]struct{}{}
		for _, r := range personalizedResp.Hits.Hits {
			var doc ProfileDoc
			if err := json.Unmarshal(r.Source, &doc); err != nil {
				return nil, fmt.Errorf("decoding profile doc from search response: %w", err)
			}

			did, err := syntax.ParseDID(doc.DID)
			if err != nil {
				return nil, fmt.Errorf("invalid DID in indexed document: %w", err)
			}

			if _, ok := followingSeen[did.String()]; ok {
				continue
			}

			followingSeen[did.String()] = struct{}{}

			// Insert the profile into the global results
			globalResp.Hits.Hits = append(globalResp.Hits.Hits, r)
		}

		// Walk the combined results and boost the scores of the personalized results and dedupe
		seen := map[string]struct{}{}
		deduped := []EsSearchHit{}
		for _, r := range globalResp.Hits.Hits {
			var doc ProfileDoc
			if err := json.Unmarshal(r.Source, &doc); err != nil {
				return nil, fmt.Errorf("decoding profile doc from search response: %w", err)
			}

			did, err := syntax.ParseDID(doc.DID)
			if err != nil {
				return nil, fmt.Errorf("invalid DID in indexed document: %w", err)
			}

			// Boost the score of the personalized results
			if _, ok := followingSeen[did.String()]; ok {
				r.Score += followingBoost
			}

			// Dedupe the results
			if _, ok := seen[did.String()]; ok {
				continue
			}

			seen[did.String()] = struct{}{}
			deduped = append(deduped, r)
		}

		// Sort the results by score
		slices.SortFunc(deduped, func(a, b EsSearchHit) int {
			if a.Score < b.Score {
				return 1
			}
			if a.Score > b.Score {
				return -1
			}
			return 0
		})

		// Trim the results to the requested size
		if len(deduped) > params.Size {
			deduped = deduped[:params.Size]
		}

		globalResp.Hits.Hits = deduped
	}

	actors := []*appbsky.UnspeccedDefs_SkeletonSearchActor{}
	for _, r := range globalResp.Hits.Hits {
		var doc ProfileDoc
		if err := json.Unmarshal(r.Source, &doc); err != nil {
			return nil, fmt.Errorf("decoding profile doc from search response: %w", err)
		}

		did, err := syntax.ParseDID(doc.DID)
		if err != nil {
			return nil, fmt.Errorf("invalid DID in indexed document: %w", err)
		}

		actors = append(actors, &appbsky.UnspeccedDefs_SkeletonSearchActor{
			Did: did.String(),
		})
	}

	out := appbsky.UnspeccedSearchActorsSkeleton_Output{Actors: actors}
	if len(actors) == params.Size && (params.Offset+params.Size) < 10000 {
		s := fmt.Sprintf("%d", params.Offset+params.Size)
		out.Cursor = &s
	}
	if globalResp.Hits.Total.Relation == "eq" {
		i := int64(globalResp.Hits.Total.Value)
		out.HitsTotal = &i
	}
	return &out, nil
}
