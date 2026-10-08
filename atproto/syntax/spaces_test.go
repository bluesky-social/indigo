package syntax_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

const testSpaceRef = "at://did:plc:authority/space/com.example.forum/default"
const testSpaceURI = testSpaceRef + "/did:plc:author/org.other.post/record"

func TestSpaceAddressComponentsAndText(t *testing.T) {
	ref, err := syntax.ParseSpaceRef(testSpaceRef)
	if err != nil {
		t.Fatal(err)
	}
	if ref.SpaceAuthority() != "did:plc:authority" || ref.SpaceType() != "com.example.forum" || ref.SpaceKey() != "default" {
		t.Fatalf("wrong space components: %q", ref)
	}
	if syntax.NewSpaceRef(ref.SpaceAuthority(), ref.SpaceType(), ref.SpaceKey()) != ref {
		t.Fatal("space constructor changed reference")
	}
	uri, err := syntax.ParseSpaceURI(testSpaceURI)
	if err != nil {
		t.Fatal(err)
	}
	if uri.SpaceRef() != ref || uri.SpaceAuthority() != ref.SpaceAuthority() || uri.SpaceType() != ref.SpaceType() || uri.SpaceKey() != ref.SpaceKey() || uri.Repo() != "did:plc:author" || uri.Collection() != "org.other.post" || uri.RecordKey() != "record" {
		t.Fatalf("wrong record components: %q", uri)
	}
	if syntax.NewSpaceURI(ref, uri.Repo(), uri.Collection(), uri.RecordKey()) != uri {
		t.Fatal("record constructor changed address")
	}
	refText, _ := ref.MarshalText()
	uriText, _ := uri.MarshalText()
	var decodedRef syntax.SpaceRef
	var decodedURI syntax.SpaceURI
	if err := decodedRef.UnmarshalText(refText); err != nil || decodedRef != ref {
		t.Fatalf("reference text roundtrip: %q, %v", decodedRef, err)
	}
	if err := decodedURI.UnmarshalText(uriText); err != nil || decodedURI != uri {
		t.Fatalf("record text roundtrip: %q, %v", decodedURI, err)
	}
	wire, err := json.Marshal(struct {
		Ref syntax.SpaceRef `json:"ref"`
		URI syntax.SpaceURI `json:"uri"`
	}{ref, uri})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Ref syntax.SpaceRef `json:"ref"`
		URI syntax.SpaceURI `json:"uri"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil || decoded.Ref != ref || decoded.URI != uri {
		t.Fatalf("JSON text codecs: %s, %v", wire, err)
	}
	if err := decodedRef.UnmarshalText([]byte("https://example.com")); err == nil || decodedRef != ref {
		t.Fatal("invalid reference text mutated destination")
	}
	if err := decodedURI.UnmarshalText(refText); err == nil || decodedURI != uri {
		t.Fatal("incomplete URI text mutated destination")
	}
}

func TestSpaceRefRejectsMalformedAndRecordAddresses(t *testing.T) {
	invalid := []string{
		"", "https://example.com", "at://did:plc:authority", "at://did:plc:authority/space",
		"at://example.com/space/com.example.forum/default",
		"at://did:plc:authority/space/com.example/default",
		"at://did:plc:authority/space/com.example.forum/.",
		"at://did:plc:authority/space/com.example.forum/..",
		"at://did:plc:authority/space/com.example.forum/" + strings.Repeat("a", 513),
		testSpaceRef + "/", testSpaceRef + "?query=true", testSpaceRef + "#fragment",
		testSpaceRef + "/did:plc:author", testSpaceURI,
		strings.Replace(testSpaceRef, "/space/", "/SPACE/", 1),
	}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			if ref, err := syntax.ParseSpaceRef(raw); err == nil || ref != "" {
				t.Fatalf("accepted malformed space reference %q", raw)
			}
		})
	}
}

func TestSpaceURIRequiresEveryValidatedRecordComponent(t *testing.T) {
	invalid := []string{
		"", testSpaceRef, testSpaceRef + "/did:plc:author", testSpaceRef + "/did:plc:author/org.other.post",
		testSpaceRef + "/did:plc:author/org.other.post/", testSpaceURI + "/extra",
		strings.Replace(testSpaceURI, "did:plc:authority", "authority.example", 1),
		strings.Replace(testSpaceURI, "did:plc:author/", "author.example/", 1),
		strings.Replace(testSpaceURI, "did:plc:author/", "did:PLC:author/", 1),
		strings.Replace(testSpaceURI, "org.other.post", "not-an-nsid", 1),
		strings.Replace(testSpaceURI, "com.example.forum", "not-an-nsid", 1),
		strings.Replace(testSpaceURI, "/default/", "/../", 1),
		strings.Replace(testSpaceURI, "/record", "/..", 1),
		strings.Replace(testSpaceURI, "/record", "/"+strings.Repeat("a", 513), 1),
		testSpaceURI + "?query=true", testSpaceURI + "#fragment",
	}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			if uri, err := syntax.ParseSpaceURI(raw); err == nil || uri != "" {
				t.Fatalf("accepted malformed record address %q", raw)
			}
		})
	}
	for _, raw := range []string{"at://did:plc:author", "at://author.example/org.other.post/record"} {
		if _, err := syntax.ParseATURI(raw); err != nil {
			t.Fatalf("public ATURI changed: %s: %v", raw, err)
		}
	}
	if _, err := syntax.ParseATURI(testSpaceURI); err == nil {
		t.Fatal("public ATURI parser accepted a Spaces URI")
	}
}
