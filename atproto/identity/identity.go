package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/mr-tron/base58"
)

// API for doing account lookups by DID or handle, with bi-directional verification handled automatically. Almost all atproto services and clients should use an implementation of this interface instead of resolving handles or DIDs separately
//
// Handles which fail to resolve, or don't match DID alsoKnownAs, are an error. DIDs which resolve but the handle does not resolve back to the DID return an Identity where the Handle is the special `handle.invalid` value.
//
// Some example implementations of this interface could be:
//   - basic direct resolution on every call
//   - local in-memory caching layer to reduce network hits
//   - API client, which just makes requests to PDS (or other remote service)
//   - client for shared network cache (eg, Redis)
type Catalog interface {
	LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error)
	LookupDID(ctx context.Context, d syntax.DID) (*Identity, error)
	Lookup(ctx context.Context, i syntax.AtIdentifier) (*Identity, error)
	// TODO: add "flush" methods to purge caches?
}

// Indicates that resolution process completed successfully, but handle does not exist.
var ErrHandleNotFound = errors.New("handle not found")

// Indicates that handle and DID resolved, but handle points to a DID with a different handle. This is only returned when looking up a handle, not when looking up a DID.
var ErrHandleNotValid = errors.New("handle resolves to DID with different handle")

// Indicates that resolution process completed successfully, but the DID does not exist.
var ErrDIDNotFound = errors.New("DID not found")

var DefaultPLCURL = "https://plc.directory"

// Returns a reasonable default Catalog implementation for most use cases
func DefaultCatalog() Catalog {
	naive := NewBasicCatalog(DefaultPLCURL)
	cached := NewCacheCatalog(&naive)
	return &cached
}

// Represents an atproto identity. Could be a regular user account, or a service account (eg, feed generator)
type Identity struct {
	DID syntax.DID

	// Handle/DID mapping must be bi-directionally verified. If that fails, the Handle should be the special 'handle.invalid' value
	// TODO: should we make this nullable, instead of 'handle.invalid'?
	Handle syntax.Handle

	// These fields represent a parsed subset of a DID document. They are all nullable.
	// TODO: should we just embed DIDDocument here?
	AlsoKnownAs []string
	// TODO: this doesn't preserve order (doesn't round-trip)
	Services map[string]Service
	// TODO: this doesn't preserve order (doesn't round-trip)
	Keys map[string]Key

	// If a valid atproto repo signing public key was parsed, it can be cached here. This is nullable/optional. Calling code should call PublicKey() instead of accessing this member.
	ParsedPublicKey crypto.PublicKey
}

type Key struct {
	Type               string
	PublicKeyMultibase string
}

type Service struct {
	Type string
	URL  string
}

func ParseIdentity(doc *DIDDocument) Identity {
	keys := make(map[string]Key, len(doc.VerificationMethod))
	for _, vm := range doc.VerificationMethod {
		parts := strings.SplitN(vm.ID, "#", 2)
		if len(parts) < 2 {
			continue
		}
		// ignore keys not controlled by this DID itself
		if vm.Controller != doc.DID.String() {
			continue
		}
		// don't want to clobber existing entries with same ID fragment
		if _, ok := keys[parts[1]]; ok {
			continue
		}
		// TODO: verify that ID and type match for atproto-specific services?
		keys[parts[1]] = Key{
			Type:               vm.Type,
			PublicKeyMultibase: vm.PublicKeyMultibase,
		}
	}
	svc := make(map[string]Service, len(doc.Service))
	for _, s := range doc.Service {
		parts := strings.SplitN(s.ID, "#", 2)
		if len(parts) < 2 {
			continue
		}
		// don't want to clobber existing entries with same ID fragment
		if _, ok := svc[parts[1]]; ok {
			continue
		}
		// TODO: verify that ID and type match for atproto-specific services?
		svc[parts[1]] = Service{
			Type: s.Type,
			URL:  s.ServiceEndpoint,
		}
	}
	return Identity{
		DID:         doc.DID,
		Handle:      syntax.Handle("invalid.handle"),
		AlsoKnownAs: doc.AlsoKnownAs,
		Services:    svc,
		Keys:        keys,
	}
}

// Identifiers the atproto repo signing public key, specifically, out of any keys associated with this identity.
//
// Returns an error if there is no key. Note that 'crypto.PublicKey' is an interface, not a concrete type.
func (i *Identity) PublicKey() (crypto.PublicKey, error) {
	if i.ParsedPublicKey != nil {
		return i.ParsedPublicKey, nil
	}
	if i.Keys == nil {
		return nil, fmt.Errorf("identity has no atproto public key attached")
	}
	k, ok := i.Keys["atproto"]
	if !ok {
		return nil, fmt.Errorf("identity has no atproto public key attached")
	}
	switch k.Type {
	case "Multikey":
		return crypto.ParsePublicMultibase(k.PublicKeyMultibase)
	case "EcdsaSecp256r1VerificationKey2019":
		if len(k.PublicKeyMultibase) < 2 || k.PublicKeyMultibase[0] != 'z' {
			return nil, fmt.Errorf("identity key not a multibase base58btc string")
		}
		keyBytes, err := base58.Decode(k.PublicKeyMultibase[1:])
		if err != nil {
			return nil, fmt.Errorf("identity key multibase parsing: %w", err)
		}
		return crypto.ParsePublicUncompressedBytesP256(keyBytes)
	case "EcdsaSecp256k1VerificationKey2019":
		if len(k.PublicKeyMultibase) < 2 || k.PublicKeyMultibase[0] != 'z' {
			return nil, fmt.Errorf("identity key not a multibase base58btc string")
		}
		keyBytes, err := base58.Decode(k.PublicKeyMultibase[1:])
		if err != nil {
			return nil, fmt.Errorf("identity key multibase parsing: %w", err)
		}
		return crypto.ParsePublicUncompressedBytesK256(keyBytes)
	default:
		return nil, fmt.Errorf("unsupported atproto public key type: %s", k.Type)
	}
}

// The home PDS endpoint for this account, if one is included in identity metadata (returns empty string if not found). The endpoint will be an HTTP URL with method, hostname, and optional port, but no path segments.
func (i *Identity) PDSEndpoint() string {
	if i.Services == nil {
		return ""
	}
	atp, ok := i.Services["atproto_pds"]
	if !ok {
		return ""
	}
	return atp.URL
}

// Returns an atproto handle from the alsoKnownAs URI list for this identifier. Returns an error if there is no handle, or if an at:// URI failes to parse as a handle.
//
// Note that this handle is *not* necessarily to be trusted, as it may not have been bi-directionally verified. The 'Handle' field on the 'Identity' should contain either a verified handle, or the special 'handle.invalid' indicator value.
func (i *Identity) DeclaredHandle() (syntax.Handle, error) {
	for _, u := range i.AlsoKnownAs {
		if strings.HasPrefix(u, "at://") && len(u) > len("at://") {
			return syntax.ParseHandle(u[5:])
		}
	}
	return "", fmt.Errorf("DID document contains no atproto handle")
}
