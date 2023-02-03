package bsky

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.actor.getProfile

func init() {
}

type ActorGetProfile_MyState struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Follow        *string `json:"follow,omitempty" cborgen:"follow"`
	Muted         *bool   `json:"muted,omitempty" cborgen:"muted"`
}

type ActorGetProfile_Output struct {
	LexiconTypeID  string                   `json:"$type,omitempty"`
	Avatar         *string                  `json:"avatar,omitempty" cborgen:"avatar"`
	Banner         *string                  `json:"banner,omitempty" cborgen:"banner"`
	Creator        string                   `json:"creator" cborgen:"creator"`
	Declaration    *SystemDeclRef           `json:"declaration" cborgen:"declaration"`
	Description    *string                  `json:"description,omitempty" cborgen:"description"`
	Did            string                   `json:"did" cborgen:"did"`
	DisplayName    *string                  `json:"displayName,omitempty" cborgen:"displayName"`
	FollowersCount int64                    `json:"followersCount" cborgen:"followersCount"`
	FollowsCount   int64                    `json:"followsCount" cborgen:"followsCount"`
	Handle         string                   `json:"handle" cborgen:"handle"`
	MyState        *ActorGetProfile_MyState `json:"myState,omitempty" cborgen:"myState"`
	PostsCount     int64                    `json:"postsCount" cborgen:"postsCount"`
}

func ActorGetProfile(ctx context.Context, c *xrpc.Client, actor string) (*ActorGetProfile_Output, error) {
	var out ActorGetProfile_Output

	params := map[string]interface{}{
		"actor": actor,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.actor.getProfile", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
