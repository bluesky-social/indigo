// Code generated by cmd/lexgen (see Makefile's lexgen); DO NOT EDIT.

package bsky

// schema: app.bsky.video.getUploadLimits

import (
	"context"

	"github.com/bluesky-social/indigo/lex/util"
)

// VideoGetUploadLimits_Output is the output of a app.bsky.video.getUploadLimits call.
type VideoGetUploadLimits_Output struct {
	CanUpload            bool    `json:"canUpload" cborgen:"canUpload"`
	Error                *string `json:"error,omitempty" cborgen:"error,omitempty"`
	Message              *string `json:"message,omitempty" cborgen:"message,omitempty"`
	RemainingDailyBytes  *int64  `json:"remainingDailyBytes,omitempty" cborgen:"remainingDailyBytes,omitempty"`
	RemainingDailyVideos *int64  `json:"remainingDailyVideos,omitempty" cborgen:"remainingDailyVideos,omitempty"`
}

// VideoGetUploadLimits calls the XRPC method "app.bsky.video.getUploadLimits".
func VideoGetUploadLimits(ctx context.Context, c util.LexClient) (*VideoGetUploadLimits_Output, error) {
	var out VideoGetUploadLimits_Output
	if err := c.LexDo(ctx, util.Query, "", "app.bsky.video.getUploadLimits", nil, nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}
