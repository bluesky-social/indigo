package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.repo.describeRepo

func init() {
}

type RepoDescribeRepo_Output struct {
	LexiconTypeID   string                   `json:"$type,omitempty" cborgen:"$type,omitempty"`
	Collections     []string                 `json:"collections" cborgen:"collections"`
	Did             string                   `json:"did" cborgen:"did"`
	DidDoc          *util.LexiconTypeDecoder `json:"didDoc" cborgen:"didDoc"`
	Handle          string                   `json:"handle" cborgen:"handle"`
	HandleIsCorrect bool                     `json:"handleIsCorrect" cborgen:"handleIsCorrect"`
}

func RepoDescribeRepo(ctx context.Context, c *xrpc.Client, repo string) (*RepoDescribeRepo_Output, error) {
	var out RepoDescribeRepo_Output

	params := map[string]interface{}{
		"repo": repo,
	}
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.repo.describeRepo", params, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
