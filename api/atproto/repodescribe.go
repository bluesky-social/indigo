package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.repo.describe

func init() {
}

type RepoDescribe_Output struct {
	LexiconTypeID   string                  `json:"$type,omitempty"`
	Collections     []string                `json:"collections" cborgen:"collections"`
	Did             string                  `json:"did" cborgen:"did"`
	DidDoc          util.LexiconTypeDecoder `json:"didDoc" cborgen:"didDoc"`
	Handle          string                  `json:"handle" cborgen:"handle"`
	HandleIsCorrect bool                    `json:"handleIsCorrect" cborgen:"handleIsCorrect"`
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
