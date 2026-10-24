package identity_test

import (
	"context"
	"encoding/json"
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

func TestAbsentSpaceAuthorityFallsBack(t *testing.T) {
	doc := authorityDocument()
	for _, ident := range authorityRoundTrips(t, identity.ParseIdentity(&doc)) {
		key, err := ident.SpacePublicKey()
		if err != nil || key.Multibase() != authorityKey {
			t.Fatalf("absent key fallback: %v", err)
		}
		host, err := ident.SpaceHostEndpoint()
		if err != nil || host != "https://pds.example" {
			t.Fatalf("absent host fallback: %q, %v", host, err)
		}
	}
	empty := identity.Identity{DID: spaceAuthority}
	if _, err := empty.SpacePublicKey(); err == nil {
		t.Fatal("missing fallback key accepted")
	}
	if host, err := empty.SpaceHostEndpoint(); err == nil || host != "" {
		t.Fatal("missing fallback host accepted")
	}
}

func TestDedicatedSpaceAuthorityOverridesFallback(t *testing.T) {
	priv, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := priv.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"", spaceAuthority.String()} {
		doc := authorityDocument()
		doc.VerificationMethod = append(doc.VerificationMethod, identity.DocVerificationMethod{
			ID: prefix + "#atproto_space", Type: "Multikey", Controller: spaceAuthority.String(), PublicKeyMultibase: pub.Multibase(),
		})
		doc.Service = append(doc.Service, identity.DocService{
			ID: prefix + "#atproto_space_host", Type: "AtprotoSpaceHost", ServiceEndpoint: "https://space.example:8443/path",
		})
		for _, ident := range authorityRoundTrips(t, identity.ParseIdentity(&doc)) {
			key, err := ident.SpacePublicKey()
			if err != nil || !key.Equal(pub) {
				t.Fatalf("dedicated key: %v", err)
			}
			host, err := ident.SpaceHostEndpoint()
			if err != nil || host != "https://space.example:8443/path" {
				t.Fatalf("dedicated host: %q, %v", host, err)
			}
			if ident.PDSEndpoint() != "https://pds.example" {
				t.Fatal("dedicated host altered public PDS")
			}
		}
	}
}

func TestInvalidDedicatedSpaceKeysNeverFallBack(t *testing.T) {
	valid := identity.DocVerificationMethod{
		ID: "#atproto_space", Type: "Multikey", Controller: spaceAuthority.String(), PublicKeyMultibase: authorityKey,
	}
	cases := []struct {
		name   string
		mutate func(*identity.DocVerificationMethod)
	}{
		{"foreign-controller", func(vm *identity.DocVerificationMethod) { vm.Controller = "did:plc:other" }},
		{"missing-controller", func(vm *identity.DocVerificationMethod) { vm.Controller = "" }},
		{"foreign-id", func(vm *identity.DocVerificationMethod) { vm.ID = "did:plc:other#atproto_space" }},
		{"malformed-id", func(vm *identity.DocVerificationMethod) { vm.ID = "https://example.com#atproto_space" }},
		{"bare-id", func(vm *identity.DocVerificationMethod) { vm.ID = "atproto_space" }},
		{"unsupported-type", func(vm *identity.DocVerificationMethod) { vm.Type = "UnknownKey" }},
		{"missing-type", func(vm *identity.DocVerificationMethod) { vm.Type = "" }},
		{"malformed-key", func(vm *identity.DocVerificationMethod) { vm.PublicKeyMultibase = "invalid" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := authorityDocument()
			vm := valid
			tc.mutate(&vm)
			doc.VerificationMethod = append(doc.VerificationMethod, vm)
			for _, ident := range authorityRoundTrips(t, identity.ParseIdentity(&doc)) {
				if key, err := ident.SpacePublicKey(); err == nil || key != nil {
					t.Fatal("invalid dedicated key fell back to account key")
				}
				if _, err := ident.PublicKey(); err != nil {
					t.Fatalf("public account key changed: %v", err)
				}
				if !reflect.DeepEqual(ident.SpaceKeyDeclarations, []identity.DocVerificationMethod{vm}) {
					t.Fatal("malformed key declaration was lost in serialization")
				}
			}
		})
	}
	for _, firstInvalid := range []bool{false, true} {
		doc := authorityDocument()
		invalid := valid
		invalid.Controller = "did:plc:other"
		declarations := []identity.DocVerificationMethod{valid, invalid}
		if firstInvalid {
			declarations[0], declarations[1] = declarations[1], declarations[0]
		}
		doc.VerificationMethod = append(doc.VerificationMethod, declarations...)
		for _, ident := range authorityRoundTrips(t, identity.ParseIdentity(&doc)) {
			if _, err := ident.SpacePublicKey(); err == nil {
				t.Fatal("duplicate dedicated keys accepted")
			}
		}
	}
}

func TestInvalidDedicatedSpaceHostsNeverFallBack(t *testing.T) {
	valid := identity.DocService{ID: "#atproto_space_host", Type: "AtprotoSpaceHost", ServiceEndpoint: "https://space.example"}
	cases := []identity.DocService{
		{ID: "did:plc:other#atproto_space_host", Type: valid.Type, ServiceEndpoint: valid.ServiceEndpoint},
		{ID: "https://example.com#atproto_space_host", Type: valid.Type, ServiceEndpoint: valid.ServiceEndpoint},
		{ID: "atproto_space_host", Type: valid.Type, ServiceEndpoint: valid.ServiceEndpoint},
		{ID: valid.ID, Type: "AtprotoPersonalDataServer", ServiceEndpoint: valid.ServiceEndpoint},
		{ID: valid.ID, Type: "", ServiceEndpoint: valid.ServiceEndpoint},
	}
	for _, endpoint := range []string{"", "/relative", "space.example", "//space.example", "ftp://space.example", "https:///path", "https://:8443", "https://space.example:invalid", "http:opaque", "https://space.example/%ZZ"} {
		cases = append(cases, identity.DocService{ID: valid.ID, Type: valid.Type, ServiceEndpoint: endpoint})
	}
	for _, svc := range cases {
		t.Run(svc.ID+"-"+svc.Type+"-"+svc.ServiceEndpoint, func(t *testing.T) {
			doc := authorityDocument()
			doc.Service = append(doc.Service, svc)
			for _, ident := range authorityRoundTrips(t, identity.ParseIdentity(&doc)) {
				if host, err := ident.SpaceHostEndpoint(); err == nil || host != "" {
					t.Fatalf("invalid dedicated host fell back: %q", host)
				}
				if ident.PDSEndpoint() != "https://pds.example" {
					t.Fatal("invalid dedicated host altered public PDS")
				}
				if !reflect.DeepEqual(ident.SpaceHostDeclarations, []identity.DocService{svc}) {
					t.Fatal("malformed host declaration was lost in serialization")
				}
			}
		})
	}
	doc := authorityDocument()
	doc.Service = append(doc.Service, valid, valid)
	for _, ident := range authorityRoundTrips(t, identity.ParseIdentity(&doc)) {
		if _, err := ident.SpaceHostEndpoint(); err == nil {
			t.Fatal("duplicate dedicated hosts accepted")
		}
	}
}

func TestSpaceAuthorityValidationForConstructedIdentities(t *testing.T) {
	ident := identity.Identity{
		DID: spaceAuthority,
		Keys: map[string]identity.VerificationMethod{
			"atproto":       {Type: "Multikey", PublicKeyMultibase: authorityKey},
			"atproto_space": {Type: "UnknownKey", PublicKeyMultibase: authorityKey},
		},
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds":        {Type: "PublicType", URL: "/historically-permitted-relative-pds"},
			"atproto_space_host": {Type: "AtprotoSpaceHost", URL: "/relative-space-host"},
		},
	}
	if _, err := ident.SpacePublicKey(); err == nil {
		t.Fatal("constructed invalid dedicated key fell back")
	}
	if _, err := ident.SpaceHostEndpoint(); err == nil {
		t.Fatal("constructed invalid dedicated host fell back")
	}
	if ident.PDSEndpoint() != "/historically-permitted-relative-pds" {
		t.Fatal("unrelated public PDS parsing became strict")
	}
	ident.Services["atproto_space_host"] = identity.ServiceEndpoint{Type: "AtprotoSpaceHost", URL: "http://space.example"}
	if host, err := ident.SpaceHostEndpoint(); err != nil || host != "http://space.example" {
		t.Fatalf("absolute HTTP dedicated host rejected: %q, %v", host, err)
	}
}

func TestInvalidSpaceAuthoritySurvivesDirectoryCache(t *testing.T) {
	doc := authorityDocument()
	doc.VerificationMethod = append(doc.VerificationMethod, identity.DocVerificationMethod{
		ID: "#atproto_space", Type: "Multikey", Controller: "did:plc:other", PublicKeyMultibase: authorityKey,
	})
	doc.Service = append(doc.Service, identity.DocService{ID: "did:plc:other#atproto_space_host", Type: "AtprotoSpaceHost", ServiceEndpoint: "https://space.example"})
	inner := identity.NewMockDirectory()
	inner.Insert(identity.ParseIdentity(&doc))
	cached := identity.NewCacheDirectory(inner, 4, time.Hour, time.Minute, time.Hour)
	for range 2 {
		ident, err := cached.LookupDID(context.Background(), spaceAuthority)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ident.SpacePublicKey(); err == nil {
			t.Fatal("cached identity lost invalid dedicated key presence")
		}
		if _, err := ident.SpaceHostEndpoint(); err == nil {
			t.Fatal("cached identity lost invalid dedicated host presence")
		}
	}
}

func TestSpaceDeclarationsRejectInvalidIdentityDID(t *testing.T) {
	for _, did := range []syntax.DID{"", "not-a-did"} {
		doc := identity.DIDDocument{
			DID:                did,
			VerificationMethod: []identity.DocVerificationMethod{{ID: "#atproto_space", Type: "Multikey", Controller: did.String(), PublicKeyMultibase: authorityKey}},
			Service:            []identity.DocService{{ID: "#atproto_space_host", Type: "AtprotoSpaceHost", ServiceEndpoint: "https://space.example"}},
		}
		ident := identity.ParseIdentity(&doc)
		if _, err := ident.SpacePublicKey(); err == nil {
			t.Fatal("dedicated key accepted for invalid identity DID")
		}
		if _, err := ident.SpaceHostEndpoint(); err == nil {
			t.Fatal("dedicated host accepted for invalid identity DID")
		}
	}
}
