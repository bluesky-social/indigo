package spaces

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedSecretIndependentP256Vector(t *testing.T) {
	// Constructed outside Go with independent affine P-256 arithmetic and
	// HMAC-SHA256 Extract/Expand, using syncer scalar 3 and author scalar 7.
	decode := func(value string) []byte {
		t.Helper()
		data, err := hex.DecodeString(value)
		require.NoError(t, err)
		return data
	}
	dh := decode("3250fcf686637c7b2e4ac86eb473bca53a582139f42b1523fd76364e67399e83")
	syncer := decode("045ecbe4d1a6330a44c8f7ef951d4bf165e6c6b721efada985fb41661bc6e7fd6c8734640c4998ff7e374b06ce1a64a2ecd82ab036384fb83d9a79b127a27d5032")
	author := decode("048e533b6fa0bf7b4625bb30667c01fb607ef9f8b8a80fef5b300628703187b2a373eb1dbde03318366d069f83a6f5900053c73633cb041b21c55e1a86c1f400b4")
	secret, err := deriveSharedSecret(dh, syncer, author)
	require.NoError(t, err)
	require.Equal(t, "6d5583f6c99086921a36ba07358a13195fd4a1da7beb30eff853f05691108baf", hex.EncodeToString(secret[:]))
	// Swapping the public keys must not authenticate the same exchange.
	reversed, err := deriveSharedSecret(dh, author, syncer)
	require.NoError(t, err)
	require.NotEqual(t, secret, reversed)
}

// The response is signed without the production serializer or verifier.
func TestHTTPSignatureProfiles(t *testing.T) {
	secret := [32]byte{1, 2, 3}
	keyID := "did:key:test-request-key"
	kind := "atproto-space-commit"
	covered := `ver=1, repo="did:web:author.example", space="at://did:web:authority.example/space/com.example.space/skey", rev="3mtwwsccuqy22", data=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:`
	baseParams := `("atproto-space-commit";sf);tag="atproto-space-commit"`
	sign := func(params string) http.Header {
		base := `"atproto-space-commit";sf: ` + covered + "\n\"@signature-params\": " + params
		mac := hmac.New(sha256.New, secret[:])
		mac.Write([]byte(base))
		return http.Header{"Atproto-Space-Commit": {covered}, "Signature-Input": {"renamed=" + params}, "Signature": {"renamed=:" + base64.StdEncoding.EncodeToString(mac.Sum(nil)) + ":"}}
	}
	request := &macRequest{sharedSecret: secret, kind: kind, keyID: keyID}
	for _, tc := range []struct {
		name, params string
		mutate       func(http.Header)
		valid        bool
	}{
		{name: "arbitrary label", params: baseParams, valid: true},
		{name: "matching keyid", params: baseParams + `;keyid="` + keyID + `"`, valid: true},
		{name: "explicit algorithm", params: baseParams + `;alg="hmac-sha256"`, valid: true},
		{name: "other parameters are signed", params: baseParams + `;created=100;expires=200`, valid: true},
		{name: "normalized dictionary", params: baseParams, mutate: func(h http.Header) { h.Set(kind, strings.ReplaceAll(covered, ", ", ",")) }, valid: true},
		{name: "unrelated signatures", params: baseParams, mutate: func(h http.Header) {
			h.Add("Signature-Input", `other=("date");tag="another-protocol"`)
			h.Add("Signature", `other=:AA==:`)
		}, valid: true},
		{name: "wrong keyid", params: baseParams + `;keyid="other"`},
		{name: "wrong algorithm", params: baseParams + `;alg="ecdsa-p256-sha256"`},
		{name: "missing sf", params: `("atproto-space-commit");tag="atproto-space-commit"`},
		{name: "false sf", params: `("atproto-space-commit";sf=?0);tag="atproto-space-commit"`},
		{name: "extra component parameter", params: `("atproto-space-commit";sf;req);tag="atproto-space-commit"`},
		{name: "wrong covered header", params: `("date";sf);tag="atproto-space-commit"`},
		{name: "extra component", params: `("atproto-space-commit";sf "date");tag="atproto-space-commit"`},
		{name: "wrong tag", params: `("atproto-space-commit";sf);tag="atproto-space-record"`},
		{name: "missing tag", params: `("atproto-space-commit";sf)`},
		{name: "two matching signatures", params: baseParams, mutate: func(h http.Header) { h.Add("Signature-Input", "second="+baseParams) }},
		{name: "missing signature", params: baseParams, mutate: func(h http.Header) { h.Del("Signature") }},
		{name: "missing covered header", params: baseParams, mutate: func(h http.Header) { h.Del(kind) }},
		{name: "changed covered value", params: baseParams, mutate: func(h http.Header) { h.Set(kind, strings.Replace(covered, "ver=1", "ver=2", 1)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := sign(tc.params)
			if tc.mutate != nil {
				tc.mutate(headers)
			}
			_, err := request.verification(headers)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
