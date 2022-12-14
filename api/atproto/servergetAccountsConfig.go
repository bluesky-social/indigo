package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.server.getAccountsConfig

func init() {
}

type ServerGetAccountsConfig_Output struct {
	InviteCodeRequired   *bool    `json:"inviteCodeRequired" cborgen:"inviteCodeRequired"`
	AvailableUserDomains []string `json:"availableUserDomains" cborgen:"availableUserDomains"`
}

func ServerGetAccountsConfig(ctx context.Context, c *xrpc.Client) (*ServerGetAccountsConfig_Output, error) {
	var out ServerGetAccountsConfig_Output
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.server.getAccountsConfig", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
