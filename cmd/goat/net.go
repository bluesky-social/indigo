package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bluesky-social/indigo/api/agnostic"
	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"
)

func fetchRecord(ctx context.Context, ident identity.Identity, aturi syntax.ATURI) (map[string]any, error) {

	slog.Debug("fetching record", "did", ident.DID.String(), "collection", aturi.Collection().String(), "rkey", aturi.RecordKey().String())
	xrpcc := xrpc.Client{
		Host: ident.PDSEndpoint(),
	}
	resp, err := agnostic.RepoGetRecord(ctx, &xrpcc, "", aturi.Collection().String(), ident.DID.String(), aturi.RecordKey().String())
	if err != nil {
		return nil, err
	}

	if nil == resp.Value {
		return nil, fmt.Errorf("empty record in response")
	}
	record, err := data.UnmarshalJSON(*resp.Value)
	if err != nil {
		return nil, fmt.Errorf("fetched record was invalid data: %w", err)
	}
	return record, nil
}
