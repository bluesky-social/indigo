package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getMutes

func init() {
}

type GraphGetMutes_Mute struct {
	LexiconTypeID string         `json:"$type,omitempty"`
	CreatedAt     string         `json:"createdAt" cborgen:"createdAt"`
	Declaration   *SystemDeclRef `json:"declaration" cborgen:"declaration"`
	Did           string         `json:"did" cborgen:"did"`
	DisplayName   *string        `json:"displayName,omitempty" cborgen:"displayName"`
	Handle        string         `json:"handle" cborgen:"handle"`
}

type GraphGetMutes_Output struct {
	LexiconTypeID string                `json:"$type,omitempty"`
	Cursor        *string               `json:"cursor,omitempty" cborgen:"cursor"`
	Mutes         []*GraphGetMutes_Mute `json:"mutes" cborgen:"mutes"`
}

func GraphGetMutes(ctx context.Context, c *xrpc.Client, before string, limit int64) (*GraphGetMutes_Output, error) {
	var out GraphGetMutes_Output

	params := map[string]interface{}{
		"before": before,
		"limit":  limit,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.graph.getMutes", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
