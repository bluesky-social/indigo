package identity

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
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
	Services    map[string]ServiceEndpoint
	Keys        map[string]VerificationMethod

	// Preserve dedicated declarations before lossy DID-document parsing. These
	// exported fields also survive identity serialization and directory caches.
	SpaceKeyDeclarations  []DocVerificationMethod `json:",omitempty"`
	SpaceHostDeclarations []DocService            `json:",omitempty"`
}

// Sub-field type for [Identity], representing a cryptographic public key declared as a "verificationMethod" in the DID document.
type VerificationMethod struct {
	Type               string
	PublicKeyMultibase string
}

// Sub-field type for [Identity], representing a service endpoint URL declared in the DID document.
type ServiceEndpoint struct {
	Type string
	URL  string
}

// Extracts the information relevant to atproto from an arbitrary DID document.
//
// Always returns an invalid Handle field; calling code should only populate that field if it has been bi-directionally verified.
func ParseIdentity(doc *DIDDocument) Identity {
	var spaceKeys []DocVerificationMethod
	var spaceHosts []DocService
	keys := make(map[string]VerificationMethod, len(doc.VerificationMethod))
	for _, vm := range doc.VerificationMethod {
		if isSpaceDeclaration(vm.ID, "atproto_space") {
			spaceKeys = append(spaceKeys, vm)
		}
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
		keys[parts[1]] = VerificationMethod{
			Type:               vm.Type,
			PublicKeyMultibase: vm.PublicKeyMultibase,
		}
	}
	svc := make(map[string]ServiceEndpoint, len(doc.Service))
	for _, s := range doc.Service {
		if isSpaceDeclaration(s.ID, "atproto_space_host") {
			spaceHosts = append(spaceHosts, s)
		}
		parts := strings.SplitN(s.ID, "#", 2)
		if len(parts) < 2 {
			continue
		}
		// don't want to clobber existing entries with same ID fragment
		if _, ok := svc[parts[1]]; ok {
			continue
		}
		// TODO: verify that ID and type match for atproto-specific services?
		svc[parts[1]] = ServiceEndpoint{
			Type: s.Type,
			URL:  s.ServiceEndpoint,
		}
	}
	return Identity{
		DID:                   doc.DID,
		Handle:                syntax.HandleInvalid,
		AlsoKnownAs:           doc.AlsoKnownAs,
		Services:              svc,
		Keys:                  keys,
		SpaceKeyDeclarations:  spaceKeys,
		SpaceHostDeclarations: spaceHosts,
	}
}

// Helper to generate a DID document based on an identity. Note that there is flexibility around parsing, and this won't necessarily "round-trip" for every valid DID document.
func (ident *Identity) DIDDocument() DIDDocument {
	doc := DIDDocument{
		DID:                ident.DID,
		AlsoKnownAs:        ident.AlsoKnownAs,
		VerificationMethod: make([]DocVerificationMethod, 0, len(ident.Keys)+len(ident.SpaceKeyDeclarations)),
		Service:            make([]DocService, 0, len(ident.Services)+len(ident.SpaceHostDeclarations)),
	}
	for k, key := range ident.Keys {
		if k == "atproto_space" && len(ident.SpaceKeyDeclarations) > 0 {
			continue
		}
		doc.VerificationMethod = append(doc.VerificationMethod, DocVerificationMethod{
			ID:                 fmt.Sprintf("%s#%s", ident.DID, k),
			Type:               key.Type,
			Controller:         ident.DID.String(),
			PublicKeyMultibase: key.PublicKeyMultibase,
		})
	}
	for k, svc := range ident.Services {
		if k == "atproto_space_host" && len(ident.SpaceHostDeclarations) > 0 {
			continue
		}
		doc.Service = append(doc.Service, DocService{
			ID:              fmt.Sprintf("#%s", k),
			Type:            svc.Type,
			ServiceEndpoint: svc.URL,
		})
	}
	doc.VerificationMethod = append(doc.VerificationMethod, ident.SpaceKeyDeclarations...)
	doc.Service = append(doc.Service, ident.SpaceHostDeclarations...)
	return doc
}

// Identifies and parses the atproto repo signing public key, specifically, out of any keys in this identity's DID document.
//
// Returns [ErrKeyNotFound] if there is no such key.
//
// Note that [atcrypto.PublicKey] is an interface, not a concrete type.
func (i *Identity) PublicKey() (atcrypto.PublicKey, error) {
	return i.GetPublicKey("atproto")
}

// Identifies and parses a specified service signing public key out of any keys in this identity's DID document.
//
// Returns [ErrKeyNotFound] if there is no such key.
//
// Note that [atcrypto.PublicKey] is an interface, not a concrete type.
func (i *Identity) GetPublicKey(id string) (atcrypto.PublicKey, error) {
	if i.Keys == nil {
		return nil, ErrKeyNotDeclared
	}
	k, ok := i.Keys[id]
	if !ok {
		return nil, ErrKeyNotDeclared
	}
	return parsePublicKey(k)
}

func parsePublicKey(k VerificationMethod) (atcrypto.PublicKey, error) {
	switch k.Type {
	case "Multikey":
		return atcrypto.ParsePublicMultibase(k.PublicKeyMultibase)
	case "EcdsaSecp256r1VerificationKey2019":
		if len(k.PublicKeyMultibase) < 2 || k.PublicKeyMultibase[0] != 'z' {
			return nil, fmt.Errorf("identity key not a multibase base58btc string")
		}
		keyBytes, err := base58.Decode(k.PublicKeyMultibase[1:])
		if err != nil {
			return nil, fmt.Errorf("identity key multibase parsing: %w", err)
		}
		return atcrypto.ParsePublicUncompressedBytesP256(keyBytes)
	case "EcdsaSecp256k1VerificationKey2019":
		if len(k.PublicKeyMultibase) < 2 || k.PublicKeyMultibase[0] != 'z' {
			return nil, fmt.Errorf("identity key not a multibase base58btc string")
		}
		keyBytes, err := base58.Decode(k.PublicKeyMultibase[1:])
		if err != nil {
			return nil, fmt.Errorf("identity key multibase parsing: %w", err)
		}
		return atcrypto.ParsePublicUncompressedBytesK256(keyBytes)
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
			hdl, err := syntax.ParseHandle(u[5:])
			if err != nil {
				continue
			}
			return hdl.Normalize(), nil
		}
	}
	return "", ErrHandleNotDeclared
}
