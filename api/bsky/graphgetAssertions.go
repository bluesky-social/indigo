package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getAssertions

func init() {
}

type GraphGetAssertions_Assertion struct {
	LexiconTypeID string                           `json:"$type,omitempty"`
	Assertion     string                           `json:"assertion" cborgen:"assertion"`
	Author        *ActorRef_WithInfo               `json:"author" cborgen:"author"`
	Cid           string                           `json:"cid" cborgen:"cid"`
	Confirmation  *GraphGetAssertions_Confirmation `json:"confirmation,omitempty" cborgen:"confirmation"`
	CreatedAt     string                           `json:"createdAt" cborgen:"createdAt"`
	IndexedAt     string                           `json:"indexedAt" cborgen:"indexedAt"`
	Subject       *ActorRef_WithInfo               `json:"subject" cborgen:"subject"`
	Uri           string                           `json:"uri" cborgen:"uri"`
}

type GraphGetAssertions_Confirmation struct {
	LexiconTypeID string `json:"$type,omitempty"`
	Cid           string `json:"cid" cborgen:"cid"`
	CreatedAt     string `json:"createdAt" cborgen:"createdAt"`
	IndexedAt     string `json:"indexedAt" cborgen:"indexedAt"`
	Uri           string `json:"uri" cborgen:"uri"`
}

type GraphGetAssertions_Output struct {
	LexiconTypeID string                          `json:"$type,omitempty"`
	Assertions    []*GraphGetAssertions_Assertion `json:"assertions" cborgen:"assertions"`
	Cursor        *string                         `json:"cursor,omitempty" cborgen:"cursor"`
}

func GraphGetAssertions(ctx context.Context, c *xrpc.Client, assertion string, author string, before string, confirmed bool, limit int64, subject string) (*GraphGetAssertions_Output, error) {
	var out GraphGetAssertions_Output

	params := map[string]interface{}{
		"assertion": assertion,
		"author":    author,
		"before":    before,
		"confirmed": confirmed,
		"limit":     limit,
		"subject":   subject,
	}
	if err := c.Do(ctx, xrpc.Query, "", "app.bsky.graph.getAssertions", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
