package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.server.getAccountsConfig

type ServerGetAccountsConfig_Output struct {
	InviteCodeRequired   bool                           `json:"inviteCodeRequired" cborgen:"inviteCodeRequired"`
	AvailableUserDomains []string                       `json:"availableUserDomains" cborgen:"availableUserDomains"`
	Links                *ServerGetAccountsConfig_Links `json:"links" cborgen:"links"`
}

func (t *ServerGetAccountsConfig_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["availableUserDomains"] = t.AvailableUserDomains
	out["inviteCodeRequired"] = t.InviteCodeRequired
	out["links"] = t.Links
	return json.Marshal(out)
}

type ServerGetAccountsConfig_Links struct {
	PrivacyPolicy  string `json:"privacyPolicy" cborgen:"privacyPolicy"`
	TermsOfService string `json:"termsOfService" cborgen:"termsOfService"`
}

func (t *ServerGetAccountsConfig_Links) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["privacyPolicy"] = t.PrivacyPolicy
	out["termsOfService"] = t.TermsOfService
	return json.Marshal(out)
}

func ServerGetAccountsConfig(ctx context.Context, c *xrpc.Client) (*ServerGetAccountsConfig_Output, error) {
	var out ServerGetAccountsConfig_Output
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.server.getAccountsConfig", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
