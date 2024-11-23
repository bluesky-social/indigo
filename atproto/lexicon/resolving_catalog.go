package lexicon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/data"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/xrpc"
)

// Catalog which supplements an in-memory BaseCatalog with live resolution from the network
type ResolvingCatalog struct {
	Base      BaseCatalog
	Resolver  net.Resolver
	Directory identity.Directory
}

// TODO: maybe this should take a base catalog as an arg?
func NewResolvingCatalog() ResolvingCatalog {
	return ResolvingCatalog{
		Base: NewBaseCatalog(),
		Resolver: net.Resolver{
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: time.Second * 5}
				return d.DialContext(ctx, network, address)
			},
		},
		Directory: identity.DefaultDirectory(),
	}
}

func (rc *ResolvingCatalog) Resolve(ref string) (*Schema, error) {
	if ref == "" {
		return nil, fmt.Errorf("tried to resolve empty string name")
	}

	existing, err := rc.Base.Resolve(ref)
	if nil == err {
		return existing, nil
	}

	// TODO: was the idea to split the API and have "ensure" as a method?
	ctx := context.Background()

	// TODO: split on '#'
	nsid, err := syntax.ParseNSID(ref)
	if err != nil {
		return nil, err
	}

	did, err := rc.ResolveNSID(ctx, nsid)
	if err != nil {
		return nil, err
	}
	slog.Info("resolved NSID", "nsid", nsid, "did", did)

	ident, err := rc.Directory.LookupDID(ctx, did)
	if err != nil {
		return nil, err
	}

	aturi := syntax.ATURI(fmt.Sprintf("at://%s/com.atproto.lexicon.record/%s", did, nsid))
	record, err := fetchRecord(ctx, *ident, aturi)
	if err != nil {
		return nil, err
	}

	recordJSON, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}

	var sf SchemaFile
	if err = json.Unmarshal(recordJSON, &sf); err != nil {
		return nil, err
	}
	if err = rc.Base.AddSchemaFile(sf); err != nil {
		return nil, err
	}

	return rc.Base.Resolve(ref)
}

var (
	ErrResolutionFailed = fmt.Errorf("NSID resolution mechanism failed")
	ErrNotFound         = fmt.Errorf("NSID not associated with a DID")
)

func parseTXTResp(res []string) (syntax.DID, error) {
	for _, s := range res {
		if strings.HasPrefix(s, "did=") {
			parts := strings.SplitN(s, "=", 2)
			did, err := syntax.ParseDID(parts[1])
			if err != nil {
				return "", fmt.Errorf("%w: invalid DID in handle DNS record: %w", ErrResolutionFailed, err)
			}
			return did, nil
		}
	}
	return "", ErrNotFound
}

// resolves an NSID to a DID, using lexicon DNS TXT record
func (rc *ResolvingCatalog) ResolveNSID(ctx context.Context, nsid syntax.NSID) (syntax.DID, error) {

	domain := nsid.Authority()
	res, err := rc.Resolver.LookupTXT(ctx, "_lexicon."+domain)
	// check for NXDOMAIN
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "", ErrNotFound
		}
	}
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrResolutionFailed, err)
	}
	return parseTXTResp(res)
}

func fetchRecord(ctx context.Context, ident identity.Identity, aturi syntax.ATURI) (any, error) {

	slog.Debug("fetching record", "did", ident.DID.String(), "collection", aturi.Collection().String(), "rkey", aturi.RecordKey().String())
	xrpcc := xrpc.Client{
		Host: ident.PDSEndpoint(),
	}
	resp, err := RepoGetRecord(ctx, &xrpcc, "", aturi.Collection().String(), ident.DID.String(), aturi.RecordKey().String())
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
