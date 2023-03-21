package api

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwk"
	did "github.com/whyrusleeping/go-did"
)

type testVector struct {
	Jwk       string
	Did       string
	Op        string
	SignedOp  string
	EncodedOp string
	Sig       string
}

func TestPLCCreateVector(t *testing.T) {

	tv := testVector{
		Jwk: `{
    "key_ops": [
      "sign"
    ],
    "ext": true,
    "kty": "EC",
    "x": "DwHtAjFHuhYWf9RKnE4XgMImEUKA40G0PR8Cbz-GTMY",
    "y": "pqny0WfZnm2jHEimqEwg21wvHsWWnqV3E_ofMW_jWuw",
    "crv": "P-256",
    "d": "elqCGEYj8NB1jXd-I7y4qyy35yZ2PhCOqPW3fM2ToM0"
  }`,
		Did: "did:key:zDnaeRSYs7c2NpcNA5NRAUqS8DCkLWDyNLnATi28D6w7no7hX",
		Op: `{
    "type": "create",
    "signingKey": "did:key:zDnaeRSYs7c2NpcNA5NRAUqS8DCkLWDyNLnATi28D6w7no7hX",
    "recoveryKey": "did:key:zDnaeRSYs7c2NpcNA5NRAUqS8DCkLWDyNLnATi28D6w7no7hX",
    "handle": "why.bsky.social",
    "service": "bsky.social",
    "prev": null
  }`,
		SignedOp: `{
    "type": "create",
    "signingKey": "did:key:zDnaeRSYs7c2NpcNA5NRAUqS8DCkLWDyNLnATi28D6w7no7hX",
    "recoveryKey": "did:key:zDnaeRSYs7c2NpcNA5NRAUqS8DCkLWDyNLnATi28D6w7no7hX",
    "handle": "why.bsky.social",
    "service": "bsky.social",
    "prev": null,
    "sig": "e8h6dCx405Z_95cZWWkZtfLgDPvfdXDG9pCZQi1NhduooZgb4d1w-CzahA3J-iNGCCgP3D0O5l997G3vQfxKOA"
  }`,
		EncodedOp: "pmRwcmV29mR0eXBlZmNyZWF0ZWZoYW5kbGVvd2h5LmJza3kuc29jaWFsZ3NlcnZpY2VrYnNreS5zb2NpYWxqc2lnbmluZ0tleXg5ZGlkOmtleTp6RG5hZVJTWXM3YzJOcGNOQTVOUkFVcVM4RENrTFdEeU5MbkFUaTI4RDZ3N25vN2hYa3JlY292ZXJ5S2V5eDlkaWQ6a2V5OnpEbmFlUlNZczdjMk5wY05BNU5SQVVxUzhEQ2tMV0R5TkxuQVRpMjhENnc3bm83aFg",
		Sig:       "e8h6dCx405Z_95cZWWkZtfLgDPvfdXDG9pCZQi1NhduooZgb4d1w-CzahA3J-iNGCCgP3D0O5l997G3vQfxKOA",
	}

	var op CreateOp
	if err := json.Unmarshal([]byte(tv.Op), &op); err != nil {
		t.Fatal(err)
	}

	buf := new(bytes.Buffer)
	if err := op.MarshalCBOR(buf); err != nil {
		t.Fatal(err)
	}

	exp, err := base64.RawURLEncoding.DecodeString(tv.EncodedOp)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(buf.Bytes(), exp) {
		fmt.Printf("%x\n", buf.Bytes())
		fmt.Printf("%x\n", exp)
		t.Fatal("encoding mismatched")
	}

	k, err := jwk.ParseKey([]byte(tv.Jwk))
	if err != nil {
		t.Fatal(err)
	}

	var spk ecdsa.PrivateKey
	if err := k.Raw(&spk); err != nil {
		t.Fatal(err)
	}

	mk := did.PrivKey{
		Raw:  &spk,
		Type: did.KeyTypeP256,
	}

	if mk.Public().DID() != tv.Did {
		t.Fatal("keys generated different DIDs")
	}

	mysig, err := mk.Sign(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(base64.RawURLEncoding.DecodeString(tv.Sig))
	fmt.Println(base64.RawURLEncoding.EncodeToString(mysig))
	fmt.Println(tv.Sig)

}
