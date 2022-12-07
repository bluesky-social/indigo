package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.graph.getAssertions

type GraphGetAssertions_Output struct {
	Cursor     string                          `json:"cursor"`
	Assertions []*GraphGetAssertions_Assertion `json:"assertions"`
}

func (t *GraphGetAssertions_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["assertions"] = t.Assertions
	out["cursor"] = t.Cursor
	return json.Marshal(out)
}

type GraphGetAssertions_Assertion struct {
	IndexedAt    string                           `json:"indexedAt"`
	CreatedAt    string                           `json:"createdAt"`
	Uri          string                           `json:"uri"`
	Cid          string                           `json:"cid"`
	Assertion    string                           `json:"assertion"`
	Confirmation *GraphGetAssertions_Confirmation `json:"confirmation"`
	Author       *ActorRef_WithInfo               `json:"author"`
	Subject      *ActorRef_WithInfo               `json:"subject"`
}

func (t *GraphGetAssertions_Assertion) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["assertion"] = t.Assertion
	out["author"] = t.Author
	out["cid"] = t.Cid
	out["confirmation"] = t.Confirmation
	out["createdAt"] = t.CreatedAt
	out["indexedAt"] = t.IndexedAt
	out["subject"] = t.Subject
	out["uri"] = t.Uri
	return json.Marshal(out)
}

type GraphGetAssertions_Confirmation struct {
	Uri       string `json:"uri"`
	Cid       string `json:"cid"`
	IndexedAt string `json:"indexedAt"`
	CreatedAt string `json:"createdAt"`
}

func (t *GraphGetAssertions_Confirmation) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["cid"] = t.Cid
	out["createdAt"] = t.CreatedAt
	out["indexedAt"] = t.IndexedAt
	out["uri"] = t.Uri
	return json.Marshal(out)
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
