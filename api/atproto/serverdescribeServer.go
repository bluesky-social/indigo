package atproto

import (
	"context"

	"github.com/bluesky-social/indigo/xrpc"
)

// schema: com.atproto.server.describeServer

func init() {
}

type ServerDescribeServer_Links struct {
	PrivacyPolicy  *string `json:"privacyPolicy,omitempty" cborgen:"privacyPolicy,omitempty"`
	TermsOfService *string `json:"termsOfService,omitempty" cborgen:"termsOfService,omitempty"`
}

type ServerDescribeServer_Output struct {
	AvailableUserDomains []string                    `json:"availableUserDomains" cborgen:"availableUserDomains"`
	InviteCodeRequired   *bool                       `json:"inviteCodeRequired,omitempty" cborgen:"inviteCodeRequired,omitempty"`
	Links                *ServerDescribeServer_Links `json:"links,omitempty" cborgen:"links,omitempty"`
}

func ServerDescribeServer(ctx context.Context, c *xrpc.Client) (*ServerDescribeServer_Output, error) {
	var out ServerDescribeServer_Output
	if err := c.Do(ctx, xrpc.Query, "", "com.atproto.server.describeServer", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
