package lexicon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Catalog which supplements an in-memory BaseCatalog with live resolution from the network
type ResolvingCatalog struct {
	Base      BaseCatalog
	Directory identity.Directory
}

func NewResolvingCatalog() ResolvingCatalog {
	return ResolvingCatalog{
		Base:      NewBaseCatalog(),
		Directory: identity.DefaultDirectory(),
	}
}

func (rc *ResolvingCatalog) Resolve(ref string) (*Schema, error) {
	// NOTE: not passed through!
	ctx := context.Background()

	if ref == "" {
		return nil, fmt.Errorf("tried to resolve empty string name")
	}

	// first try existing catalog
	schema, err := rc.Base.Resolve(ref)
	if nil == err { // no error: found a hit
		return schema, nil
	}

	// split any ref from the end '#'
	parts := strings.SplitN(ref, "#", 2)
	nsid, err := syntax.ParseNSID(parts[0])
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

	if sf.Lexicon != 1 {
		return nil, fmt.Errorf("unsupported lexicon language version: %d", sf.Lexicon)
	}
	if sf.ID != nsid.String() {
		return nil, fmt.Errorf("lexicon ID does not match NSID: %s != %s", sf.ID, nsid)
	}
	if err = rc.Base.AddSchemaFile(sf); err != nil {
		return nil, err
	}

	// re-resolving from the raw ref ensures that fragments are handled
	return rc.Base.Resolve(ref)
}
