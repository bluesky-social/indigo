package testing

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"

	"github.com/bluesky-social/indigo/repo"

	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/stretchr/testify/assert"
	"github.com/whyrusleeping/go-did"
)

func TestVerification(t *testing.T) {
	assert := assert.New(t)

	fi, err := os.Open("divy.repo")
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

func TestVerificationK256Another(t *testing.T) {
	t.Skip("XXX: this test is failing!")

	// 2023-03-30T14:45:38.564-0700	WARN	indexer	indexer/keymgr.go:39	signature failed to verify	{"err": "invalid signature", "did": "did:plc:5wy3mk2y6hr5hfjd27t25mwq", "pubKey": {"Raw":"BG43klS5n0pGwV4pbSDZus9gEpAv9y9ixMw5g+BTXejAefzTvGuS0wXUtd+4gNynDKnJI8Ql5HZgd31wUOcuEnI=","Type":"EcdsaSecp256k1VerificationKey2019"}, "sigBytes": "/RqP+2UeQxEotDobElhPIqMUfLuP6NAqWH1DFYz4uBIzG9m2rq+AOv+7ByTs1Iz3W2Pb/ArU6h4u9b32TcOA8w==", "msgBytes": "pGNkaWR4IGRpZDpwbGM6NXd5M21rMnk2aHI1aGZqZDI3dDI1bXdxZGRhdGHYKlglAAFxEiAmLxtdfzvOecsKYGpQcJoKe/sez3Azipj+ruH8+Oeb2mRwcmV22CpYJQABcRIgaaoi6eUxIHB/n6QucX3fjxP/43pLhAd2NEo8wIpc1I1ndmVyc2lvbgI="}

	assert := assert.New(t)
	keyBytes, err := base64.StdEncoding.DecodeString("BG43klS5n0pGwV4pbSDZus9gEpAv9y9ixMw5g+BTXejAefzTvGuS0wXUtd+4gNynDKnJI8Ql5HZgd31wUOcuEnI=")
	assert.NoError(err)
	msgBytes, err := base64.StdEncoding.DecodeString("pGNkaWR4IGRpZDpwbGM6NXd5M21rMnk2aHI1aGZqZDI3dDI1bXdxZGRhdGHYKlglAAFxEiAmLxtdfzvOecsKYGpQcJoKe/sez3Azipj+ruH8+Oeb2mRwcmV22CpYJQABcRIgaaoi6eUxIHB/n6QucX3fjxP/43pLhAd2NEo8wIpc1I1ndmVyc2lvbgI=")
	assert.NoError(err)
	sigBytes, err := base64.StdEncoding.DecodeString("/RqP+2UeQxEotDobElhPIqMUfLuP6NAqWH1DFYz4uBIzG9m2rq+AOv+7ByTs1Iz3W2Pb/ArU6h4u9b32TcOA8w==")
	assert.NoError(err)

	key := did.PubKey{
		Type: "EcdsaSecp256k1VerificationKey2019", // k1 -> K256
		Raw:  keyBytes,
	}

	assert.NoError(key.Verify(msgBytes, sigBytes))
}

func TestVerificationP256(t *testing.T) {
	t.Skip("XXX: this test is failing!")

	// 2023-03-30T10:48:24.163-0700    WARN    indexer indexer/keymgr.go:37    trying to verify sig    {"key": {"Raw":"BHNFqXf9epzecIlKScjkhbG40FfJ77Cc3klxkozXuQ+UzzxyVwIQMmwrd8hW+BtF1GHLv7bt3D6feMvsnOgoxTI=","Type":"EcdsaSecp256r1VerificationKey2019"}, "sigBytes": "Utv2SWqgajkPF0MdMAEEK4JY1eF6DQEqPZgYEXAFlus4zRcdoK/5ttKRG1Nn4yaqbxJ/ezpW2d2dbZoxhhTe1A==", "msgBytes": "pGNkaWR4IGRpZDpwbGM6emtva2JrZ3g3a3B6bGF0Y2ZxeXVkZ2ttZGRhdGHYKlglAAFxEiC9iE2tv7bvgaTPHg7Z+ay8hQK+7QY+OM7OO8IYI0X/sWRwcmV22CpYJQABcRIgG5obqev4a3cjqY6juXsSUgcV4Vad5id+1nE1/GPqfuBndmVyc2lvbgI="}

	assert := assert.New(t)
	keyBytes, err := base64.StdEncoding.DecodeString("BHNFqXf9epzecIlKScjkhbG40FfJ77Cc3klxkozXuQ+UzzxyVwIQMmwrd8hW+BtF1GHLv7bt3D6feMvsnOgoxTI=")
	assert.NoError(err)
	msgBytes, err := base64.StdEncoding.DecodeString("pGNkaWR4IGRpZDpwbGM6emtva2JrZ3g3a3B6bGF0Y2ZxeXVkZ2ttZGRhdGHYKlglAAFxEiC9iE2tv7bvgaTPHg7Z+ay8hQK+7QY+OM7OO8IYI0X/sWRwcmV22CpYJQABcRIgG5obqev4a3cjqY6juXsSUgcV4Vad5id+1nE1/GPqfuBndmVyc2lvbgI=")
	assert.NoError(err)
	sigBytes, err := base64.StdEncoding.DecodeString("Utv2SWqgajkPF0MdMAEEK4JY1eF6DQEqPZgYEXAFlus4zRcdoK/5ttKRG1Nn4yaqbxJ/ezpW2d2dbZoxhhTe1A==")
	assert.NoError(err)

	key := did.PubKey{
		Type: "EcdsaSecp256r1VerificationKey2019", // r1 -> P256
		Raw:  keyBytes,
	}

	assert.NoError(key.Verify(msgBytes, sigBytes))
}
