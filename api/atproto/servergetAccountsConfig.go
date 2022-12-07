package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.server.getAccountsConfig

type ServerGetAccountsConfig_Output struct {
	InviteCodeRequired   bool     `json:"inviteCodeRequired"`
	AvailableUserDomains []string `json:"availableUserDomains"`
}

func (t *ServerGetAccountsConfig_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["availableUserDomains"] = t.AvailableUserDomains
	out["inviteCodeRequired"] = t.InviteCodeRequired
	return json.Marshal(out)
}

func ServerGetAccountsConfig(ctx context.Context, c *xrpc.Client) (*ServerGetAccountsConfig_Output, error) {
	var out ServerGetAccountsConfig_Output
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.server.getAccountsConfig", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
