package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.repo.describe

func init() {
}

type RepoDescribe_Output struct {
	DidDoc          any      `json:"didDoc" cborgen:"didDoc"`
	Collections     []string `json:"collections" cborgen:"collections"`
	HandleIsCorrect bool     `json:"handleIsCorrect" cborgen:"handleIsCorrect"`
	Handle          string   `json:"handle" cborgen:"handle"`
	Did             string   `json:"did" cborgen:"did"`
}

func RepoDescribe(ctx context.Context, c *xrpc.Client, user string) (*RepoDescribe_Output, error) {
	var out RepoDescribe_Output

	params := map[string]interface{}{
		"user": user,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.repo.describe", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
