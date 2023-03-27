package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.peering.list

func init() {
}

type PeeringList_Output struct {
	LexiconTypeID string                 `json:"$type,omitempty"`
	Peerings      []*PeeringList_Peering `json:"peerings" cborgen:"peerings"`
}

type PeeringList_Peering struct {
	LexiconTypeID string  `json:"$type,omitempty"`
	Host          *string `json:"host,omitempty" cborgen:"host"`
	Status        *string `json:"status,omitempty" cborgen:"status"`
}

func PeeringList(ctx context.Context, c *xrpc.Client) (*PeeringList_Output, error) {
	var out PeeringList_Output
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.peering.list", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
