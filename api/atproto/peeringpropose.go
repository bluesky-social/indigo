package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.peering.propose

func init() {
}

type PeeringPropose_Input struct {
	LexiconTypeID string                   `json:"$type,omitempty"`
	Proposal      *PeeringPropose_Proposal `json:"proposal" cborgen:"proposal"`
	Signature     string                   `json:"signature" cborgen:"signature"`
}

type PeeringPropose_Output struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Rejected      bool   `json:"rejected" cborgen:"rejected"`
}

type PeeringPropose_Proposal struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Proposer      *string `json:"proposer,omitempty" cborgen:"proposer"`
}

func PeeringPropose(ctx context.Context, c *xrpc.Client, input *PeeringPropose_Input) (*PeeringPropose_Output, error) {
	var out PeeringPropose_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "com.atproto.peering.propose", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
