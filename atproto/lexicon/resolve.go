package lexicon

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

// Low-level routine for resolving an NSID to full Lexicon data record (as stored in a repository).
//
// The current implementation uses a naive 'getRepo' fetch to the relevant PDS instance, without validating MST proof chain.
//
// Calling code should usually use ResolvingCatalog, which handles basic caching and validation of the Lexicon language itself.
func ResolveLexiconData(ctx context.Context, dir identity.Directory, nsid syntax.NSID) (map[string]any, error) {

	baseDir := identity.BaseDirectory{}
	did, err := baseDir.ResolveNSID(ctx, nsid)
	if err != nil {
		return nil, err
	}
	slog.Debug("resolved NSID", "nsid", nsid, "did", did)

	ident, err := dir.LookupDID(ctx, did)
	if err != nil {
		return nil, err
	}

	aturi := syntax.ATURI(fmt.Sprintf("at://%s/com.atproto.lexicon.schema/%s", did, nsid))
	record, err := fetchRecord(ctx, *ident, aturi)
	if err != nil {
		return nil, err
	}
	return record, nil
}

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
