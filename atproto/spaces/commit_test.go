package spaces

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	cbg "github.com/whyrusleeping/cbor-gen"
)

const testSpace syntax.SpaceRef = "at://did:web:authority.example/space/com.example.space/skey"
const testAuthor syntax.DID = "did:web:author.example"
const testRevision syntax.TID = "3mtwwsccuqy22"

// The reference constructs the specified UTF-8 TLS vectors independently of
// commitContext. Static values below were calculated with Python hashlib/hmac.
func referenceContext(space syntax.SpaceRef, author syntax.DID, revision syntax.TID) []byte {
	var out bytes.Buffer
	out.WriteString("atproto-space-repo-v1")
	for _, field := range []string{string(space), string(author), string(revision)} {
		_ = binary.Write(&out, binary.BigEndian, uint16(len(field)))
		out.WriteString(field)
	}
	return out.Bytes()
}

func referenceMAC(secret [32]byte, context []byte, digest [32]byte) [32]byte {
	mac := hmac.New(sha256.New, secret[:])
	mac.Write(context)
	mac.Write(digest[:])
	var result [32]byte
	copy(result[:], mac.Sum(nil))
	return result
}

func independentCommit(t *testing.T, digest [32]byte) (*Commit, *MACVerification) {
	t.Helper()
	c := &Commit{Version: 1, Repo: testAuthor, Space: testSpace, Revision: testRevision, Data: digest}
	authentication := &MACVerification{}
	for i := range authentication.SharedSecret {
		authentication.SharedSecret[i] = byte(i)
	}
	authentication.MAC = referenceMAC(authentication.SharedSecret, referenceContext(c.Space, c.Repo, c.Revision), digest)
	return c, authentication
}

func TestCommitIndependentContextAndHMACVector(t *testing.T) {
	c, authentication := independentCommit(t, sha256.Sum256([]byte("independent repository digest")))
	const expectedContext = "617470726f746f2d73706163652d7265706f2d7631003b61743a2f2f6469643a7765623a617574686f726974792e6578616d706c652f73706163652f636f6d2e6578616d706c652e73706163652f736b657900166469643a7765623a617574686f722e6578616d706c65000d336d7477777363637571793232"
	if hex.EncodeToString(commitContext(testSpace, testAuthor, testRevision)) != expectedContext {
		t.Fatal("context differs from independent length-prefixed fixture")
	}
	if hex.EncodeToString(c.Data[:]) != "bca5ec2792b2994ae9352364fc276c4a9a38339c1c91824065360464e47a8cc3" {
		t.Fatal("independent digest fixture differs")
	}
	mac, err := c.ComputeMAC(authentication.SharedSecret)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(mac[:]) != "c4cd57f06a625d4ad29d4e2e7cdcb032746490584cfc70dd931c850df632ab1e" || mac != authentication.MAC {
		t.Fatal("MAC differs from independent shared-secret vector")
	}
	if err := c.Verify(testSpace, testAuthor, authentication); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(syntax.SpaceRef(string(testSpace)+"2"), testAuthor, authentication); err == nil {
		t.Fatal("accepted another space")
	}
	if err := c.Verify(testSpace, "did:web:other.example", authentication); err == nil {
		t.Fatal("accepted another author")
	}
	if err := c.Verify(testSpace, testAuthor, nil); err == nil {
		t.Fatal("accepted missing response authentication")
	}
	for _, field := range []string{"data", "repo", "space", "rev", "ver", "secret", "mac"} {
		t.Run(field, func(t *testing.T) {
			changed, auth := *c, *authentication
			switch field {
			case "data":
				changed.Data[0] ^= 1
			case "repo":
				changed.Repo = "did:web:other.example"
			case "space":
				changed.Space += "2"
			case "rev":
				changed.Revision = "3mtwwsccuqy23"
			case "ver":
				changed.Version = 2
			case "secret":
				auth.SharedSecret[0] ^= 1
			case "mac":
				auth.MAC[0] ^= 1
			}
			if err := changed.Verify(testSpace, testAuthor, &auth); err == nil {
				t.Fatal("accepted changed commit or authentication")
			}
		})
	}
}

func independentCommitJSON(t *testing.T, c *Commit) map[string]any {
	t.Helper()
	return map[string]any{"ver": c.Version, "repo": string(c.Repo), "space": string(c.Space), "rev": string(c.Revision), "data": map[string]string{"$bytes": base64.RawStdEncoding.EncodeToString(c.Data[:])}}
}

func independentCommitCBOR(t *testing.T, c *Commit, order []string) []byte {
	t.Helper()
	return independentCommitCBORType(t, c, order, "com.atproto.space.defs#commit")
}

func independentCommitCBORType(t *testing.T, c *Commit, order []string, typeName string) []byte {
	t.Helper()
	var out bytes.Buffer
	cw := cbg.NewCborWriter(&out)
	if err := cw.WriteMajorTypeHeader(cbg.MajMap, uint64(len(order))); err != nil {
		t.Fatal(err)
	}
	for _, field := range order {
		if err := testCBORText(cw, field); err != nil {
			t.Fatal(err)
		}
		var err error
		switch field {
		case "data":
			err = cbg.WriteByteArray(cw, c.Data[:])
		case "rev":
			err = testCBORText(cw, string(c.Revision))
		case "ver":
			err = cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(c.Version))
		case "repo":
			err = testCBORText(cw, string(c.Repo))
		case "space":
			err = testCBORText(cw, string(c.Space))
		case "$type":
			err = testCBORText(cw, typeName)
		case "sig":
			err = cbg.WriteByteArray(cw, make([]byte, 64))
		default:
			err = cbg.WriteByteArray(cw, make([]byte, 32))
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}

func TestCommitIndependentWireCodecs(t *testing.T) {
	c, authentication := independentCommit(t, sha256.Sum256([]byte("wire fixture")))
	for _, withType := range []bool{false, true} {
		obj := independentCommitJSON(t, c)
		if withType {
			obj["$type"] = "com.atproto.space.defs#commit"
		}
		fixture, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeCommitJSON(fixture)
		if err != nil || *decoded != *c {
			t.Fatal("independent JSON commit differs", err)
		}
		if err := decoded.Verify(testSpace, testAuthor, authentication); err != nil {
			t.Fatal(err)
		}
	}
	for _, order := range [][]string{{"rev", "ver", "data", "repo", "space"}, {"rev", "ver", "data", "repo", "$type", "space"}} {
		fixture := independentCommitCBOR(t, c, order)
		decoded, err := DecodeCommitCBOR(fixture)
		if err != nil || *decoded != *c {
			t.Fatal("independent CBOR commit differs", err)
		}
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var jsonObj map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &jsonObj); err != nil || len(jsonObj) != 5 || !bytes.Contains(jsonObj["data"], []byte(`"$bytes"`)) {
		t.Fatal("JSON commit is not the current five-field wire format", err)
	}
	var out bytes.Buffer
	if err := c.MarshalCBOR(&out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), independentCommitCBOR(t, c, []string{"rev", "ver", "data", "repo", "space"})) {
		t.Fatal("CBOR commit differs from independently encoded fixture")
	}
}

func TestCommitMalformedJSON(t *testing.T) {
	c, _ := independentCommit(t, [32]byte{})
	for _, field := range []string{"ver", "repo", "space", "rev", "data"} {
		for _, value := range []any{nil, true, []int{1}, map[string]string{"bad": "value"}} {
			obj := independentCommitJSON(t, c)
			obj[field] = value
			data, _ := json.Marshal(obj)
			if _, err := DecodeCommitJSON(data); err == nil {
				t.Fatalf("accepted malformed %s", field)
			}
		}
		obj := independentCommitJSON(t, c)
		delete(obj, field)
		data, _ := json.Marshal(obj)
		if _, err := DecodeCommitJSON(data); err == nil {
			t.Fatalf("accepted missing %s", field)
		}
	}
	for _, size := range []int{0, 31, 33, 64} {
		obj := independentCommitJSON(t, c)
		obj["data"] = map[string]string{"$bytes": base64.RawStdEncoding.EncodeToString(make([]byte, size))}
		data, _ := json.Marshal(obj)
		if _, err := DecodeCommitJSON(data); err == nil {
			t.Fatalf("accepted data length %d", size)
		}
	}
	for _, change := range []func(map[string]any){
		func(o map[string]any) { o["ver"] = 2 },
		func(o map[string]any) { o["rev"] = "bad" },
		func(o map[string]any) { o["repo"] = "author" },
		func(o map[string]any) { o["space"] = string(testSpace) + "/extra" },
		func(o map[string]any) { o["$type"] = "com.atproto.space.defs#signedCommit" },
		func(o map[string]any) { o["$type"] = "atproto-space-repo" },
		func(o map[string]any) { o["data"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" },
		func(o map[string]any) { o["data"] = map[string]any{"$bytes": strings.Repeat("A", 43), "extra": true} },
		func(o map[string]any) { o["data"] = map[string]string{"$bytes": strings.Repeat("A", 43) + "="} },
	} {
		obj := independentCommitJSON(t, c)
		change(obj)
		data, _ := json.Marshal(obj)
		if _, err := DecodeCommitJSON(data); err == nil {
			t.Fatal("accepted malformed commit JSON")
		}
	}
	for _, legacy := range []string{"hash", "ikm", "mac", "sig"} {
		obj := independentCommitJSON(t, c)
		obj[legacy] = map[string]string{"$bytes": strings.Repeat("A", 43)}
		data, _ := json.Marshal(obj)
		if _, err := DecodeCommitJSON(data); err == nil {
			t.Fatalf("accepted obsolete field %s", legacy)
		}
	}
	fixture, _ := json.Marshal(independentCommitJSON(t, c))
	for _, data := range [][]byte{
		append(bytes.Clone(fixture), []byte(" true")...),
		[]byte(strings.Replace(string(fixture), `"ver":1`, `"ver":1.0`, 1)),
		[]byte(strings.Replace(string(fixture), `"ver":1`, `"ver":1,"ver":1`, 1)),
		[]byte(strings.Replace(string(fixture), `"$bytes":`, `"$bytes":"ignored","$bytes":`, 1)),
		[]byte(`{"ver":1,"rev":"3mtwwsccuqy22","hash":{"$bytes":"` + strings.Repeat("A", 43) + `"},"ikm":{"$bytes":"` + strings.Repeat("A", 43) + `"},"mac":{"$bytes":"` + strings.Repeat("A", 43) + `"},"sig":{"$bytes":"` + strings.Repeat("A", 86) + `"}}`),
	} {
		if _, err := DecodeCommitJSON(data); err == nil {
			t.Fatal("accepted obsolete, trailing, fractional, or duplicate fields")
		}
	}
	original := *c
	if err := c.UnmarshalJSON([]byte(`{"ver":2}`)); err == nil || *c != original {
		t.Fatal("failed JSON decode mutated receiver or accepted malformed input")
	}
}

func TestCommitMalformedCBOR(t *testing.T) {
	c, _ := independentCommit(t, [32]byte{})
	order := []string{"rev", "ver", "data", "repo", "space"}
	fixture := independentCommitCBOR(t, c, order)
	bad := [][]byte{
		independentCommitCBOR(t, c, []string{"ver", "rev", "data", "repo", "space"}),
		independentCommitCBOR(t, c, []string{"rev", "ver", "data", "repo", "repo"}),
		independentCommitCBOR(t, c, []string{"ikm", "mac", "rev", "sig", "ver", "hash"}),
		independentCommitCBORType(t, c, []string{"rev", "ver", "data", "repo", "$type", "space"}, "com.atproto.space.defs#signedCommit"),
		append(bytes.Clone(fixture), 0),
		append([]byte{0xb8, 5}, fixture[1:]...),
		bytes.Replace(fixture, []byte{0x63, 'v', 'e', 'r', 0x01}, []byte{0x63, 'v', 'e', 'r', 0x18, 0x01}, 1),
		bytes.Replace(fixture, []byte{0x58, 0x20}, []byte{0x78, 0x20}, 1),
		bytes.Replace(fixture, []byte{0x58, 0x20}, []byte{0x58, 0x1f}, 1),
	}
	for _, field := range []string{"version", "revision", "repo", "space"} {
		changed := *c
		switch field {
		case "version":
			changed.Version = 2
		case "revision":
			changed.Revision = "bad"
		case "repo":
			changed.Repo = "invalid"
		case "space":
			changed.Space = "invalid"
		}
		bad = append(bad, independentCommitCBOR(t, &changed, order))
	}
	for i, data := range bad {
		if _, err := DecodeCommitCBOR(data); err == nil {
			t.Fatalf("accepted malformed CBOR case %d", i)
		}
	}
	for cut := range len(fixture) {
		if _, err := DecodeCommitCBOR(fixture[:cut]); err == nil {
			t.Fatalf("accepted truncated commit at %d", cut)
		}
	}
	original := *c
	if err := c.UnmarshalCBOR(bytes.NewReader(fixture[:len(fixture)-1])); err == nil || *c != original {
		t.Fatal("failed CBOR decode mutated receiver or accepted truncation")
	}
}

func TestCommitConstruction(t *testing.T) {
	c, err := NewCommit(sha256.Sum256([]byte("repo")), testRevision, testSpace, testAuthor)
	if err != nil {
		t.Fatal(err)
	}
	auth := &MACVerification{SharedSecret: [32]byte{1, 2, 3}}
	auth.MAC, err = c.ComputeMAC(auth.SharedSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(testSpace, testAuthor, auth); err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(c); err != nil {
		t.Fatal("commit must serialize without any signature or embedded MAC", err)
	}
	for _, invalid := range []struct {
		space    syntax.SpaceRef
		author   syntax.DID
		revision syntax.TID
	}{{"invalid", testAuthor, testRevision}, {testSpace, "invalid", testRevision}, {testSpace, testAuthor, "invalid"}} {
		if _, err := NewCommit([32]byte{}, invalid.revision, invalid.space, invalid.author); err == nil {
			t.Fatal("accepted invalid constructor input")
		}
	}
	var missing *Commit
	if err := missing.Verify(testSpace, testAuthor, auth); err == nil {
		t.Fatal("accepted missing commit")
	}
	if _, err := missing.ComputeMAC(auth.SharedSecret); err == nil {
		t.Fatal("computed MAC for missing commit")
	}
}

func testCBORText(w *cbg.CborWriter, value string) error {
	if err := w.WriteMajorTypeHeader(cbg.MajTextString, uint64(len(value))); err != nil {
		return err
	}
	_, err := w.Write([]byte(value))
	return err
}

func TestCommitStaticBytesJSONVector(t *testing.T) {
	const fixture = `{"ver":1,"repo":"did:web:author.example","space":"at://did:web:authority.example/space/com.example.space/skey","rev":"3mtwwsccuqy22","data":{"$bytes":"vKXsJ5KymUrpNSNk/CdsSpo4M5wckYJAZTYEZOR6jMM"}}`
	c, err := DecodeCommitJSON([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", c.Data) != "bca5ec2792b2994ae9352364fc276c4a9a38339c1c91824065360464e47a8cc3" {
		t.Fatal("static $bytes vector decoded incorrectly")
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := atdata.UnmarshalJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := obj["data"].(atdata.Bytes); !ok || !bytes.Equal(got, c.Data[:]) {
		t.Fatal("static $bytes vector encoded incorrectly")
	}
}
