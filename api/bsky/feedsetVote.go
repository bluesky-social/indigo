package schemagen

import (
	"context"
	"encoding/json"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.setVote

type FeedSetVote_Input struct {
	Subject   *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
	Direction string                         `json:"direction" cborgen:"direction"`
}

func (t *FeedSetVote_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["direction"] = t.Direction
	out["subject"] = t.Subject
	return json.Marshal(out)
}

type FeedSetVote_Output struct {
	Upvote   string `json:"upvote" cborgen:"upvote"`
	Downvote string `json:"downvote" cborgen:"downvote"`
}

func (t *FeedSetVote_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["downvote"] = t.Downvote
	out["upvote"] = t.Upvote
	return json.Marshal(out)
}

func FeedSetVote(ctx context.Context, c *xrpc.Client, input FeedSetVote_Input) (*FeedSetVote_Output, error) {
	var out FeedSetVote_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.feed.setVote", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
