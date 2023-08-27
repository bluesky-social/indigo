package identity

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// TODO: refactor this to wrap a regular Catalog. have it always update both handle and identity maps together.
type CacheCatalog struct {
	HitTTL        time.Duration
	ErrTTL        time.Duration
	PLCURL        string
	mutex         sync.RWMutex
	handleCache   map[syntax.Handle]HandleEntry
	identityCache map[syntax.DID]IdentityEntry
}

type HandleEntry struct {
	Updated time.Time
	DID     syntax.DID
	Err     error
}

type IdentityEntry struct {
	Updated  time.Time
	Identity *Identity
	Err      error
}

var _ Catalog = (*CacheCatalog)(nil)

func NewCacheCatalog(plcURL string) CacheCatalog {
	// TODO: these are kind of arbitrary default values
	hitTTL, err := time.ParseDuration("1h")
	if err != nil {
		panic(err)
	}
	errTTL, err := time.ParseDuration("2m")
	if err != nil {
		panic(err)
	}
	return CacheCatalog{
		HitTTL:        hitTTL,
		ErrTTL:        errTTL,
		PLCURL:        plcURL,
		handleCache:   make(map[syntax.Handle]HandleEntry, 10),
		identityCache: make(map[syntax.DID]IdentityEntry, 10),
	}
}

func (c *CacheCatalog) updateHandle(ctx context.Context, h syntax.Handle) (*HandleEntry, error) {
	did, err := ResolveHandle(ctx, h)
	entry := HandleEntry{
		Updated: time.Now(),
		DID:     did,
		Err:     err,
	}
	c.mutex.Lock()
	c.handleCache[h] = entry
	c.mutex.Unlock()
	return &entry, nil
}

func (c *CacheCatalog) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	var err error
	var entry *HandleEntry
	c.mutex.RLock()
	eObj, ok := c.handleCache[h]
	c.mutex.RUnlock()

	if !ok {
		entry, err = c.updateHandle(ctx, h)
		if err != nil {
			return "", err
		}
	} else {
		entry = &eObj
	}
	if (entry.Err == nil && time.Since(entry.Updated) > c.HitTTL) || (entry.Err != nil && time.Since(entry.Updated) > c.ErrTTL) {
		entry, err = c.updateHandle(ctx, h)
		if err != nil {
			return "", err
		}
	}
	return entry.DID, entry.Err
}

func (c *CacheCatalog) getIdentity(ctx context.Context, did syntax.DID) (*Identity, error) {
	doc, err := ResolveDID(ctx, did)
	if err != nil {
		return nil, err
	}
	ident := ParseIdentity(doc)
	declared, err := ident.DeclaredHandle()
	if err != nil {
		return nil, err
	}
	resolvedDID, err := c.ResolveHandle(ctx, declared)
	if err != nil {
		return nil, err
	}
	if resolvedDID == did {
		ident.Handle = declared
	}

	// optimistic caching of public key
	pk, err := ident.PublicKey()
	if nil == err {
		ident.ParsedPublicKey = pk
	}
	return &ident, nil
}

func (c *CacheCatalog) updateIdentity(ctx context.Context, did syntax.DID) (*IdentityEntry, error) {
	ident, err := c.getIdentity(ctx, did)
	entry := IdentityEntry{
		Updated:  time.Now(),
		Identity: ident,
		Err:      err,
	}

	c.mutex.Lock()
	c.identityCache[did] = entry
	c.mutex.Unlock()
	return &entry, nil
}

func (c *CacheCatalog) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	var err error
	var entry *IdentityEntry
	c.mutex.RLock()
	eObj, ok := c.identityCache[did]
	c.mutex.RUnlock()

	if !ok {
		entry, err = c.updateIdentity(ctx, did)
		if err != nil {
			return nil, err
		}
	} else {
		entry = &eObj
	}
	if (entry.Err == nil && time.Since(entry.Updated) > c.HitTTL) || (entry.Err != nil && time.Since(entry.Updated) > c.ErrTTL) {
		entry, err = c.updateIdentity(ctx, did)
		if err != nil {
			return nil, err
		}
	}
	return entry.Identity, entry.Err
}

func (c *CacheCatalog) LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error) {
	did, err := c.ResolveHandle(ctx, h)
	if err != nil {
		return nil, err
	}
	ident, err := c.LookupDID(ctx, did)
	if err != nil {
		return nil, err
	}

	declared, err := ident.DeclaredHandle()
	if err != nil {
		return nil, err
	}
	if declared != h {
		return nil, fmt.Errorf("handle does not match that declared in DID document")
	}
	return ident, nil
}

func (c *CacheCatalog) Lookup(ctx context.Context, a syntax.AtIdentifier) (*Identity, error) {
	handle, err := a.AsHandle()
	if err == nil {
		return c.LookupHandle(ctx, handle)
	}
	did, err := a.AsDID()
	if err == nil {
		return c.LookupDID(ctx, did)
	}
	return nil, fmt.Errorf("at-identifier neither a Handle nor a DID")
}
