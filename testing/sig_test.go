package testing

import (
	"context"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/repo"

	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/multiformats/go-multibase"
	"github.com/stretchr/testify/assert"
	"github.com/whyrusleeping/go-did"
)

func TestVerification(t *testing.T) {
	assert := assert.New(t)

	fi, err := os.Open("test_files/divy.repo")
	if err != nil {
		t.Fatal(err)
	}

	bs := blockstore.NewBlockstore(datastore.NewMapDatastore())
	ctx := context.TODO()
	c, err := repo.IngestRepo(ctx, bs, fi)
	if err != nil {
		t.Fatal(err)
	}

	r, err := repo.OpenRepo(ctx, bs, c, true)
	if err != nil {
		t.Fatal(err)
	}

	vmstr := `{
      "id": "#atproto",
      "type": "EcdsaSecp256k1VerificationKey2019",
      "controller": "did:plc:wj5jny4sq4sohwoaxjkjgug6",
      "publicKeyMultibase": "zQYEBzXeuTM9UR3rfvNag6L3RNAs5pQZyYPsomTsgQhsxLdEgCrPTLgFna8yqCnxPpNT7DBk6Ym3dgPKNu86vt9GR"
    }`
	var vm did.VerificationMethod

	if err := json.Unmarshal([]byte(vmstr), &vm); err != nil {
		t.Fatal(err)
	}

	pk, err := did.KeyFromMultibase(vm)
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(pk.Type, "EcdsaSecp256k1VerificationKey2019")

	scom := r.SignedCommit()

	msg, err := scom.Unsigned().BytesForSigning()
	if err != nil {
		t.Fatal(err)
	}

	if err := pk.Verify(msg, scom.Sig); err != nil {
		t.Fatal(err)
	}
}

func TestVerificationK256(t *testing.T) {
	// 2023-03-30T11:18:20.605-0700	WARN	indexer	indexer/keymgr.go:37	trying to verify sig	{"key": {"Raw":"BBKybGcJOMvsIyPaKglHtcocOFN7QrlppYHN3i4fW5PfLmfUFCXNcNKMk/MjT/cnquZS1APwxr6QUR7LE8/bJC8=","Type":"EcdsaSecp256k1VerificationKey2019"}, "sigBytes": "1ZJM8YFVmHJksi+liHFn62GBfUd7zDio0BVej0JTjtJUdYMgmV8Mg4/4RNfL9VFM8bXMhzusJ1qpu2kTyHoliA==", "msgBytes": "pGNkaWR4IGRpZDpwbGM6cHVydnZqNXV0N2hyeGo1ejdtbTZyNGd0ZGRhdGHYKlglAAFxEiAG8t9fbFkSGKBhEXYLZLC5njldpEfHGg2hheTdR9VLi2RwcmV22CpYJQABcRIgtJroXREnp3TZxxf8xZTQC+w4+vnfz1KIkWVitinSPOFndmVyc2lvbgI="}

	assert := assert.New(t)
	keyBytes, err := base64.StdEncoding.DecodeString("BBKybGcJOMvsIyPaKglHtcocOFN7QrlppYHN3i4fW5PfLmfUFCXNcNKMk/MjT/cnquZS1APwxr6QUR7LE8/bJC8=")
	assert.NoError(err)
	msgBytes, err := base64.StdEncoding.DecodeString("pGNkaWR4IGRpZDpwbGM6cHVydnZqNXV0N2hyeGo1ejdtbTZyNGd0ZGRhdGHYKlglAAFxEiAG8t9fbFkSGKBhEXYLZLC5njldpEfHGg2hheTdR9VLi2RwcmV22CpYJQABcRIgtJroXREnp3TZxxf8xZTQC+w4+vnfz1KIkWVitinSPOFndmVyc2lvbgI=")
	assert.NoError(err)
	sigBytes, err := base64.StdEncoding.DecodeString("1ZJM8YFVmHJksi+liHFn62GBfUd7zDio0BVej0JTjtJUdYMgmV8Mg4/4RNfL9VFM8bXMhzusJ1qpu2kTyHoliA==")
	assert.NoError(err)

	key := did.PubKey{
		Type: "EcdsaSecp256k1VerificationKey2019", // k1 -> K256
		Raw:  keyBytes,
	}

	assert.NoError(key.Verify(msgBytes, sigBytes))
}

func parseKeyFromMultibase(t *testing.T, s, keyType string) did.PubKey {
	_, data, err := multibase.Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	return did.PubKey{
		Type: keyType,
		Raw:  data,
	}
}

func parseDidKey(t *testing.T, s string) did.PubKey {
	parts := strings.SplitN(s, ":", 3)
	_, data, err := multibase.Decode(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case data[0] == 0x80 && data[1] == 0x24:
		// p256;  need to "uncompress"
		curve := elliptic.P256()
		x, y := elliptic.UnmarshalCompressed(curve, data[2:])
		return did.PubKey{
			Type: "EcdsaSecp256r1VerificationKey2019",
			Raw:  elliptic.Marshal(curve, x, y),
		}
	case data[0] == 0xE7 && data[1] == 0x01:
		// k256; apparently don't need to uncompress
		return did.PubKey{
			Type: "EcdsaSecp256k1VerificationKey2019",
			Raw:  data[2:],
		}
	default:
		t.Fatal(fmt.Errorf("unhandled did:key type: %d %d", data[0], data[1]))
	}
	panic("unreachable")
}

func TestInteropSignatures(t *testing.T) {
	assert := assert.New(t)

	testTable := []struct {
		description  string
		keyType      string
		docType      string
		didKey       string
		multibaseKey string
		msgBase64    string
		sigBase64    string
	}{
		{
			description:  "p256/secp256r1",
			keyType:      "ES256",
			docType:      "EcdsaSecp256r1VerificationKey2019",
			didKey:       "did:key:zDnaebKg3xzqP4DnpjCjaUpPkCVridQJufGQqwYWi623VPxcN",
			multibaseKey: "zQi6N7wGYuk7GhUfU17jkaitpAPzeGyCufAyz9i7jT4T2XSC1KjbAA2QridVGidGbfXdV9X9fWotHmmkVwbeXRujR",
			msgBase64:    "oWVoZWxsb2V3b3JsZA",
			sigBase64:    "/SbeL+Tx6TXUZGC7uHHkI9b+1ARoLcqTzkUYJmPBqlCVAvKkxkPowfqKwnJWpZcuF4g3MT7eVgdBROHgdUplIQ",
		},
		{
			description:  "k256/secp256k1",
			keyType:      "ES256K",
			docType:      "EcdsaSecp256k1VerificationKey2019",
			didKey:       "did:key:zQ3shueETAVp1HCdE7pcrUD1wEHNhfaQiDJmWeJgCxZRmDJuy",
			multibaseKey: "zRvvEWNhRDxCYpkWWKzvrCP5Sca54yhgXjLcSgfJsWW9Jk8Wq79d5h2PTW3BQDmNQv4prqGjRkXiEDwnfpq2tTsmp",
			msgBase64:    "oWVoZWxsb2V3b3JsZA",
			sigBase64:    "FbKI9u/VoY9SFHvtsuYsZULkt3QiNRrPUP3N4bX0xTxfWIUccyoNJ7egTITlaD1xBhXAvDdBnbyC6aOZ0jyjEA",
		},
	}

	for _, row := range testTable {
		pkDid := parseDidKey(t, row.didKey)
		pkMultibase := parseKeyFromMultibase(t, row.multibaseKey, row.docType)
		msgBytes, err := base64.RawStdEncoding.DecodeString(row.msgBase64)
		assert.NoError(err)
		sigBytes, err := base64.RawStdEncoding.DecodeString(row.sigBase64)
		assert.NoError(err)

		assert.NoError(pkDid.Verify(msgBytes, sigBytes), "keyType=%v format=%v", row.description, "did:key")
		assert.NoError(pkMultibase.Verify(msgBytes, sigBytes), "keyType=%v format=%v", row.description, "multibase")
		assert.Error(pkMultibase.Verify(msgBytes, []byte{1, 2, 3}), "keyType=%v format=%v", row.description, "multibase")
		assert.Error(pkMultibase.Verify([]byte{1, 2, 3}, sigBytes), "keyType=%v format=%v", row.description, "multibase")

		// TODO: investigate these additional tests, which partially fail, instead of "continue"
		/*
			assert.Equal(pkDid, pkMultibase, row.description)
			assert.NotEqual(pkDid.MultibaseString(), "<invalid key>", "keyType=%v format=%v", row.description, "did:key")
			assert.NotEqual(pkMultibase.MultibaseString(), "<invalid key>", "keyType=%v format=%v", row.description, "multibase")

			// check that keys round-trip ok
			assert.Equal(row.didKey, pkDid.DID(), "export keyType=%v format=%v", row.description, "did:key")
			assert.Equal(row.multibaseKey, pkDid.MultibaseString(), "export keyType=%v format=%v", row.description, "multibase")
			pkDid = parseDidKey(t, pkDid.DID())
			pkMultibase = parseKeyFromMultibase(t, pkMultibase.MultibaseString(), row.docType)

			assert.NoError(pkDid.Verify(msgBytes, sigBytes), "round-trip keyType=%v format=%v", row.description, "did:key")
			assert.NoError(pkMultibase.Verify(msgBytes, sigBytes), "round-trip keyType=%v format=%v", row.description, "multibase")
		*/
	}
}
