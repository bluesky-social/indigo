package identity

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/mr-tron/base58"
)

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

// Helper to generate a DID document based on an identity. Note that there is flexibility around parsing, and this won't necessarily "round-trip" for every valid DID document.
func (ident *Identity) DIDDocument() DIDDocument {
	doc := DIDDocument{
		DID:                ident.DID,
		AlsoKnownAs:        ident.AlsoKnownAs,
		VerificationMethod: make([]DocVerificationMethod, len(ident.Keys)),
		Service:            make([]DocService, len(ident.Services)),
	}
	i := 0
	for k, key := range ident.Keys {
		doc.VerificationMethod[i] = DocVerificationMethod{
			ID:                 fmt.Sprintf("%s#%s", ident.DID, k),
			Type:               key.Type,
			Controller:         ident.DID.String(),
			PublicKeyMultibase: key.PublicKeyMultibase,
		}
		i += 1
	}
	i = 0
	for k, svc := range ident.Services {
		doc.Service[i] = DocService{
			ID:              fmt.Sprintf("#%s", k),
			Type:            svc.Type,
			ServiceEndpoint: svc.URL,
		}
		i += 1
	}
	return doc
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
// Returns an empty string if the service isn't found, or if the URL fails to parse.
func (i *Identity) PDSEndpoint() string {
	return i.GetServiceEndpoint("atproto_pds")
}

// Returns the service endpoint URL for specified service ID (the fragment part of identifier, not including the hash symbol).
//
// The endpoint should be an HTTP URL with method, hostname, and optional port. It may or may not include path segments.
//
// Returns an empty string if the service isn't found, or if the URL fails to parse.
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
