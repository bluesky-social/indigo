package labeling

import (
	"encoding/json"
	"testing"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atcrypto"

	"github.com/stretchr/testify/assert"
)

func TestVerifyLabel(t *testing.T) {
	assert := assert.New(t)

	pubkeyStr := "zQ3shcnfWLQN1bY4d2patsEAYFzy4xp1zdckEvHsV7S4ocTnC"
	pubkey, err := atcrypto.ParsePublicMultibase(pubkeyStr)
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

	priv, err := atcrypto.GeneratePrivateKeyK256()
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

func TestToLexicon(t *testing.T) {
	assert := assert.New(t)

	expiresAt := "2025-07-28T23:53:19.804Z"
	negated := true
	cid := "bafyreifxykqhed72s26cr4i64rxvrtofeqrly3j4vjzbkvo3ckkjbxjqtq"

	l := Label{
		CID:       &cid,
		CreatedAt: "2024-10-23T17:51:19.128Z",
		ExpiresAt: &expiresAt,
		Negated:   &negated,
		SourceDID: "did:plc:ewvi7nxzyoun6zhxrhs64oiz",
		URI:       "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/app.bsky.actor.profile/self",
		Val:       "good",
		Version:   ATPROTO_LABEL_VERSION,
		Sig:       []byte("sig"), // invalid, but we only care about the conversion
	}

	lex := l.ToLexicon()
	assert.Equal(l.Version, *lex.Ver)
	assert.Equal(l.CreatedAt, lex.Cts)
	assert.Equal(l.URI, lex.Uri)
	assert.Equal(l.Val, lex.Val)
	assert.Equal(l.CID, lex.Cid)
	assert.Equal(l.ExpiresAt, lex.Exp)
	assert.Equal(l.Negated, lex.Neg)
	assert.Equal(l.SourceDID, lex.Src)
}

func TestFromLexicon(t *testing.T) {
	assert := assert.New(t)

	expiresAt := "2025-07-28T23:53:19.804Z"
	negated := true
	cid := "bafyreifxykqhed72s26cr4i64rxvrtofeqrly3j4vjzbkvo3ckkjbxjqtq"
	version := int64(1)

	lex := &comatproto.LabelDefs_Label{
		Cid: &cid,
		Cts: "2024-10-23T17:51:19.128Z",
		Exp: &expiresAt,
		Neg: &negated,
		Src: "did:plc:ewvi7nxzyoun6zhxrhs64oiz",
		Uri: "at://did:plc:ewvi7nxzyoun6zhxrhs64oiz/app.bsky.actor.profile/self",
		Val: "good",
		Ver: &version,
		Sig: []byte("sig"), // invalid, but we only care about the conversion
	}

	l := FromLexicon(lex)
	assert.Equal(lex.Ver, &l.Version)
	assert.Equal(lex.Cts, l.CreatedAt)
	assert.Equal(lex.Uri, l.URI)
	assert.Equal(lex.Val, l.Val)
	assert.Equal(lex.Cid, l.CID)
	assert.Equal(lex.Exp, l.ExpiresAt)
	assert.Equal(lex.Neg, l.Negated)
	assert.Equal(lex.Src, l.SourceDID)
}
