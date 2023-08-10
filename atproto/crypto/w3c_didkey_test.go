package crypto

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/mr-tron/base58"
	"github.com/stretchr/testify/assert"
)

type DidKeyFixture struct {
	PrivateKeyBytesBase58 string `json:"privateKeyBytesBase58"`
	PrivateKeyBytesHex    string `json:"privateKeyBytesHex"`
	PublicDidKey          string `json:"publicDidKey"`
}

func TestDidKeyFixtures(t *testing.T) {

	fixtureBatches := []struct {
		path string
		kt   KeyType
	}{
		{path: "testdata/w3c_didkey_P256.json", kt: P256},
		{path: "testdata/w3c_didkey_K256.json", kt: K256},
	}

	for _, batch := range fixtureBatches {

		f, err := os.Open(batch.path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()

		fixBytes, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}

		var fixtures []DidKeyFixture
		if err := json.Unmarshal(fixBytes, &fixtures); err != nil {
			t.Fatal(err)
		}

		for _, row := range fixtures {
			testDidKeyFixture(t, row, batch.kt)
		}
	}
}

func testDidKeyFixture(t *testing.T, row DidKeyFixture, kt KeyType) {
	assert := assert.New(t)

	var raw []byte
	var err error
	if row.PrivateKeyBytesBase58 != "" {
		raw, err = base58.Decode(row.PrivateKeyBytesBase58)
		if err != nil {
			t.Fatal(err)
		}
	} else if row.PrivateKeyBytesHex != "" {
		raw, err = hex.DecodeString(row.PrivateKeyBytesHex)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal("no private key found")
	}

	priv, err := ParsePrivateKeyBytes(raw, kt)
	if err != nil {
		t.Fatal(err)
	}
	kBytes := priv.PublicKey()
	kDidKey, err := ParsePublicDidKey(row.PublicDidKey)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(kBytes.Equal(kDidKey), true)
	assert.Equal(row.PublicDidKey, kBytes.DidKey())
	assert.Equal(row.PublicDidKey, kDidKey.DidKey())
}
