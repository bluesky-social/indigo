package engine

import (
	"context"
	"fmt"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"
)

func (e *Engine) FetchAndProcessRecord(ctx context.Context, aturi syntax.ATURI) error {
	// resolve URI, identity, and record
	if aturi.RecordKey() == "" {
		return fmt.Errorf("need a full, not partial, AT-URI: %s", aturi)
	}
	ident, err := e.Directory.Lookup(ctx, aturi.Authority())
	if err != nil {
		return fmt.Errorf("resolving AT-URI authority: %v", err)
	}
	pdsURL := ident.PDSEndpoint()
	if pdsURL == "" {
		return fmt.Errorf("could not resolve PDS endpoint for AT-URI account: %s", ident.DID.String())
	}
	pdsClient := xrpc.Client{Host: ident.PDSEndpoint()}

	e.Logger.Info("fetching record", "did", ident.DID.String(), "collection", aturi.Collection().String(), "rkey", aturi.RecordKey().String())
	out, err := comatproto.RepoGetRecord(ctx, &pdsClient, "", aturi.Collection().String(), ident.DID.String(), aturi.RecordKey().String())
	if err != nil {
		return fmt.Errorf("fetching record from Relay (%s): %v", aturi, err)
	}
	if out.Cid == nil {
		return fmt.Errorf("expected a CID in getRecord response")
	}
	return e.ProcessRecord(ctx, ident.DID, aturi.Path(), *out.Cid, out.Value.Val)
}

func (e *Engine) FetchRecent(ctx context.Context, atid syntax.AtIdentifier, limit int) (*identity.Identity, []*comatproto.RepoListRecords_Record, error) {
	ident, err := e.Directory.Lookup(ctx, atid)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve AT identifier: %v", err)
	}
	pdsURL := ident.PDSEndpoint()
	if pdsURL == "" {
		return nil, nil, fmt.Errorf("could not resolve PDS endpoint for account: %s", ident.DID.String())
	}
	pdsClient := xrpc.Client{Host: ident.PDSEndpoint()}

	resp, err := comatproto.RepoListRecords(ctx, &pdsClient, "app.bsky.feed.post", "", int64(limit), ident.DID.String(), false, "", "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch record list: %v", err)
	}
	e.Logger.Info("got recent posts", "did", ident.DID.String(), "pds", pdsURL, "count", len(resp.Records))
	return ident, resp.Records, nil
}

func (e *Engine) FetchAndProcessRecent(ctx context.Context, atid syntax.AtIdentifier, limit int) error {

	ident, records, err := e.FetchRecent(ctx, atid, limit)
	if err != nil {
		return err
	}
	// records are most-recent first; we want recent but oldest-first, so iterate backwards
	for i := range records {
		rec := records[len(records)-i-1]
		aturi, err := syntax.ParseATURI(rec.Uri)
		if err != nil {
			return fmt.Errorf("parsing PDS record response: %v", err)
		}
		err = e.ProcessRecord(ctx, ident.DID, aturi.Path(), rec.Cid, rec.Value.Val)
		if err != nil {
			return err
		}
	}
	return nil
}
