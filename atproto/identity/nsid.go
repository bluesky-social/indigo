package identity

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

var (
	ErrNSIDResolutionFailed = fmt.Errorf("NSID resolution mechanism failed")
	ErrNSIDNotFound         = fmt.Errorf("NSID not associated with a DID")
)

// Resolves an NSID to a DID, as used for Lexicon resolution (using "_lexicon" DNS TXT record)
func (d *BaseDirectory) ResolveNSID(ctx context.Context, nsid syntax.NSID) (syntax.DID, error) {

	domain := nsid.Authority()
	res, err := d.Resolver.LookupTXT(ctx, "_lexicon."+domain)

	// check for NXDOMAIN
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "", ErrNSIDNotFound
		}
	}

	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNSIDResolutionFailed, err)
	}

	return parseTXTResp(res)
}
