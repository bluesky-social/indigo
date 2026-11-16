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
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/dunglas/httpsfv"
	"github.com/ipfs/go-cid"
)

const (
	repoMACType   = "atproto-space-commit"
	recordMACType = "atproto-space-record"
	wantMACHeader = "Accept-Signature"
)

// MACVerification contains authentication material for one HTTP response. Use
// the value returned by GetRepo with VerifyRepo; it is not serialized in a repo.
// SharedSecret and MAC are sensitive and must not be persisted or logged.
type MACVerification struct {
	SharedSecret    [32]byte
	MAC             [32]byte
	kind            string
	header          string
	signatureParams string
}

type macRequest struct {
	sharedSecret [32]byte
	kind         string
	keyID        string
	header       string
}

func newMACRequest(author *atcrypto.PublicKeyP256, kind string) (*macRequest, error) {
	if author == nil {
		return nil, fmt.Errorf("missing Spaces author KEM key")
	}
	if kind != repoMACType && kind != recordMACType {
		return nil, fmt.Errorf("unsupported Spaces MAC type: %s", kind)
	}
	public, err := ecdh.P256().NewPublicKey(author.UncompressedBytes())
	if err != nil {
		return nil, err
	}
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	dh, err := ephemeral.ECDH(public)
	if err != nil {
		return nil, err
	}
	secret, err := deriveSharedSecret(dh, ephemeral.PublicKey().Bytes(), public.Bytes())
	if err != nil {
		return nil, err
	}
	share, err := atcrypto.ParsePublicUncompressedBytesP256(ephemeral.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	keyID := share.DIDKey()
	return &macRequest{sharedSecret: secret, kind: kind, keyID: keyID,
		header: `mac=("` + kind + `";sf);keyid="` + keyID + `";tag="` + kind + `"`}, nil
}

// RFC 9180 base mode, DHKEM(P-256, HKDF-SHA256), HKDF-SHA256, export-only AEAD.
func deriveSharedSecret(dh, syncerPublic, authorPublic []byte) ([32]byte, error) {
	const kem = "KEM\x00\x10"
	const suite = "HPKE\x00\x10\x00\x01\xff\xff"
	extract := func(id string, salt []byte, label string, input []byte) ([]byte, error) {
		return hkdf.Extract(sha256.New, append([]byte("HPKE-v1"+id+label), input...), salt)
	}
	expand := func(id string, key []byte, label string, info []byte) ([]byte, error) {
		labeled := binary.BigEndian.AppendUint16(nil, 32)
		labeled = append(labeled, []byte("HPKE-v1"+id+label)...)
		labeled = append(labeled, info...)
		return hkdf.Expand(sha256.New, key, string(labeled), 32)
	}
	eae, err := extract(kem, nil, "eae_prk", dh)
	if err != nil {
		return [32]byte{}, err
	}
	keys := append(append([]byte(nil), syncerPublic...), authorPublic...)
	kemSecret, err := expand(kem, eae, "shared_secret", keys)
	if err != nil {
		return [32]byte{}, err
	}
	pskIDHash, err := extract(suite, nil, "psk_id_hash", nil)
	if err != nil {
		return [32]byte{}, err
	}
	infoHash, err := extract(suite, nil, "info_hash", []byte("atproto-mac"))
	if err != nil {
		return [32]byte{}, err
	}
	context := append(append([]byte{0}, pskIDHash...), infoHash...)
	secret, err := extract(suite, kemSecret, "secret", nil)
	if err != nil {
		return [32]byte{}, err
	}
	exporter, err := expand(suite, secret, "exp", context)
	if err != nil {
		return [32]byte{}, err
	}
	exported, err := expand(suite, exporter, "sec", nil)
	if err != nil {
		return [32]byte{}, err
	}
	return [32]byte(exported), nil
}

func (request *macRequest) verification(headers http.Header) (*MACVerification, error) {
	inputs, err := httpsfv.UnmarshalDictionary(headers.Values("Signature-Input"))
	if err != nil {
		return nil, fmt.Errorf("invalid Signature-Input: %w", err)
	}
	var label string
	var selected httpsfv.InnerList
	for _, name := range inputs.Names() {
		member, _ := inputs.Get(name)
		list, ok := member.(httpsfv.InnerList)
		if !ok {
			continue
		}
		tag, _ := list.Params.Get("tag")
		if tag != request.kind {
			continue
		}
		if label != "" {
			return nil, fmt.Errorf("multiple Spaces MAC signatures with tag %s", request.kind)
		}
		label, selected = name, list
	}
	if label == "" {
		return nil, fmt.Errorf("missing Spaces MAC signature with tag %s", request.kind)
	}
	if len(selected.Items) != 1 || selected.Items[0].Value != request.kind {
		return nil, fmt.Errorf("incorrect Spaces MAC covered components")
	}
	sf, ok := selected.Items[0].Params.Get("sf")
	if !ok || sf != true || len(selected.Items[0].Params.Names()) != 1 {
		return nil, fmt.Errorf("Spaces MAC component requires only the sf flag")
	}
	if key, ok := selected.Params.Get("keyid"); ok && key != request.keyID {
		return nil, fmt.Errorf("Spaces MAC keyid does not match request")
	}
	if alg, ok := selected.Params.Get("alg"); ok && alg != "hmac-sha256" {
		return nil, fmt.Errorf("unsupported Spaces MAC algorithm")
	}
	paramsDict := httpsfv.NewDictionary()
	paramsDict.Add("sig", selected)
	encodedParams, err := httpsfv.Marshal(paramsDict)
	if err != nil {
		return nil, err
	}
	signatureParams := strings.TrimPrefix(encodedParams, "sig=")
	dictionary, err := httpsfv.UnmarshalDictionary(headers.Values(request.kind))
	if err != nil || len(dictionary.Names()) == 0 {
		return nil, fmt.Errorf("missing or invalid %s header", request.kind)
	}
	covered, err := httpsfv.Marshal(dictionary)
	if err != nil {
		return nil, err
	}
	signatures, err := httpsfv.UnmarshalDictionary(headers.Values("Signature"))
	if err != nil {
		return nil, fmt.Errorf("invalid Signature: %w", err)
	}
	value, _ := signatures.Get(label)
	item, ok := value.(httpsfv.Item)
	if !ok {
		return nil, fmt.Errorf("missing Spaces MAC signature value")
	}
	mac, ok := item.Value.([]byte)
	if !ok || len(mac) != sha256.Size || len(item.Params.Names()) != 0 {
		return nil, fmt.Errorf("Spaces response MAC must be a 32-byte sequence")
	}
	result := &MACVerification{SharedSecret: request.sharedSecret, MAC: [32]byte(mac), kind: request.kind, header: covered, signatureParams: signatureParams}
	if err := result.verify(); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *MACVerification) verify() error {
	if a == nil || a.kind == "" || a.header == "" || a.signatureParams == "" {
		return fmt.Errorf("missing Spaces response authentication")
	}
	base := `"` + a.kind + `";sf: ` + a.header + "\n\"@signature-params\": " + a.signatureParams
	mac := hmac.New(sha256.New, a.SharedSecret[:])
	_, _ = mac.Write([]byte(base))
	if !hmac.Equal(mac.Sum(nil), a.MAC[:]) {
		return fmt.Errorf("Spaces response MAC mismatch")
	}
	return nil
}

func requiredMACItem(dictionary *httpsfv.Dictionary, name string) (httpsfv.Item, error) {
	member, ok := dictionary.Get(name)
	if ok {
		if item, ok := member.(httpsfv.Item); ok {
			return item, nil
		}
	}
	return httpsfv.Item{}, fmt.Errorf("missing or invalid Spaces response field %s", name)
}

func (a *MACVerification) commit() (*Commit, error) {
	if err := a.verify(); err != nil {
		return nil, err
	}
	if a.kind != repoMACType {
		return nil, fmt.Errorf("expected Spaces commit MAC")
	}
	dictionary, err := httpsfv.UnmarshalDictionary([]string{a.header})
	if err != nil {
		return nil, err
	}
	stringValue := func(name string) (string, error) {
		item, err := requiredMACItem(dictionary, name)
		if err != nil {
			return "", err
		}
		value, ok := item.Value.(string)
		if !ok {
			return "", fmt.Errorf("invalid Spaces commit %s", name)
		}
		return value, nil
	}
	repo, err := stringValue("repo")
	if err != nil {
		return nil, err
	}
	space, err := stringValue("space")
	if err != nil {
		return nil, err
	}
	rev, err := stringValue("rev")
	if err != nil {
		return nil, err
	}
	ver, err := requiredMACItem(dictionary, "ver")
	if err != nil {
		return nil, err
	}
	version, ok := ver.Value.(int64)
	if !ok {
		return nil, fmt.Errorf("invalid Spaces commit version")
	}
	data, err := requiredMACItem(dictionary, "data")
	if err != nil {
		return nil, err
	}
	digest, ok := data.Value.([]byte)
	if !ok || len(digest) != 32 {
		return nil, fmt.Errorf("invalid Spaces commit digest")
	}
	commit := &Commit{Version: version, Repo: syntax.DID(repo), Space: syntax.SpaceRef(space), Revision: syntax.TID(rev), Data: [32]byte(digest)}
	return commit, commit.validate()
}

func verifyRecordMAC(uri syntax.SpaceURI, recordCID cid.Cid, authentication *MACVerification) error {
	if err := authentication.verify(); err != nil {
		return err
	}
	if authentication.kind != recordMACType {
		return fmt.Errorf("expected Spaces record MAC")
	}
	dictionary, err := httpsfv.UnmarshalDictionary([]string{authentication.header})
	if err != nil {
		return err
	}
	address, err := requiredMACItem(dictionary, "uri")
	if err != nil {
		return err
	}
	encodedCID, err := requiredMACItem(dictionary, "cid")
	if err != nil {
		return err
	}
	value, ok := encodedCID.Value.(string)
	if !ok {
		return fmt.Errorf("invalid Spaces record CID header")
	}
	actual, err := parseRecordCID(value)
	if err != nil {
		return err
	}
	if address.Value != uri.String() || !actual.Equals(recordCID) {
		return fmt.Errorf("Spaces record header does not match response")
	}
	return nil
}
