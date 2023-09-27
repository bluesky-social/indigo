package handlers

import (
	"fmt"
	"log/slog"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/graphd"
	"github.com/bluesky-social/indigo/util/version"
	"github.com/labstack/echo/v4"
)

type Handlers struct {
	graph *graphd.Graph
}

func NewHandlers(graph *graphd.Graph) *Handlers {
	return &Handlers{
		graph: graph,
	}
}

type HealthStatus struct {
	Status      string  `json:"status"`
	Version     string  `json:"version"`
	Message     string  `json:"msg,omitempty"`
	UserCount   *uint64 `json:"userCount,omitempty"`
	FollowCount *uint64 `json:"followCount,omitempty"`
}

func (h *Handlers) Health(c echo.Context) error {
	s := HealthStatus{
		Status:  "ok",
		Version: version.Version,
	}
	if c.QueryParam("stats") == "true" {
		userCount := h.graph.GetUsercount()
		s.UserCount = &userCount

		followCount := h.graph.GetFollowcount()
		s.FollowCount = &followCount
	}

	return c.JSON(200, s)
}

func (h *Handlers) GetFollowers(c echo.Context) error {
	did := c.QueryParam("did")

	uid, ok := h.graph.GetUID(did)
	if !ok {
		return c.JSON(404, "uid not found")
	}

	followers, err := h.graph.GetFollowers(uid)
	if err != nil {
		slog.Error("failed to get followers", "err", err)
		return c.JSON(500, fmt.Errorf("failed to get followers"))
	}

	dids, err := h.graph.GetDIDs(followers)
	if err != nil {
		slog.Error("failed to get dids", "err", err)
		return c.JSON(500, fmt.Errorf("failed to get dids"))
	}

	return c.JSON(200, dids)
}

func (h *Handlers) GetFollowing(c echo.Context) error {
	did := c.QueryParam("did")

	uid, ok := h.graph.GetUID(did)
	if !ok {
		return c.JSON(404, "uid not found")
	}

	following, err := h.graph.GetFollowing(uid)
	if err != nil {
		slog.Error("failed to get following", "err", err)
		return c.JSON(500, fmt.Errorf("failed to get following"))
	}

	dids, err := h.graph.GetDIDs(following)
	if err != nil {
		slog.Error("failed to get dids", "err", err)
		return c.JSON(500, fmt.Errorf("failed to get dids"))
	}

	return c.JSON(200, dids)
}

func (h *Handlers) GetFollowersNotFollowing(c echo.Context) error {
	did := c.QueryParam("did")

	uid, ok := h.graph.GetUID(did)
	if !ok {
		return c.JSON(404, "uid not found")
	}

	followersNotFollowing, err := h.graph.GetFollowersNotFollowing(uid)
	if err != nil {
		slog.Error("failed to get followers not following", "err", err)
		return c.JSON(500, fmt.Errorf("failed to get followers not following"))
	}

	dids, err := h.graph.GetDIDs(followersNotFollowing)
	if err != nil {
		slog.Error("failed to get dids", "err", err)
		return c.JSON(500, fmt.Errorf("failed to get dids"))
	}

	return c.JSON(200, dids)
}

func (h *Handlers) GetDoesFollow(c echo.Context) error {
	actorDid := c.QueryParam("actorDid")
	targetDid := c.QueryParam("targetDid")

	actorUID, ok := h.graph.GetUID(actorDid)
	if !ok {
		return c.JSON(404, "actor uid not found")
	}

	targetUID, ok := h.graph.GetUID(targetDid)
	if !ok {
		return c.JSON(404, "target uid not found")
	}

	doesFollow, err := h.graph.DoesFollow(actorUID, targetUID)
	if err != nil {
		slog.Error("failed to check if follows", "err", err)
		return c.JSON(500, "failed to check if follows")
	}

	return c.JSON(200, doesFollow)
}

func (h *Handlers) GetAreMoots(c echo.Context) error {
	didA := c.QueryParam("didA")
	didB := c.QueryParam("didB")

	uidA, ok := h.graph.GetUID(didA)
	if !ok {
		return c.JSON(404, "actor uid not found")
	}

	uidB, ok := h.graph.GetUID(didB)
	if !ok {
		return c.JSON(404, "target uid not found")
	}

	aFollowsB := false
	bFollowsA := false

	aFollowsB, err := h.graph.DoesFollow(uidA, uidB)
	if err != nil {
		slog.Error("failed to check follows", "err", err)
		return c.JSON(500, "failed to check follows")
	}

	bFollowsA, err = h.graph.DoesFollow(uidB, uidA)
	if err != nil {
		slog.Error("failed to check follows", "err", err)
		return c.JSON(500, "failed to check follows")
	}

	return c.JSON(200, aFollowsB && bFollowsA)
}

func (h *Handlers) GetIntersectFollowers(c echo.Context) error {
	if !c.QueryParams().Has("did") {
		return c.JSON(400, "did query param is required")
	}
	qDIDs := c.QueryParams()["did"]
	uids := make([]uint64, 0)
	for _, qDID := range qDIDs {
		uid, ok := h.graph.GetUID(qDID)
		if !ok {
			return c.JSON(404, fmt.Sprintf("uid not found for did %s", qDID))
		}
		uids = append(uids, uid)
	}

	intersect, err := h.graph.IntersectFollowers(uids)
	if err != nil {
		slog.Error("failed to intersect followers", "err", err)
		return c.JSON(500, "failed to intersect followers")
	}

	dids, err := h.graph.GetDIDs(intersect)
	if err != nil {
		slog.Error("failed to get dids", "err", err)
		return c.JSON(500, fmt.Errorf("failed to get dids"))
	}

	return c.JSON(200, dids)
}

func (h *Handlers) GetIntersectFollowing(c echo.Context) error {
	if !c.QueryParams().Has("did") {
		return c.JSON(400, "did query param is required")
	}
	qDIDs := c.QueryParams()["did"]
	uids := make([]uint64, 0)
	for _, qDID := range qDIDs {
		uid, ok := h.graph.GetUID(qDID)
		if !ok {
			return c.JSON(404, fmt.Sprintf("uid not found for did %s", qDID))
		}
		uids = append(uids, uid)
	}

	intersect, err := h.graph.IntersectFollowing(uids)
	if err != nil {
		slog.Error("failed to intersect following", "err", err)
		return c.JSON(500, "failed to intersect following")
	}

	dids, err := h.graph.GetDIDs(intersect)
	if err != nil {
		slog.Error("failed to get dids", "err", err)
		return c.JSON(500, fmt.Errorf("failed to get dids"))
	}

	return c.JSON(200, dids)
}

type Follow struct {
	ActorDid  string `json:"actorDid"`
	TargetDid string `json:"targetDid"`
}

func (h *Handlers) PostFollow(c echo.Context) error {
	var body Follow
	if err := c.Bind(&body); err != nil {
		return c.JSON(400, fmt.Sprintf("invalid body: %s", err))
	}

	actorDid, err := syntax.ParseDID(body.ActorDid)
	if err != nil {
		return c.JSON(400, fmt.Sprintf("invalid actor did: %s", err))
	}

	targetDid, err := syntax.ParseDID(body.TargetDid)
	if err != nil {
		return c.JSON(400, fmt.Sprintf("invalid target did: %s", err))
	}

	actorUID := h.graph.AcquireDID(actorDid.String())
	targetUID := h.graph.AcquireDID(targetDid.String())
	h.graph.AddFollow(actorUID, targetUID)

	return c.JSON(200, "ok")
}

type PostFollowsBody struct {
	Follows []Follow `json:"follows"`
}

func (h *Handlers) PostFollows(c echo.Context) error {
	var body PostFollowsBody
	if err := c.Bind(&body); err != nil {
		return c.JSON(400, fmt.Sprintf("invalid body: %s", err))
	}

	// Validate all the DIDs before adding any of them
	for i, follow := range body.Follows {
		_, err := syntax.ParseDID(follow.ActorDid)
		if err != nil {
			return c.JSON(400, fmt.Sprintf("invalid actor did[%d]: %s", i, err))
		}

		_, err = syntax.ParseDID(follow.TargetDid)
		if err != nil {
			return c.JSON(400, fmt.Sprintf("invalid target did[%d]: %s", i, err))
		}
	}

	for _, follow := range body.Follows {
		actorUID := h.graph.AcquireDID(follow.ActorDid)
		targetUID := h.graph.AcquireDID(follow.TargetDid)
		h.graph.AddFollow(actorUID, targetUID)
	}

	return c.JSON(200, "ok")
}

type Unfollow struct {
	ActorDid  string `json:"actorDid"`
	TargetDid string `json:"targetDid"`
}

func (h *Handlers) PostUnfollow(c echo.Context) error {
	var body Unfollow
	if err := c.Bind(&body); err != nil {
		return c.JSON(400, fmt.Sprintf("invalid body: %s", err))
	}

	actorDid, err := syntax.ParseDID(body.ActorDid)
	if err != nil {
		return c.JSON(400, fmt.Sprintf("invalid actor did: %s", err))
	}

	targetDid, err := syntax.ParseDID(body.TargetDid)
	if err != nil {
		return c.JSON(400, fmt.Sprintf("invalid target did: %s", err))
	}

	actorUID := h.graph.AcquireDID(actorDid.String())
	targetUID := h.graph.AcquireDID(targetDid.String())
	h.graph.RemoveFollow(actorUID, targetUID)

	return c.JSON(200, "ok")
}

type PostUnfollowsBody struct {
	Unfollows []Unfollow `json:"unfollows"`
}

func (h *Handlers) PostUnfollows(c echo.Context) error {
	var body PostUnfollowsBody
	if err := c.Bind(&body); err != nil {
		return c.JSON(400, fmt.Sprintf("invalid body: %s", err))
	}

	// Validate all the DIDs before adding any of them
	for i, unfollow := range body.Unfollows {
		_, err := syntax.ParseDID(unfollow.ActorDid)
		if err != nil {
			return c.JSON(400, fmt.Sprintf("invalid actor did[%d]: %s", i, err))
		}

		_, err = syntax.ParseDID(unfollow.TargetDid)
		if err != nil {
			return c.JSON(400, fmt.Sprintf("invalid target did[%d]: %s", i, err))
		}
	}

	for _, unfollow := range body.Unfollows {
		actorUID := h.graph.AcquireDID(unfollow.ActorDid)
		targetUID := h.graph.AcquireDID(unfollow.TargetDid)
		h.graph.RemoveFollow(actorUID, targetUID)
	}

	return c.JSON(200, "ok")
}
