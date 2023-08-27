package identity

import (
	"context"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/mr-tron/base58"
)

// API for doing account lookups by DID or handle, with bi-directional verification handled automatically.
//
// Handles which fail to resolve or don't match DID alsoKnownAs are an error. DIDs which resolve but the handle does not resolve back to the DID return an Identity where the Handle is the special `handle.invalid` value.
//
// Some example implementations of this interface would be:
//   - naive direct resolution on every call
//   - API client, which just makes requests to PDS (or other remote service)
//   - simple in-memory caching wrapper layer to reduce network hits
//   - services with backing datastore to do sophisticated caching, TTL, auto-refresh, etc
type Catalog interface {
	LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error)
	LookupDID(ctx context.Context, d syntax.DID) (*Identity, error)
	Lookup(ctx context.Context, i syntax.AtIdentifier) (*Identity, error)
	// TODO: add "flush" methods to purge caches?
}

var DefaultPLCURL = "https://plc.directory"

func DefaultCatalog() Catalog {
	cat := NewCacheCatalog(DefaultPLCURL)
	return &cat
}

// TODO: DIDHistory() helper, returns log of declared handle, PDS location, and public key? or maybe this is something best left to did-method-plc helper library

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

	// If a valid atproto repo signing public key was parsed, it can be cached here. This is nullable/optional.
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

func (i *Identity) DeclaredHandle() (syntax.Handle, error) {
	for _, u := range i.AlsoKnownAs {
		if strings.HasPrefix(u, "at://") && len(u) > len("at://") {
			return syntax.ParseHandle(u[5:])
		}
	}
	return "", fmt.Errorf("DID document contains no atproto handle")
}
