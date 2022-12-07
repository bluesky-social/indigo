package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.getProfile

type ActorGetProfile_Output struct {
	Did            string                   `json:"did"`
	Declaration    *SystemDeclRef           `json:"declaration"`
	Handle         string                   `json:"handle"`
	DisplayName    string                   `json:"displayName"`
	FollowersCount int64                    `json:"followersCount"`
	MembersCount   int64                    `json:"membersCount"`
	PostsCount     int64                    `json:"postsCount"`
	Creator        string                   `json:"creator"`
	Description    string                   `json:"description"`
	FollowsCount   int64                    `json:"followsCount"`
	MyState        *ActorGetProfile_MyState `json:"myState"`
}

func (t *ActorGetProfile_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["creator"] = t.Creator
	out["declaration"] = t.Declaration
	out["description"] = t.Description
	out["did"] = t.Did
	out["displayName"] = t.DisplayName
	out["followersCount"] = t.FollowersCount
	out["followsCount"] = t.FollowsCount
	out["handle"] = t.Handle
	out["membersCount"] = t.MembersCount
	out["myState"] = t.MyState
	out["postsCount"] = t.PostsCount
	return json.Marshal(out)
}

type ActorGetProfile_MyState struct {
	Follow string `json:"follow"`
	Member string `json:"member"`
}

func (t *ActorGetProfile_MyState) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["follow"] = t.Follow
	out["member"] = t.Member
	return json.Marshal(out)
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
