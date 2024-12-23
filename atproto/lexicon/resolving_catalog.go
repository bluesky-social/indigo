package lexicon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
	// TODO: not passed through!
	ctx := context.Background()

	if ref == "" {
		return nil, fmt.Errorf("tried to resolve empty string name")
	}

	// TODO: split on '#'
	nsid, err := syntax.ParseNSID(ref)
	if err != nil {
		return nil, err
	}

	record, err := ResolveLexiconData(ctx, rc.Directory, nsid)
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
