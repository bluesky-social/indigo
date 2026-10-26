package identity_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/vmihailenco/msgpack/v5"
)

const spaceAuthority = syntax.DID("did:plc:authority")
const authorityKey = "zQ3shXjHeiBuRCKmM36cuYnm7YEMzhGnCmCyW92sRJ9pribSF"

func authorityDocument() identity.DIDDocument {
	return identity.DIDDocument{
		DID: spaceAuthority,
		VerificationMethod: []identity.DocVerificationMethod{{
			ID: spaceAuthority.String() + "#atproto", Type: "Multikey",
			Controller: spaceAuthority.String(), PublicKeyMultibase: authorityKey,
		}},
		Service: []identity.DocService{{ID: "#atproto_pds", Type: "AtprotoPersonalDataServer", ServiceEndpoint: "https://pds.example"}},
	}
}

func kemDeclaration(t *testing.T) identity.DocVerificationMethod {
	t.Helper()
	private, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		t.Fatal(err)
	}
	public, err := private.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	return identity.DocVerificationMethod{
		ID: spaceAuthority.String() + "#atproto_kem", Type: "Multikey",
		Controller: spaceAuthority.String(), PublicKeyMultibase: public.Multibase(),
	}
}

func authorityRoundTrips(t *testing.T, ident identity.Identity) []identity.Identity {
	t.Helper()
	jsonBytes, err := json.Marshal(ident)
	if err != nil {
		t.Fatal(err)
	}
	var jsonIdent identity.Identity
	if err := json.Unmarshal(jsonBytes, &jsonIdent); err != nil {
		t.Fatal(err)
	}
	cacheBytes, err := msgpack.Marshal(ident)
	if err != nil {
		t.Fatal(err)
	}
	var cachedIdent identity.Identity
	if err := msgpack.Unmarshal(cacheBytes, &cachedIdent); err != nil {
		t.Fatal(err)
	}
	doc := ident.DIDDocument()
	return []identity.Identity{ident, jsonIdent, cachedIdent, identity.ParseIdentity(&doc)}
}

func TestKEMPublicKeySurvivesSerialization(t *testing.T) {
	doc := authorityDocument()
	vm := kemDeclaration(t)
	doc.VerificationMethod = append(doc.VerificationMethod, vm)
	for _, ident := range authorityRoundTrips(t, identity.ParseIdentity(&doc)) {
		key, err := ident.KEMPublicKey()
		if err != nil || key.Multibase() != vm.PublicKeyMultibase {
			t.Fatalf("dedicated KEM key: %v", err)
		}
		if account, err := ident.PublicKey(); err != nil || account.Multibase() != authorityKey {
			t.Fatalf("KEM key altered signing key: %v", err)
		}
		if ident.PDSEndpoint() != "https://pds.example" {
			t.Fatal("KEM key altered public PDS endpoint")
		}
	}
}

func TestKEMPublicKeyRequiresDedicatedDeclaration(t *testing.T) {
	doc := authorityDocument()
	for _, ident := range authorityRoundTrips(t, identity.ParseIdentity(&doc)) {
		if key, err := ident.KEMPublicKey(); !errors.Is(err, identity.ErrKeyNotDeclared) || key != nil {
			t.Fatalf("missing KEM declaration fell back to account key: %v", err)
		}
	}
	vm := kemDeclaration(t)
	constructed := identity.Identity{
		DID: spaceAuthority, Handle: syntax.HandleInvalid,
		Keys: map[string]identity.VerificationMethod{
			"atproto_kem": {Type: vm.Type, PublicKeyMultibase: vm.PublicKeyMultibase},
		},
	}
	for _, ident := range authorityRoundTrips(t, constructed) {
		if key, err := ident.KEMPublicKey(); !errors.Is(err, identity.ErrKeyNotDeclared) || key != nil {
			t.Fatalf("map without KEM declaration provenance became trusted: %v", err)
		}
	}
}

func TestKEMPublicKeyRejectsInvalidDeclarations(t *testing.T) {
	valid := kemDeclaration(t)
	cases := []struct {
		name   string
		mutate func(*identity.DocVerificationMethod)
	}{
		{"foreign-controller", func(vm *identity.DocVerificationMethod) { vm.Controller = "did:plc:other" }},
		{"missing-controller", func(vm *identity.DocVerificationMethod) { vm.Controller = "" }},
		{"relative-id", func(vm *identity.DocVerificationMethod) { vm.ID = "#atproto_kem" }},
		{"foreign-id", func(vm *identity.DocVerificationMethod) { vm.ID = "did:plc:other#atproto_kem" }},
		{"malformed-id", func(vm *identity.DocVerificationMethod) { vm.ID = "https://example.com#atproto_kem" }},
		{"bare-id", func(vm *identity.DocVerificationMethod) { vm.ID = "atproto_kem" }},
		{"unsupported-type", func(vm *identity.DocVerificationMethod) { vm.Type = "UnknownKey" }},
		{"legacy-p256-type", func(vm *identity.DocVerificationMethod) { vm.Type = "EcdsaSecp256r1VerificationKey2019" }},
		{"missing-type", func(vm *identity.DocVerificationMethod) { vm.Type = "" }},
		{"malformed-key", func(vm *identity.DocVerificationMethod) { vm.PublicKeyMultibase = "invalid" }},
		{"wrong-curve", func(vm *identity.DocVerificationMethod) { vm.PublicKeyMultibase = authorityKey }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := authorityDocument()
			vm := valid
			tc.mutate(&vm)
			doc.VerificationMethod = append(doc.VerificationMethod, vm)
			for _, ident := range authorityRoundTrips(t, identity.ParseIdentity(&doc)) {
				if key, err := ident.KEMPublicKey(); err == nil || key != nil {
					t.Fatal("invalid KEM declaration accepted")
				}
				if _, err := ident.PublicKey(); err != nil {
					t.Fatalf("public account key changed: %v", err)
				}
				if !reflect.DeepEqual(ident.KEMKeyDeclarations, []identity.DocVerificationMethod{vm}) {
					t.Fatal("malformed KEM declaration was lost in serialization")
				}
			}
		})
	}
	invalid := valid
	invalid.Controller = "did:plc:other"
	for _, declarations := range [][]identity.DocVerificationMethod{{valid, valid}, {valid, invalid}, {invalid, valid}} {
		doc := authorityDocument()
		doc.VerificationMethod = append(doc.VerificationMethod, declarations...)
		for _, ident := range authorityRoundTrips(t, identity.ParseIdentity(&doc)) {
			if _, err := ident.KEMPublicKey(); err == nil {
				t.Fatal("duplicate KEM declarations accepted")
			}
		}
	}
}

func TestKEMDeclarationSurvivesDirectoryCache(t *testing.T) {
	for _, valid := range []bool{false, true} {
		doc := authorityDocument()
		vm := kemDeclaration(t)
		if !valid {
			vm.Controller = "did:plc:other"
		}
		doc.VerificationMethod = append(doc.VerificationMethod, vm)
		inner := identity.NewMockDirectory()
		inner.Insert(identity.ParseIdentity(&doc))
		cached := identity.NewCacheDirectory(inner, 4, time.Hour, time.Minute, time.Hour)
		for range 2 {
			ident, err := cached.LookupDID(context.Background(), spaceAuthority)
			if err != nil {
				t.Fatal(err)
			}
			key, err := ident.KEMPublicKey()
			if valid {
				if err != nil || key.Multibase() != vm.PublicKeyMultibase {
					t.Fatalf("cached identity lost valid KEM declaration: %v", err)
				}
			} else if err == nil || key != nil {
				t.Fatal("cached identity trusted invalid KEM declaration")
			}
		}
	}
}

func TestKEMDeclarationRejectsInvalidIdentityDID(t *testing.T) {
	for _, did := range []syntax.DID{"", "not-a-did"} {
		vm := kemDeclaration(t)
		vm.ID = did.String() + "#atproto_kem"
		vm.Controller = did.String()
		doc := identity.DIDDocument{DID: did, VerificationMethod: []identity.DocVerificationMethod{vm}}
		ident := identity.ParseIdentity(&doc)
		if _, err := ident.KEMPublicKey(); err == nil {
			t.Fatal("KEM declaration accepted for invalid identity DID")
		}
	}
}
