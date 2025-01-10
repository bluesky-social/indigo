package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/lexicon"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/urfave/cli/v2"
)

func runValidateRecord(cctx *cli.Context) error {
	ctx := context.Background()
	args := cctx.Args().Slice()
	if len(args) != 2 {
		return fmt.Errorf("expected two args (catalog path and AT-URI)")
	}
	p := args[0]
	if p == "" {
		return fmt.Errorf("need to provide directory path as an argument")
	}

	cat := lexicon.NewBaseCatalog()
	err := cat.LoadDirectory(p)
	if err != nil {
		return err
	}

	aturi, err := syntax.ParseATURI(args[1])
	if err != nil {
		return err
	}
	if aturi.RecordKey() == "" {
		return fmt.Errorf("need a full, not partial, AT-URI: %s", aturi)
	}
	dir := identity.DefaultDirectory()
	ident, err := dir.Lookup(ctx, aturi.Authority())
	if err != nil {
		return fmt.Errorf("resolving AT-URI authority: %v", err)
	}
	pdsURL := ident.PDSEndpoint()
	if pdsURL == "" {
		return fmt.Errorf("could not resolve PDS endpoint for AT-URI account: %s", ident.DID.String())
	}

	slog.Info("fetching record", "did", ident.DID.String(), "collection", aturi.Collection().String(), "rkey", aturi.RecordKey().String())
	url := fmt.Sprintf("%s/xrpc/com.atproto.repo.getRecord?repo=%s&collection=%s&rkey=%s",
		pdsURL, ident.DID, aturi.Collection(), aturi.RecordKey())
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch failed")
	}
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	body, err := data.UnmarshalJSON(respBytes)
	record, ok := body["value"].(map[string]any)
	if !ok {
		return fmt.Errorf("fetched record was not an object")
	}

	slog.Info("validating", "did", ident.DID.String(), "collection", aturi.Collection().String(), "rkey", aturi.RecordKey().String())
	err = lexicon.ValidateRecord(&cat, record, aturi.Collection().String(), lexicon.LenientMode)
	if err != nil {
		return err
	}
	fmt.Println("success!")
	return nil
}
