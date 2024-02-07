package engine

import (
	"context"
	"encoding/json"
	"fmt"

	appbsky "github.com/bluesky-social/indigo/api/bsky"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Represents app.bsky social graph relationship between a primary account, and an "other" account
type AccountRelationship struct {
	// the DID of the "other" account
	DID syntax.DID
	// if the primary account is followed by this "other" DID
	FollowedBy bool
	// if the primary account follows this "other" DID
	Following bool
}

// Helper to fetch social graph relationship metadata
func (eng *Engine) GetAccountRelationship(ctx context.Context, primary, other syntax.DID) (*AccountRelationship, error) {

	logger := eng.Logger.With("did", primary.String(), "otherDID", other.String())

	cacheKey := fmt.Sprintf("%s/%s", primary.String(), other.String())
	existing, err := eng.Cache.Get(ctx, "graph-rel", cacheKey)
	if err != nil {
		return nil, fmt.Errorf("failed checking account relationship cache: %w", err)
	}
	if existing != "" {
		var ar AccountRelationship
		err := json.Unmarshal([]byte(existing), &ar)
		if err != nil || ar.DID != primary {
			return nil, fmt.Errorf("parsing AccountRelationship from cache: %v", err)
		}
		return &ar, nil
	}

	if eng.BskyClient == nil {
		logger.Warn("skipping account relationship fetch")
		ar := AccountRelationship{DID: other}
		return &ar, nil
	}

	// fetch account relationship from AppView
	accountRelationshipFetches.Inc()
	resp, err := appbsky.GraphGetRelationships(ctx, eng.BskyClient, primary.String(), []string{other.String()})
	if err != nil || len(resp.Relationships) != 1 {
		logger.Warn("account relationship lookup failed", "err", err)
		ar := AccountRelationship{DID: other}
		return &ar, nil
	}

	if len(resp.Relationships) != 1 || resp.Relationships[0].GraphDefs_Relationship == nil {
		logger.Warn("account relationship actor not found")
	}

	rel := resp.Relationships[0].GraphDefs_Relationship
	ar := AccountRelationship{
		DID:        primary,
		FollowedBy: rel.FollowedBy != nil,
		Following:  rel.Following != nil,
	}
	return &ar, nil
}
