package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.getProfile

type ActorGetProfile_Output struct {
	Declaration    *SystemDeclRef           `json:"declaration" cborgen:"declaration"`
	Creator        string                   `json:"creator" cborgen:"creator"`
	DisplayName    string                   `json:"displayName" cborgen:"displayName"`
	FollowersCount int64                    `json:"followersCount" cborgen:"followersCount"`
	FollowsCount   int64                    `json:"followsCount" cborgen:"followsCount"`
	MembersCount   int64                    `json:"membersCount" cborgen:"membersCount"`
	PostsCount     int64                    `json:"postsCount" cborgen:"postsCount"`
	MyState        *ActorGetProfile_MyState `json:"myState" cborgen:"myState"`
	Did            string                   `json:"did" cborgen:"did"`
	Handle         string                   `json:"handle" cborgen:"handle"`
	Description    string                   `json:"description" cborgen:"description"`
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
	Follow string `json:"follow" cborgen:"follow"`
	Member string `json:"member" cborgen:"member"`
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
