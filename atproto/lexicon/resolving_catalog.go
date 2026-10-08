package lexicon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/util/ssrf"
)

// Catalog which supplements an in-memory BaseCatalog with live resolution from the network
type ResolvingCatalog struct {
	Base      *BaseCatalog
	Directory identity.Directory
	// This catalog makes HTTP requests to untrusted hosts, so this http.Client must be configured with SSRF protection and other network security mitigations (which NewResolvingCatalog does)
	HTTPClient *http.Client
	lk         sync.RWMutex
}

// Constructs a new ResolvingCatalog with safe defaults.
func NewResolvingCatalog() *ResolvingCatalog {
	return &ResolvingCatalog{
		Base:      NewBaseCatalog(),
		Directory: identity.DefaultDirectory(),
		HTTPClient: &http.Client{
			Timeout:   60 * time.Second,
			Transport: ssrf.PublicOnlyTransport(),
		},
	}
}

func (rc *ResolvingCatalog) Resolve(ref string) (*Schema, error) {
	// NOTE: not passed through!
	ctx := context.Background()

	if ref == "" {
		return nil, fmt.Errorf("tried to resolve empty string name")
	}

	// first try existing catalog
	rc.lk.RLock()
	schema, err := rc.Base.Resolve(ref)
	rc.lk.RUnlock()
	if nil == err { // no error: found a hit
		return schema, nil
	}

	// split any ref from the end '#'
	parts := strings.SplitN(ref, "#", 2)
	nsid, err := syntax.ParseNSID(parts[0])
	if err != nil {
		return nil, err
	}

	recordJSON, err := resolveLexiconJSON(ctx, rc.Directory, nsid, rc.HTTPClient)
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
	rc.lk.Lock()
	defer rc.lk.Unlock()
	if err = rc.Base.AddSchemaFile(sf); err != nil {
		return nil, err
	}

	// re-resolving from the raw ref ensures that fragments are handled
	return rc.Base.Resolve(ref)
}
