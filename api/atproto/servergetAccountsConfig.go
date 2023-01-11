package schemagen

import (
	"context"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: com.atproto.server.getAccountsConfig

func init() {
}

type ServerGetAccountsConfig_Links struct {
	LexiconTypeID  string  `json:"$type,omitempty"`
	PrivacyPolicy  *string `json:"privacyPolicy,omitempty" cborgen:"privacyPolicy"`
	TermsOfService *string `json:"termsOfService,omitempty" cborgen:"termsOfService"`
}

type ServerGetAccountsConfig_Output struct {
	LexiconTypeID        string                         `json:"$type,omitempty"`
	AvailableUserDomains []string                       `json:"availableUserDomains" cborgen:"availableUserDomains"`
	InviteCodeRequired   *bool                          `json:"inviteCodeRequired,omitempty" cborgen:"inviteCodeRequired"`
	Links                *ServerGetAccountsConfig_Links `json:"links,omitempty" cborgen:"links"`
}

func ServerGetAccountsConfig(ctx context.Context, c *xrpc.Client) (*ServerGetAccountsConfig_Output, error) {
	var out ServerGetAccountsConfig_Output
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.server.getAccountsConfig", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
