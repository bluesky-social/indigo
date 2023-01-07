package key

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"fmt"

	"github.com/multiformats/go-multibase"
	"github.com/multiformats/go-varint"
)

const (
	MCed25519 = 0xED
	MCP256    = 0x1200
)

type Key struct {
	Raw  interface{}
	Type string
}

func (k *Key) Sign(b []byte) ([]byte, error) {
	switch k.Type {
	case "ed25519":
		return ed25519.Sign(k.Raw.(ed25519.PrivateKey), b), nil
	case "P-256":
		h := sha256.Sum256(b)
		//return ecdsa.SignASN1(rand.Reader, k.Raw.(*ecdsa.PrivateKey), h[:])
		r, s, err := ecdsa.Sign(rand.Reader, k.Raw.(*ecdsa.PrivateKey), h[:])
		if err != nil {
			return nil, err
		}

		return append(r.Bytes(), s.Bytes()...), nil
	default:
		return nil, fmt.Errorf("unsupported key type: %s", k.Type)
	}
}

func (k *Key) DID() string {
	var buf []byte
	switch k.Type {
	case "ed25519":
		kb := k.Raw.(ed25519.PrivateKey)
		buf := make([]byte, 8+len(kb))
		n := varint.PutUvarint(buf, MCed25519)
		copy(buf[n:], kb)
		buf = buf[:n+len(kb)]
	case "P-256":
		sk := k.Raw.(*ecdsa.PrivateKey)
		enc := elliptic.MarshalCompressed(elliptic.P256(), sk.X, sk.Y)

		buf = make([]byte, 8+len(enc))
		n := varint.PutUvarint(buf, MCP256)
		copy(buf[n:], enc)
		buf = buf[:n+len(enc)]
	default:
		return "<invalid key type>"
	}

	kstr, err := multibase.Encode(multibase.Base58BTC, buf)
	if err != nil {
		panic(err)
	}

	return "did:key:" + kstr
}

func (k *Key) RawBytes() ([]byte, error) {
	switch k.Type {
	case "ed25519":
		return k.Raw.([]byte), nil
	case "P-256":
		b, err := x509.MarshalECPrivateKey(k.Raw.(*ecdsa.PrivateKey))
		if err != nil {
			return nil, err
		}

		return b, nil
	default:
		return nil, fmt.Errorf("unsupported key type: %q", k.Type)
	}
}
