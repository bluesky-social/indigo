package spaces

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	cbg "github.com/whyrusleeping/cbor-gen"
)

const testSpace syntax.SpaceRef = "at://did:web:authority.example/space/com.example.space/skey"
const testAuthor syntax.DID = "did:web:author.example"
const testRevision syntax.TID = "3mtwwsccuqy22"

// Independently constructed with RFC 5869's one-block expand, not crypto/hkdf
// or the Spaces implementation. The static vector was calculated separately
// with Python's hashlib/hmac and big-endian struct.pack.
func referenceContext(space syntax.SpaceRef, author syntax.DID, revision syntax.TID, ikm [32]byte) []byte {
	var out bytes.Buffer
	out.WriteString("atproto-space-v1")
	for _, field := range [][]byte{[]byte(space), []byte(author), []byte(revision), ikm[:]} {
		_ = binary.Write(&out, binary.BigEndian, uint16(len(field)))
		out.Write(field)
	}
	return out.Bytes()
}

func referenceMAC(ikm [32]byte, context []byte, digest [32]byte) [32]byte {
	expand := hmac.New(sha256.New, ikm[:])
	expand.Write(context)
	expand.Write([]byte{1})
	mac := hmac.New(sha256.New, expand.Sum(nil))
	mac.Write(digest[:])
	var result [32]byte
	copy(result[:], mac.Sum(nil))
	return result
}

func independentCommit(t *testing.T, digest [32]byte) (*Commit, atcrypto.PublicKey) {
	t.Helper()
	// Use the standard library's signing operation, including explicit low-S
	// normalization, rather than the implementation's Sign method.
	d := big.NewInt(1)
	x, y := elliptic.P256().ScalarBaseMult(d.Bytes())
	private := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, D: d}
	key, err := atcrypto.ParsePublicBytesP256(elliptic.MarshalCompressed(elliptic.P256(), x, y))
	if err != nil {
		t.Fatal(err)
	}
	c := &Commit{Version: 1, Hash: digest, Revision: testRevision}
	for i := range c.IKM {
		c.IKM[i] = byte(i)
	}
	context := referenceContext(testSpace, testAuthor, c.Revision, c.IKM)
	c.MAC = referenceMAC(c.IKM, context, digest)
	hash := sha256.Sum256(context)
	r, s, err := ecdsa.Sign(rand.Reader, private, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	halfOrder := new(big.Int).Rsh(new(big.Int).Set(private.Curve.Params().N), 1)
	if s.Cmp(halfOrder) > 0 {
		s.Sub(private.Curve.Params().N, s)
	}
	c.Signature = make([]byte, 64)
	r.FillBytes(c.Signature[:32])
	s.FillBytes(c.Signature[32:])
	return c, key
}

func TestCommitIndependentContextAndHMACVector(t *testing.T) {
	c, key := independentCommit(t, sha256.Sum256([]byte("independent repository digest")))
	context, err := commitContext(testSpace, testAuthor, c.Revision, c.IKM)
	if err != nil {
		t.Fatal(err)
	}
	const expectedContext = "617470726f746f2d73706163652d7631003b61743a2f2f6469643a7765623a617574686f726974792e6578616d706c652f73706163652f636f6d2e6578616d706c652e73706163652f736b657900166469643a7765623a617574686f722e6578616d706c65000d336d74777773636375717932320020000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	if hex.EncodeToString(context) != expectedContext {
		t.Fatal("context differs from independent TLS-vector fixture")
	}
	mac, err := commitMAC(c.IKM, context, c.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(c.Hash[:]) != "bca5ec2792b2994ae9352364fc276c4a9a38339c1c91824065360464e47a8cc3" {
		t.Fatal("independent hash fixture differs")
	}
	if hex.EncodeToString(mac[:]) != "7aa2f55013153432342f6d87c913f6e2fb083ab3f2ceed2e442565143d4eaf1f" {
		t.Fatal("MAC does not authenticate the repo hash")
	}
	if err := c.Verify(testSpace, testAuthor, key); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(syntax.SpaceRef(string(testSpace)+"2"), testAuthor, key); err == nil {
		t.Fatal("accepted another space")
	}
	if err := c.Verify(testSpace, "did:web:other.example", key); err == nil {
		t.Fatal("accepted another author")
	}
	for _, field := range []string{"hash", "mac", "ikm", "sig", "rev", "ver"} {
		t.Run(field, func(t *testing.T) {
			copyCommit := *c
			copyCommit.Signature = bytes.Clone(c.Signature)
			switch field {
			case "hash":
				copyCommit.Hash[0] ^= 1
			case "mac":
				copyCommit.MAC[0] ^= 1
			case "ikm":
				copyCommit.IKM[0] ^= 1
			case "sig":
				copyCommit.Signature[0] ^= 1
			case "rev":
				copyCommit.Revision = "3mtwwsccuqy23"
			case "ver":
				copyCommit.Version = 2
			}
			if err := copyCommit.Verify(testSpace, testAuthor, key); err == nil {
				t.Fatal("accepted changed commit")
			}
		})
	}
}

func independentCommitJSON(t *testing.T, c *Commit) map[string]any {
	t.Helper()
	wire := func(value []byte) any {
		return map[string]string{"$bytes": base64.RawStdEncoding.EncodeToString(value)}
	}
	return map[string]any{"ver": int64(1), "hash": wire(c.Hash[:]), "ikm": wire(c.IKM[:]), "mac": wire(c.MAC[:]), "sig": wire(c.Signature), "rev": c.Revision.String()}
}

func independentCommitCBOR(t *testing.T, c *Commit, order []string) []byte {
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
		case "ikm":
			err = cbg.WriteByteArray(cw, c.IKM[:])
		case "mac":
			err = cbg.WriteByteArray(cw, c.MAC[:])
		case "hash":
			err = cbg.WriteByteArray(cw, c.Hash[:])
		case "sig":
			err = cbg.WriteByteArray(cw, c.Signature)
		case "rev":
			err = testCBORText(cw, string(c.Revision))
		case "ver":
			err = cw.WriteMajorTypeHeader(cbg.MajUnsignedInt, uint64(c.Version))
		case "$type":
			err = testCBORText(cw, "com.atproto.space.defs#signedCommit")
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}

func TestCommitIndependentWireCodecs(t *testing.T) {
	c, key := independentCommit(t, sha256.Sum256([]byte("wire fixture")))
	jsonFixture, err := json.Marshal(independentCommitJSON(t, c))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCommitJSON(jsonFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := decoded.Verify(testSpace, testAuthor, key); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"hash", "ikm", "mac", "sig"} {
		if !bytes.Contains(got[field], []byte(`"$bytes"`)) {
			t.Fatalf("%s lacks protocol $bytes", field)
		}
	}
	for _, order := range [][]string{{"ikm", "mac", "rev", "sig", "ver", "hash"}, {"ikm", "mac", "rev", "sig", "ver", "hash", "$type"}} {
		fixture := independentCommitCBOR(t, c, order)
		decoded, err := DecodeCommitCBOR(fixture)
		if err != nil {
			t.Fatal(err)
		}
		if err := decoded.Verify(testSpace, testAuthor, key); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := c.MarshalCBOR(&out); err != nil {
		t.Fatal(err)
	}
	obj, err := atdata.UnmarshalCBOR(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"hash", "ikm", "mac", "sig"} {
		if _, ok := obj[field].(atdata.Bytes); !ok {
			t.Fatalf("%s is not a CBOR byte string", field)
		}
	}
}

func TestCommitMalformedJSON(t *testing.T) {
	c, _ := independentCommit(t, [32]byte{})
	for _, field := range []string{"hash", "ikm", "mac", "sig"} {
		for _, size := range []int{0, 31, 33, 63, 65} {
			t.Run(field+"/length"+big.NewInt(int64(size)).String(), func(t *testing.T) {
				obj := independentCommitJSON(t, c)
				obj[field] = map[string]string{"$bytes": base64.RawStdEncoding.EncodeToString(make([]byte, size))}
				data, _ := json.Marshal(obj)
				if _, err := DecodeCommitJSON(data); err == nil {
					t.Fatal("accepted invalid byte length")
				}
			})
		}
		for _, value := range []any{[]int{1, 2}, "AA", nil, map[string]any{"$bytes": "AA", "other": true}} {
			obj := independentCommitJSON(t, c)
			obj[field] = value
			data, _ := json.Marshal(obj)
			if _, err := DecodeCommitJSON(data); err == nil {
				t.Fatalf("accepted malformed %s", field)
			}
		}
	}
	for _, change := range []func(map[string]any){
		func(o map[string]any) { delete(o, "ver") },
		func(o map[string]any) { o["ver"] = 2 },
		func(o map[string]any) { o["ver"] = nil },
		func(o map[string]any) { o["rev"] = "bad" },
		func(o map[string]any) { o["$type"] = "com.example.other" },
	} {
		obj := independentCommitJSON(t, c)
		change(obj)
		data, _ := json.Marshal(obj)
		if _, err := DecodeCommitJSON(data); err == nil {
			t.Fatal("accepted malformed commit JSON")
		}
	}
	fixture, _ := json.Marshal(independentCommitJSON(t, c))
	for _, data := range [][]byte{append(bytes.Clone(fixture), []byte(" true")...), []byte(strings.Replace(string(fixture), `"ver":1`, `"ver":1.0`, 1)), []byte(strings.Replace(string(fixture), `"ver":1`, `"ver":1,"ver":1`, 1))} {
		if _, err := DecodeCommitJSON(data); err == nil {
			t.Fatal("accepted trailing, fractional, or duplicate version")
		}
	}
	original := *c
	if err := c.UnmarshalJSON([]byte(`{"ver":2}`)); err == nil {
		t.Fatal("accepted incomplete JSON")
	}
	if c.Version != original.Version || c.Hash != original.Hash || !bytes.Equal(c.Signature, original.Signature) {
		t.Fatal("failed decode mutated receiver")
	}
}

func TestCommitMalformedCBOR(t *testing.T) {
	c, _ := independentCommit(t, [32]byte{})
	order := []string{"ikm", "mac", "rev", "sig", "ver", "hash"}
	fixture := independentCommitCBOR(t, c, order)
	badOrder := independentCommitCBOR(t, c, []string{"mac", "ikm", "rev", "sig", "ver", "hash"})
	duplicate := independentCommitCBOR(t, c, []string{"ikm", "mac", "rev", "sig", "ver", "ver"})
	invalidVersion := *c
	invalidVersion.Version = 2
	invalidRevision := *c
	invalidRevision.Revision = "bad"
	invalidSignature := *c
	invalidSignature.Signature = make([]byte, 63)
	bad := [][]byte{badOrder, duplicate, append(bytes.Clone(fixture), 0), independentCommitCBOR(t, &invalidVersion, order), independentCommitCBOR(t, &invalidRevision, order), independentCommitCBOR(t, &invalidSignature, order), append([]byte{0xb8, 6}, fixture[1:]...)}
	// Replace the first field's byte-string header with a text-string header.
	wrongType := bytes.Clone(fixture)
	wrongType[5] = 0x78
	bad = append(bad, wrongType)
	// Fixed-size byte field truncated to 31 bytes, while retaining the next field.
	wrongLength := append(bytes.Clone(fixture[:6]), fixture[6:38]...)
	wrongLength[6] = 31
	wrongLength = append(wrongLength, fixture[39:]...)
	bad = append(bad, wrongLength)
	for _, data := range bad {
		if _, err := DecodeCommitCBOR(data); err == nil {
			t.Fatal("accepted malformed commit CBOR")
		}
	}
	for cut := range len(fixture) {
		if _, err := DecodeCommitCBOR(fixture[:cut]); err == nil {
			t.Fatalf("accepted truncated commit at %d", cut)
		}
	}
}

func TestCommitConstructionAndSigning(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		t.Fatal(err)
	}
	public, err := key.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewCommit(sha256.Sum256([]byte("repo")), testRevision, testSpace, testAuthor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(c); err == nil {
		t.Fatal("encoded unsigned commit")
	}
	if err := c.Sign(testSpace, testAuthor, key); err != nil {
		t.Fatal(err)
	}
	if err := c.Verify(testSpace, testAuthor, public); err != nil {
		t.Fatal(err)
	}
	if c.MAC != referenceMAC(c.IKM, referenceContext(testSpace, testAuthor, c.Revision, c.IKM), c.Hash) {
		t.Fatal("constructed MAC differs from independent reference")
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
	// This literal was constructed outside Go with standard un-padded base64.
	// The all-zero signature is a structural codec fixture, not proof of authorship.
	const fixture = `{"ver":1,"hash":{"$bytes":"vKXsJ5KymUrpNSNk/CdsSpo4M5wckYJAZTYEZOR6jMM"},"ikm":{"$bytes":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"},"mac":{"$bytes":"eqL1UBMVNDI0L22HyRP24vsIOrPyzu0uRCVlFD1Orx8"},"sig":{"$bytes":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},"rev":"3mtwwsccuqy22"}`
	c, err := DecodeCommitJSON([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(c.Hash[:]) != "bca5ec2792b2994ae9352364fc276c4a9a38339c1c91824065360464e47a8cc3" || hex.EncodeToString(c.MAC[:]) != "7aa2f55013153432342f6d87c913f6e2fb083ab3f2ceed2e442565143d4eaf1f" {
		t.Fatal("static $bytes vector decoded incorrectly")
	}
	for i, value := range c.IKM {
		if value != byte(i) {
			t.Fatal("static nonce vector decoded incorrectly")
		}
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var expected, actual map[string]any
	if err := json.Unmarshal([]byte(fixture), &expected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &actual); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"hash", "ikm", "mac", "sig"} {
		if actual[field].(map[string]any)["$bytes"] != expected[field].(map[string]any)["$bytes"] {
			t.Fatalf("static %s vector encoded incorrectly", field)
		}
	}
}
