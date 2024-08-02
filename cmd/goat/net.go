package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

func fetchRecord(ctx context.Context, ident identity.Identity, aturi syntax.ATURI) (any, error) {
	pdsURL := ident.PDSEndpoint()

	slog.Debug("fetching record", "did", ident.DID.String(), "collection", aturi.Collection().String(), "rkey", aturi.RecordKey().String())
	url := fmt.Sprintf("%s/xrpc/com.atproto.repo.getRecord?repo=%s&collection=%s&rkey=%s",
		pdsURL, ident.DID, aturi.Collection(), aturi.RecordKey())
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch failed")
	}
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	body, err := data.UnmarshalJSON(respBytes)
	record, ok := body["value"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("fetched record was not an object")
	}
	return record, nil
}
