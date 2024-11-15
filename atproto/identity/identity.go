package identity

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

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
type Directory interface {
	LookupHandle(ctx context.Context, h syntax.Handle) (*Identity, error)
	LookupDID(ctx context.Context, d syntax.DID) (*Identity, error)
	Lookup(ctx context.Context, i syntax.AtIdentifier) (*Identity, error)

	// Flushes any cache of the indicated identifier. If directory is not using caching, can ignore this.
	Purge(ctx context.Context, i syntax.AtIdentifier) error
}

// Indicates that handle resolution failed. A wrapped error may provide more context. This is only returned when looking up a handle, not when looking up a DID.
var ErrHandleResolutionFailed = errors.New("handle resolution failed")

// Indicates that resolution process completed successfully, but handle does not exist. This is only returned when looking up a handle, not when looking up a DID.
var ErrHandleNotFound = errors.New("handle not found")

// Indicates that resolution process completed successfully, handle mapped to a different DID. This is only returned when looking up a handle, not when looking up a DID.
var ErrHandleMismatch = errors.New("handle/DID mismatch")

// Indicates that DID document did not include any handle ("alsoKnownAs"). This is only returned when looking up a handle, not when looking up a DID.
var ErrHandleNotDeclared = errors.New("DID document did not declare a handle")

// Handle top-level domain (TLD) is one of the special "Reserved" suffixes, and not allowed for atproto use
var ErrHandleReservedTLD = errors.New("handle top-level domain is disallowed")

// Indicates that resolution process completed successfully, but the DID does not exist.
var ErrDIDNotFound = errors.New("DID not found")

// Indicates that DID resolution process failed. A wrapped error may provide more context.
var ErrDIDResolutionFailed = errors.New("DID resolution failed")

// Indicates that DID document did not include a public key with the specified ID
var ErrKeyNotDeclared = errors.New("DID document did not declare a relevant public key")

var DefaultPLCURL = "https://plc.directory"

// Returns a reasonable Directory implementation for applications
func DefaultDirectory() Directory {
	base := BaseDirectory{
		PLCURL: DefaultPLCURL,
		HTTPClient: http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				// would want this around 100ms for services doing lots of handle resolution. Impacts PLC connections as well, but not too bad.
				IdleConnTimeout: time.Millisecond * 1000,
				MaxIdleConns:    100,
			},
		},
		Resolver: net.Resolver{
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: time.Second * 5}
				return d.DialContext(ctx, network, address)
			},
		},
		TryAuthoritativeDNS: true,
		// primary Bluesky PDS instance only supports HTTP resolution method
		SkipDNSDomainSuffixes: []string{".bsky.social"},
	}
	cached := NewCacheDirectory(&base, 250_000, time.Hour*24, time.Minute*2, time.Minute*5)
	return &cached
}

// Represents an atproto identity. Could be a regular user account, or a service account (eg, feed generator)
type Identity struct {
	DID syntax.DID

	// Handle/DID mapping must be bi-directionally verified. If that fails, the Handle should be the special 'handle.invalid' value
	Handle syntax.Handle

	// These fields represent a parsed subset of a DID document. They are all nullable. Note that the services and keys maps do not preserve order, so they don't exactly round-trip DID documents.
	AlsoKnownAs []string
	Services    map[string]Service
	Keys        map[string]Key
}

type Key struct {
	Type               string
	PublicKeyMultibase string
}

type Service struct {
	Type string
	URL  string
}

// Extracts the information relevant to atproto from an arbitrary DID document.
//
// Always returns an invalid Handle field; calling code should only populate that field if it has been bi-directionally verified.
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

// Identifies and parses the atproto repo signing public key, specifically, out of any keys in this identity's DID document.
//
// Returns [ErrKeyNotFound] if there is no such key.
//
// Note that [crypto.PublicKey] is an interface, not a concrete type.
func (i *Identity) PublicKey() (crypto.PublicKey, error) {
	return i.GetPublicKey("atproto")
}

// Identifies and parses a specified service signing public key out of any keys in this identity's DID document.
//
// Returns [ErrKeyNotFound] if there is no such key.
//
// Note that [crypto.PublicKey] is an interface, not a concrete type.
func (i *Identity) GetPublicKey(id string) (crypto.PublicKey, error) {
	if i.Keys == nil {
		return nil, ErrKeyNotDeclared
	}
	k, ok := i.Keys[id]
	if !ok {
		return nil, ErrKeyNotDeclared
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

// The home PDS endpoint for this identity, if one is included in the DID document.
//
// The endpoint should be an HTTP URL with method, hostname, and optional port. It may or may not include path segments.
//
// Returns an empty string if the serivce isn't found, or if the URL fails to parse.
func (i *Identity) PDSEndpoint() string {
	return i.GetServiceEndpoint("atproto_pds")
}

// Returns the service endpoint URL for specified service ID (the fragment part of identifier, not including the hash symbol).
//
// The endpoint should be an HTTP URL with method, hostname, and optional port. It may or may not include path segments.
//
// Returns an empty string if the serivce isn't found, or if the URL fails to parse.
func (i *Identity) GetServiceEndpoint(id string) string {
	if i.Services == nil {
		return ""
	}
	endpoint, ok := i.Services[id]
	if !ok {
		return ""
	}
	_, err := url.Parse(endpoint.URL)
	if err != nil {
		return ""
	}
	return endpoint.URL
}

// Returns an atproto handle from the alsoKnownAs URI list for this identifier. Returns an error if there is no handle, or if an at:// URI fails to parse as a handle.
//
// Note that this handle is *not* necessarily to be trusted, as it may not have been bi-directionally verified. The 'Handle' field on the 'Identity' should contain either a verified handle, or the special 'handle.invalid' indicator value.
func (i *Identity) DeclaredHandle() (syntax.Handle, error) {
	for _, u := range i.AlsoKnownAs {
		if strings.HasPrefix(u, "at://") && len(u) > len("at://") {
			return syntax.ParseHandle(u[5:])
		}
	}
	return "", ErrHandleNotDeclared
}
