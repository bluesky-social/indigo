package identity

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type CacheCatalog struct {
	HitTTL        time.Duration
	ErrTTL        time.Duration
	Inner         Catalog
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

func NewCacheCatalog(inner Catalog) CacheCatalog {
	// TODO: these are kind of arbitrary default values...
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
		Inner:         inner,
		handleCache:   make(map[syntax.Handle]HandleEntry, 10),
		identityCache: make(map[syntax.DID]IdentityEntry, 10),
	}
}

func (c *CacheCatalog) IsHandleStale(e *HandleEntry) bool {
	if nil == e.Err && time.Since(e.Updated) > c.HitTTL {
		return true
	}
	if e.Err != nil && time.Since(e.Updated) > c.ErrTTL {
		return true
	}
	return false
}

func (c *CacheCatalog) IsIdentityStale(e *IdentityEntry) bool {
	if nil == e.Err && time.Since(e.Updated) > c.HitTTL {
		return true
	}
	if e.Err != nil && time.Since(e.Updated) > c.ErrTTL {
		return true
	}
	return false
}

func (c *CacheCatalog) updateHandle(ctx context.Context, h syntax.Handle) (*HandleEntry, error) {
	ident, err := c.Inner.LookupHandle(ctx, h)
	if err != nil {
		he := HandleEntry{
			Updated: time.Now(),
			DID:     "",
			Err:     err,
		}
		c.mutex.Lock()
		c.handleCache[h] = he
		c.mutex.Unlock()
		return &he, nil
	}

	entry := IdentityEntry{
		Updated:  time.Now(),
		Identity: ident,
		Err:      nil,
	}
	he := HandleEntry{
		Updated: time.Now(),
		DID:     ident.DID,
		Err:     nil,
	}

	c.mutex.Lock()
	c.identityCache[ident.DID] = entry
	c.handleCache[ident.Handle] = he
	c.mutex.Unlock()
	return &he, nil
}

func (c *CacheCatalog) ResolveHandle(ctx context.Context, h syntax.Handle) (syntax.DID, error) {
	var err error
	var entry *HandleEntry
	c.mutex.RLock()
	maybeEntry, ok := c.handleCache[h]
	c.mutex.RUnlock()

	if !ok {
		entry, err = c.updateHandle(ctx, h)
		if err != nil {
			return "", err
		}
	} else {
		entry = &maybeEntry
	}
	if c.IsHandleStale(entry) {
		entry, err = c.updateHandle(ctx, h)
		if err != nil {
			return "", err
		}
	}
	return entry.DID, entry.Err
}

func (c *CacheCatalog) updateDID(ctx context.Context, did syntax.DID) (*IdentityEntry, error) {
	ident, err := c.Inner.LookupDID(ctx, did)
	// persist the identity lookup error, instead of processing it immediately
	entry := IdentityEntry{
		Updated:  time.Now(),
		Identity: ident,
		Err:      err,
	}
	var he *HandleEntry
	// if *not* an error, then also update the handle cache
	if nil == err && !ident.Handle.IsInvalidHandle() {
		he = &HandleEntry{
			Updated: time.Now(),
			DID:     did,
			Err:     nil,
		}
	}

	c.mutex.Lock()
	c.identityCache[did] = entry
	if he != nil {
		c.handleCache[ident.Handle] = *he
	}
	c.mutex.Unlock()
	return &entry, nil
}

func (c *CacheCatalog) LookupDID(ctx context.Context, did syntax.DID) (*Identity, error) {
	var err error
	var entry *IdentityEntry
	c.mutex.RLock()
	maybeEntry, ok := c.identityCache[did]
	c.mutex.RUnlock()

	if !ok {
		entry, err = c.updateDID(ctx, did)
		if err != nil {
			return nil, err
		}
	} else {
		entry = &maybeEntry
	}
	if c.IsIdentityStale(entry) {
		entry, err = c.updateDID(ctx, did)
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
	if nil == err { // if not an error, is a handle
		return c.LookupHandle(ctx, handle)
	}
	did, err := a.AsDID()
	if nil == err { // if not an error, is a DID
		return c.LookupDID(ctx, did)
	}
	return nil, fmt.Errorf("at-identifier neither a Handle nor a DID")
}
