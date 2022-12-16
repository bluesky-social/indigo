package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.getProfile

func init() {
}

type ActorGetProfile_Output struct {
	Handle         string                   `json:"handle" cborgen:"handle"`
	Creator        string                   `json:"creator" cborgen:"creator"`
	DisplayName    *string                  `json:"displayName" cborgen:"displayName"`
	Description    *string                  `json:"description" cborgen:"description"`
	FollowersCount int64                    `json:"followersCount" cborgen:"followersCount"`
	MembersCount   int64                    `json:"membersCount" cborgen:"membersCount"`
	Did            string                   `json:"did" cborgen:"did"`
	Declaration    *SystemDeclRef           `json:"declaration" cborgen:"declaration"`
	MyState        *ActorGetProfile_MyState `json:"myState" cborgen:"myState"`
	FollowsCount   int64                    `json:"followsCount" cborgen:"followsCount"`
	PostsCount     int64                    `json:"postsCount" cborgen:"postsCount"`
}

type ActorGetProfile_MyState struct {
	Follow *string `json:"follow" cborgen:"follow"`
	Member *string `json:"member" cborgen:"member"`
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
