package identity

import (
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// API for doing account lookups by DID or handle, with bi-directional verification handled automatically.
//
// Handles which fail to resolve or don't match DID alsoKnownAs are an error. DIDs which resolve but the handle does not resolve back to the DID return an Account where the Handle is the special `handle.invalid` value.
//
// Some example implementations of this interface would be:
//   - naive direct resolution on every call
//   - API client, which just makes requests to PDS (or other remote service)
//   - simple in-memory caching wrapper layer to reduce network hits
//   - services with backing datastore to do sophisticated caching, TTL, auto-refresh, etc
type AccountCatalog interface {
	LookupHandle(h syntax.Handle) (*Account, error)
	LookupDID(d syntax.DID) (*Account, error)
}

// Represents an atproto identity. Could be a regular user account, or a service account (eg, feed generator)
type Account struct {
	// these fields are required and non-nullable
	DID    syntax.DID
	Handle syntax.Handle

	// these fields are nullable
	AlsoKnownAs []string
	Services map[string]Service
	Keys map[string]Key
}

type Key struct {
	Type string
	PublicKeyMultibase string
}

type Service struct {
	Type string
	URL string
}

func (a *Account) SigningKey() *Key {
	if a.Keys == nil {
		return nil
	}
	atp := a.Keys["atproto"]
	if atp == nil {
		return nil
	}
	return &atp
}

func (a *Account) PDS() string {
	if a.Services == nil {
		return nil
	}
	atp := a.Services["atproto_pds"]
	if atp == nil {
		return nil
	}
	return &atp
}
