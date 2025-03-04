package label

import (
	"encoding/json"
	"testing"

	"github.com/bluesky-social/indigo/atproto/crypto"

	"github.com/stretchr/testify/assert"
)

func TestVerifyLabel(t *testing.T) {
	assert := assert.New(t)

	pubkeyStr := "zQ3shcnfWLQN1bY4d2patsEAYFzy4xp1zdckEvHsV7S4ocTnC"
	pubkey, err := crypto.ParsePublicMultibase(pubkeyStr)
	if err != nil {
		t.Fatal(err)
	}

	labelJSON := []byte(`{
            "ver": 1,
            "src": "did:plc:n3timvoib5nau7gvwd6cshap",
            "uri": "did:plc:44ybard66vv44zksje25o7dz",
            "val": "bladerunner",
            "cts": "2024-10-23T17:51:19.128Z",
            "sig": {
                "$bytes": "uCRNA5mTzh078T5xZtkvLEt/O+z0gsKM3aqRI/lVAB8ZtbMnznwS/JwHopZE40JhNNDj80z8gsDLAp/hWqG5Pg"
            }
    }`)

	var l Label
	if err := json.Unmarshal(labelJSON, &l); err != nil {
		t.Fatal(err)
	}

	assert.NoError(l.VerifySyntax())
	assert.NoError(l.VerifySignature(pubkey))

	// check that signature fails after mutation
	l.Val = "wrong"
	assert.NoError(l.VerifySyntax())
	assert.Error(l.VerifySignature(pubkey))

	// version check
	l.Version = 0
	assert.Error(l.VerifySyntax())
}

func TestParseLabel(t *testing.T) {
	assert := assert.New(t)

	unsignedJSON := []byte(`{
            "cts": "2024-10-23T17:51:19.128Z",
            "src": "did:plc:n3timvoib5nau7gvwd6cshap",
            "uri": "did:plc:44ybard66vv44zksje25o7dz",
            "val": "bladerunner",
            "ver": 1
    }`)

	var l Label
	if err := json.Unmarshal(unsignedJSON, &l); err != nil {
		t.Fatal(err)
	}
	assert.NoError(l.VerifySyntax())
}

func TestSignLabel(t *testing.T) {
	assert := assert.New(t)

	l := Label{
		Version:   ATPROTO_LABEL_VERSION,
		CreatedAt: "2024-10-23T17:51:19.128Z",
		URI:       "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/app.bsky.actor.profile/self",
		Val:       "good",
		SourceDID: "did:plc:ewvi7nxzyoun6zhxrhs64oiz",
	}

	priv, err := crypto.GeneratePrivateKeyK256()
	if err != nil {
		t.Fatal(err)
	}

	pub, err := priv.PublicKey()
	if err != nil {
		t.Fatal(err)
	}

	assert.NoError(l.Sign(priv))
	assert.NoError(l.VerifySignature(pub))
}
