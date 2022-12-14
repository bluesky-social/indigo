package schemagen

import (
	"context"

	comatprototypes "github.com/whyrusleeping/gosky/api/atproto"
	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.feed.setVote

func init() {
}

type FeedSetVote_Input struct {
	Subject   *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
	Direction string                         `json:"direction" cborgen:"direction"`
}

type FeedSetVote_Output struct {
	Upvote   string `json:"upvote" cborgen:"upvote"`
	Downvote string `json:"downvote" cborgen:"downvote"`
}

func FeedSetVote(ctx context.Context, c *xrpc.Client, input FeedSetVote_Input) (*FeedSetVote_Output, error) {
	var out FeedSetVote_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.feed.setVote", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
