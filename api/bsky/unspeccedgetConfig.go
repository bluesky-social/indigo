// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.

package bsky

// schema: app.bsky.unspecced.getConfig

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
)

// UnspeccedGetConfig_LiveNowConfig is a "liveNowConfig" in the app.bsky.unspecced.getConfig schema.
type UnspeccedGetConfig_LiveNowConfig struct {
	Did     string   `json:"did" cborgen:"did"`
	Domains []string `json:"domains" cborgen:"domains"`
}

// UnspeccedGetConfig_Output is the output of a app.bsky.unspecced.getConfig call.
type UnspeccedGetConfig_Output struct {
	CheckEmailConfirmed *bool                               `json:"checkEmailConfirmed,omitempty" cborgen:"checkEmailConfirmed,omitempty"`
	LiveNow             []*UnspeccedGetConfig_LiveNowConfig `json:"liveNow,omitempty" cborgen:"liveNow,omitempty"`
}

// UnspeccedGetConfig calls the XRPC method "app.bsky.unspecced.getConfig".
func UnspeccedGetConfig(ctx context.Context, c util.LexClient) (*UnspeccedGetConfig_Output, error) {
	var out UnspeccedGetConfig_Output
	if err := c.LexDo(ctx, util.Query, "", "app.bsky.unspecced.getConfig", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
