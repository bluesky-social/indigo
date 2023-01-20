package schemagen

import (
	"context"

	comatprototypes "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: app.bsky.feed.setVote

func init() {
}

type FeedSetVote_Input struct {
	LexiconTypeID string                         `json:"$type,omitempty"`
	Direction     string                         `json:"direction" cborgen:"direction"`
	Subject       *comatprototypes.RepoStrongRef `json:"subject" cborgen:"subject"`
}

type FeedSetVote_Output struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Downvote      *string `json:"downvote,omitempty" cborgen:"downvote"`
	Upvote        *string `json:"upvote,omitempty" cborgen:"upvote"`
}

func FeedSetVote(ctx context.Context, c *xrpc.Client, input *FeedSetVote_Input) (*FeedSetVote_Output, error) {
	var out FeedSetVote_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.feed.setVote", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
