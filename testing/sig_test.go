package testing

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/bluesky-social/indigo/repo"
	"github.com/ipfs/go-datastore"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	"github.com/whyrusleeping/go-did"
)

func TestVerification(t *testing.T) {
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

	scom := r.SignedCommit()

	msg, err := scom.Unsigned().BytesForSigning()
	if err != nil {
		t.Fatal(err)
	}

	if err := pk.Verify(msg, scom.Sig); err != nil {
		t.Fatal(err)
	}
}
