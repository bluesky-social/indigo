package spaces

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/dunglas/httpsfv"
	"github.com/ipfs/go-cid"
)

const (
	repoMACType       = "atproto-space-repo"
	recordMACType     = "atproto-space-record"
	wantMACHeader     = "Atproto-Space-Want-Mac"
	responseMACHeader = "Atproto-Space-Mac"
)

// MACVerification holds authentication material for one response. SharedSecret
// comes from a fresh ephemeral P-256 exchange with the author's #atproto_kem
// key. Keep it private and do not persist or log it. MACs authenticate a response
// to its reader, not to third parties, and are not part of serialized repo data.
type MACVerification struct {
	SharedSecret [32]byte
	MAC          [32]byte
}

type macRequest struct {
	sharedSecret [32]byte
	kind         string
	header       string
}

func newMACRequest(author *atcrypto.PublicKeyP256, kind string) (*macRequest, error) {
	if author == nil {
		return nil, fmt.Errorf("missing Spaces author KEM key")
	}
	if kind != repoMACType && kind != recordMACType {
		return nil, fmt.Errorf("unsupported Spaces MAC type: %s", kind)
	}
	authorBytes := author.UncompressedBytes()
	public, err := ecdh.P256().NewPublicKey(authorBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid Spaces author KEM key: %w", err)
	}
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	dh, err := ephemeral.ECDH(public)
	if err != nil {
		return nil, fmt.Errorf("establishing Spaces shared secret: %w", err)
	}
	syncerBytes := ephemeral.PublicKey().Bytes()
	secret, err := deriveSharedSecret(dh, syncerBytes, authorBytes)
	if err != nil {
		return nil, err
	}
	share, err := atcrypto.ParsePublicUncompressedBytesP256(syncerBytes)
	if err != nil {
		return nil, err
	}
	return &macRequest{
		sharedSecret: secret,
		kind:         kind,
		header:       `type="` + kind + `", ver=1, key_share="` + share.DIDKey() + `"`,
	}, nil
}

// RFC 9180 DHKEM(P-256, HKDF-SHA256), with the syncer's public key first.
func deriveSharedSecret(dh, syncerPublic, authorPublic []byte) ([32]byte, error) {
	const prefix = "HPKE-v1KEM\x00\x10"
	input := make([]byte, 0, len(prefix)+len("eae_prk")+len(dh))
	input = append(input, prefix...)
	input = append(input, "eae_prk"...)
	input = append(input, dh...)
	info := make([]byte, 0, 2+len(prefix)+len("shared_secret")+len(syncerPublic)+len(authorPublic))
	info = binary.BigEndian.AppendUint16(info, 32)
	info = append(info, prefix...)
	info = append(info, "shared_secret"...)
	info = append(info, syncerPublic...)
	info = append(info, authorPublic...)
	key, err := hkdf.Key(sha256.New, input, nil, string(info), 32)
	if err != nil {
		return [32]byte{}, fmt.Errorf("deriving Spaces shared secret: %w", err)
	}
	return [32]byte(key), nil
}

func (request *macRequest) verification(headers http.Header) (*MACVerification, error) {
	values := headers.Values(responseMACHeader)
	if len(values) == 0 {
		return nil, fmt.Errorf("missing %s response header", responseMACHeader)
	}
	dictionary, err := httpsfv.UnmarshalDictionary(values)
	if err != nil {
		return nil, fmt.Errorf("invalid %s response header: %w", responseMACHeader, err)
	}
	typeItem, err := requiredMACItem(dictionary, "type")
	if err != nil {
		return nil, err
	}
	if kind, ok := typeItem.Value.(string); !ok || kind != request.kind {
		return nil, fmt.Errorf("unexpected Spaces response MAC type")
	}
	versionItem, err := requiredMACItem(dictionary, "ver")
	if err != nil {
		return nil, err
	}
	if version, ok := versionItem.Value.(int64); !ok || version != 1 {
		return nil, fmt.Errorf("unsupported Spaces response MAC version")
	}
	macItem, err := requiredMACItem(dictionary, "mac")
	if err != nil {
		return nil, err
	}
	mac, ok := macItem.Value.([]byte)
	if !ok || len(mac) != 32 {
		return nil, fmt.Errorf("Spaces response MAC must be a 32-byte sequence")
	}
	return &MACVerification{SharedSecret: request.sharedSecret, MAC: [32]byte(mac)}, nil
}

func requiredMACItem(dictionary *httpsfv.Dictionary, name string) (httpsfv.Item, error) {
	member, ok := dictionary.Get(name)
	if ok {
		if item, ok := member.(httpsfv.Item); ok {
			return item, nil
		}
	}
	return httpsfv.Item{}, fmt.Errorf("missing or invalid Spaces response MAC %s", name)
}

func verifyRecordMAC(uri syntax.SpaceURI, recordCID cid.Cid, authentication *MACVerification) error {
	if authentication == nil {
		return fmt.Errorf("missing Spaces record MAC authentication")
	}
	if len(uri) > 65535 {
		return fmt.Errorf("Spaces record URI exceeds MAC length limit")
	}
	mac := hmac.New(sha256.New, authentication.SharedSecret[:])
	_, _ = mac.Write([]byte("atproto-space-record-v1"))
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(uri)))
	_, _ = mac.Write(length[:])
	_, _ = mac.Write([]byte(uri))
	_, _ = mac.Write(recordCID.Bytes())
	var expected [32]byte
	mac.Sum(expected[:0])
	if !hmac.Equal(expected[:], authentication.MAC[:]) {
		return fmt.Errorf("Spaces record MAC mismatch")
	}
	return nil
}
