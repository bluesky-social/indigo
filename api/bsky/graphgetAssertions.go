package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getAssertions

func init() {
}

type GraphGetAssertions_Output struct {
	Cursor     *string                         `json:"cursor" cborgen:"cursor"`
	Assertions []*GraphGetAssertions_Assertion `json:"assertions" cborgen:"assertions"`
}

type GraphGetAssertions_Assertion struct {
	Cid          string                           `json:"cid" cborgen:"cid"`
	Assertion    string                           `json:"assertion" cborgen:"assertion"`
	Confirmation *GraphGetAssertions_Confirmation `json:"confirmation" cborgen:"confirmation"`
	Author       *ActorRef_WithInfo               `json:"author" cborgen:"author"`
	Subject      *ActorRef_WithInfo               `json:"subject" cborgen:"subject"`
	IndexedAt    string                           `json:"indexedAt" cborgen:"indexedAt"`
	CreatedAt    string                           `json:"createdAt" cborgen:"createdAt"`
	Uri          string                           `json:"uri" cborgen:"uri"`
}

type GraphGetAssertions_Confirmation struct {
	Uri       string `json:"uri" cborgen:"uri"`
	Cid       string `json:"cid" cborgen:"cid"`
	IndexedAt string `json:"indexedAt" cborgen:"indexedAt"`
	CreatedAt string `json:"createdAt" cborgen:"createdAt"`
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
