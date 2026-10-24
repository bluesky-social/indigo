package spaces

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type credentialFailedPublicKey struct {
	atcrypto.PrivateKey
	err error
}

func (s *credentialFailedPublicKey) PublicKey() (atcrypto.PublicKey, error) {
	return nil, s.err
}

func TestNewCredentialRequiresP256ButDoesNotIssueOrCertifyToken(t *testing.T) {
	signer := clientTestSigner(t)
	cred, err := NewCredential("not-a-jwt-but-already-issued", signer)
	require.NoError(t, err)
	assert.Equal(t, "not-a-jwt-but-already-issued", cred.Token())
	assert.Same(t, signer, cred.signer, "an opaque signer must not be replaced or exported")
	_, err = NewCredential("token", nil)
	assert.Error(t, err)
	_, err = NewCredential("", signer)
	assert.Error(t, err)
	_, err = NewCredential("token\nsecond-header", signer)
	assert.Error(t, err)
	k256, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	_, err = NewCredential("token", &clientOpaqueSigner{PrivateKey: k256})
	assert.Error(t, err)
	keyErr := errors.New("opaque signer unavailable")
	_, err = NewCredential("token", &credentialFailedPublicKey{err: keyErr})
	assert.ErrorIs(t, err, keyErr)
	_, err = NewCredential("token", &credentialFailedPublicKey{})
	assert.Error(t, err)
}

func TestExplicitDelegationAndExchangeUseCallerClientsAndSigners(t *testing.T) {
	ctx := context.Background()
	ref := clientTestRef()
	signer := clientTestSigner(t)
	public, err := signer.PublicKey()
	require.NoError(t, err)
	secondSigner := clientTestSigner(t)
	secondPublic, err := secondSigner.PublicKey()
	require.NoError(t, err)
	var pdsCalls, exchanges atomic.Int64
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		pdsCalls.Add(1)
		assert.Equal(t, "/xrpc/com.atproto.space.getDelegationToken", req.URL.Path)
		assert.Equal(t, "Bearer oauth-access", req.Header.Get("Authorization"))
		assert.Empty(t, req.Header.Get("Signature"), "delegation uses the existing PDS client's authentication")
		assert.Equal(t, ref.String(), req.URL.Query().Get("space"))
		io.WriteString(w, `{"token":"delegation-token"}`)
	}))
	defer pds.Close()
	attestation := "caller-created-client-attestation"
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		call := exchanges.Add(1)
		requestPublic := public
		if call > 1 {
			requestPublic = secondPublic
		}
		assert.Equal(t, "/xrpc/com.atproto.space.getSpaceCredential", req.URL.Path)
		assert.Equal(t, "Bearer delegation-token", req.Header.Get("Authorization"))
		assert.Empty(t, req.Header.Get("Atproto-Space-Audience"), "issuance covers only authorization")
		assert.Equal(t, "snapshot", req.Header.Get("X-Caller"))
		params := `("authorization");keyid="` + requestPublic.DIDKey() + `"`
		assert.Equal(t, "atproto-space="+params, req.Header.Get("Signature-Input"))
		base := fmt.Sprintf("\"authorization\": Bearer delegation-token\n\"@signature-params\": %s", params)
		assert.NoError(t, clientVerifySignature(req.Header.Get("Signature"), requestPublic, base))
		var input map[string]json.RawMessage
		assert.NoError(t, json.NewDecoder(req.Body).Decode(&input))
		assert.JSONEq(t, fmt.Sprintf("%q", ref.String()), string(input["space"]))
		if call == 1 {
			assert.NotContains(t, input, "clientAttestation", "omitted attestation must remain omitted")
		} else {
			assert.JSONEq(t, fmt.Sprintf("%q", attestation), string(input["clientAttestation"]))
		}
		fmt.Fprintf(w, `{"credential":"issued-%d"}`, call)
	}))
	defer authority.Close()
	pdsClient := atclient.NewAPIClient(pds.URL)
	pdsClient.Client = pds.Client()
	pdsClient.Headers.Set("Authorization", "Bearer oauth-access")
	token, err := GetDelegationToken(ctx, pdsClient, ref)
	require.NoError(t, err)
	assert.Equal(t, "delegation-token", token)
	assert.Equal(t, "Bearer oauth-access", pdsClient.Headers.Get("Authorization"), "caller client is unchanged")
	headers := http.Header{"X-Caller": {"snapshot"}, "Atproto-Space-Audience": {"did:web:stale.example"}}
	client := NewClient(ClientOptions{Directory: clientTestDirectory(authority.URL), HTTPClient: authority.Client(), Headers: headers})
	first, err := client.ExchangeCredential(ctx, ref, token, signer, CredentialExchangeOptions{})
	require.NoError(t, err)
	headers.Set("X-Caller", "mutated")
	second, err := client.ExchangeCredential(ctx, ref, token, secondSigner, CredentialExchangeOptions{ClientAttestation: &attestation})
	require.NoError(t, err)
	assert.Equal(t, "issued-1", first.Token())
	assert.Equal(t, "issued-2", second.Token())
	assert.Same(t, signer, first.signer)
	assert.Same(t, secondSigner, second.signer)
	assert.EqualValues(t, 1, pdsCalls.Load(), "exchange must not fetch delegation itself")
	assert.EqualValues(t, 2, exchanges.Load())
}

func TestExchangeNamedFailureDoesNotRetryOrReplaceCredential(t *testing.T) {
	cred, _ := clientTestCredential(t, "existing-token")
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":"AppNotAuthorized","message":"caller must decide"}`)
	}))
	defer server.Close()
	client := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL), HTTPClient: server.Client()})
	space := client.Space(clientTestRef(), cred)
	_, err := client.ExchangeCredential(context.Background(), clientTestRef(), "delegation-token", clientTestSigner(t), CredentialExchangeOptions{})
	var apiErr *atclient.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, "AppNotAuthorized", apiErr.Name)
	assert.Equal(t, "existing-token", space.credential.Token())
	assert.EqualValues(t, 1, calls.Load())
	_, err = client.ExchangeCredential(context.Background(), clientTestRef(), "delegation-token", nil, CredentialExchangeOptions{})
	assert.Error(t, err)
	assert.EqualValues(t, 1, calls.Load(), "invalid signer fails before issuance")
}

func TestNotifyRegistrationUsesServiceIdentifiersAndReturnsExpiry(t *testing.T) {
	ctx := context.Background()
	cred, public := clientTestCredential(t, "issued-token")
	ref := clientTestRef()
	expires := "2026-10-06T12:34:56.123Z"
	var calls atomic.Int64
	var malformedExpiry atomic.Bool
	services := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		assert.NoError(t, clientVerifyRequest(req, public, cred.Token(), ref.SpaceAuthority()))
		var body struct {
			Space   string `json:"space"`
			Service string `json:"service"`
		}
		assert.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		assert.Equal(t, ref.String(), body.Space)
		services <- body.Service
		switch req.URL.Path {
		case "/xrpc/com.atproto.space.registerNotify":
			if malformedExpiry.Load() {
				io.WriteString(w, `{"expiresAt":"never"}`)
			} else {
				fmt.Fprintf(w, `{"expiresAt":%q}`, expires)
			}
		case "/xrpc/com.atproto.space.unregisterNotify":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected endpoint %s", req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	space := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL), HTTPClient: server.Client()}).Space(ref, cred)
	for _, service := range []string{"did:web:syncer.example", "did:web:syncer.example#atproto_space_syncer"} {
		expiry, err := space.RegisterNotify(ctx, service)
		require.NoError(t, err)
		assert.Equal(t, syntax.Datetime(expires), expiry)
		assert.Equal(t, service, <-services)
		require.NoError(t, space.UnregisterNotify(ctx, service))
		assert.Equal(t, service, <-services)
	}
	assert.EqualValues(t, 4, calls.Load(), "expiry must not schedule automatic renewal")
	for _, service := range []string{"https://syncer.example/callback", "did:web:syncer.example#", "did:web:syncer.example#one#two"} {
		_, err := space.RegisterNotify(ctx, service)
		assert.Error(t, err)
		assert.Error(t, space.UnregisterNotify(ctx, service))
	}
	assert.EqualValues(t, 4, calls.Load(), "invalid service references fail before network requests")
	malformedExpiry.Store(true)
	_, err := space.RegisterNotify(ctx, "did:web:syncer.example")
	assert.Error(t, err)
	<-services
}

func TestVerifyNotificationAuthRequiresIssuerAudienceAndEndpoint(t *testing.T) {
	ctx := context.Background()
	issuer, otherIssuer := syntax.DID("did:web:authority.example"), syntax.DID("did:web:unrelated.example")
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	otherKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := key.PublicKey()
	require.NoError(t, err)
	otherPub, err := otherKey.PublicKey()
	require.NoError(t, err)
	dir := identity.NewMockDirectory()
	for _, entry := range []struct {
		did syntax.DID
		pub atcrypto.PublicKey
	}{{issuer, pub}, {otherIssuer, otherPub}} {
		dir.Insert(identity.Identity{DID: entry.did, Handle: syntax.HandleInvalid, Keys: map[string]identity.VerificationMethod{
			"atproto": {Type: "Multikey", PublicKeyMultibase: entry.pub.Multibase()},
		}})
	}
	audience := "did:web:syncer.example#atproto_space_syncer"
	endpoint := syntax.NSID("com.atproto.space.notifySpaceDeleted")
	otherEndpoint := syntax.NSID("com.atproto.space.notifyWrite")
	validator := &auth.ServiceAuthValidator{Audience: audience, Dir: dir}
	cases := []struct {
		name     string
		issuer   syntax.DID
		audience string
		endpoint *syntax.NSID
		ttl      time.Duration
		key      atcrypto.PrivateKey
		valid    bool
	}{
		{"matching notification", issuer, audience, &endpoint, time.Minute, key, true},
		{"other valid service issuer", otherIssuer, audience, &endpoint, time.Minute, otherKey, false},
		{"wrong audience", issuer, "did:web:elsewhere.example#atproto_space_syncer", &endpoint, time.Minute, key, false},
		{"wrong endpoint", issuer, audience, &otherEndpoint, time.Minute, key, false},
		{"missing endpoint", issuer, audience, nil, time.Minute, key, false},
		{"expired notification", issuer, audience, &endpoint, -time.Minute, key, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token, err := auth.SignServiceAuth(tc.issuer, tc.audience, tc.ttl, tc.endpoint, tc.key)
			require.NoError(t, err)
			err = VerifyNotificationAuth(ctx, validator, token, endpoint, issuer)
			if tc.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				if tc.issuer == otherIssuer {
					assert.ErrorIs(t, err, jwt.ErrTokenInvalidIssuer, "an arbitrary valid service JWT is insufficient")
				}
			}
		})
	}
	assert.Error(t, VerifyNotificationAuth(ctx, nil, "token", endpoint, issuer))
	assert.Error(t, VerifyNotificationAuth(ctx, &auth.ServiceAuthValidator{Dir: dir}, "token", endpoint, issuer))
	assert.Error(t, VerifyNotificationAuth(ctx, validator, "token", endpoint, ""))
}

func TestIndependentHTTPSignatureVerifierAcceptsHighS(t *testing.T) {
	signer := clientTestSigner(t)
	public, err := signer.PublicKey()
	require.NoError(t, err)
	base := "\"authorization\": Atproto-Space token\n\"atproto-space-audience\": did:web:author.example\n\"@signature-params\": (\"authorization\" \"atproto-space-audience\")"
	sig, err := signer.HashAndSign([]byte(base))
	require.NoError(t, err)
	lowS := new(big.Int).SetBytes(sig[32:])
	// P-256 group order, independent of Indigo's verifier.
	order, ok := new(big.Int).SetString("ffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551", 16)
	require.True(t, ok)
	highS := new(big.Int).Sub(order, lowS)
	highS.FillBytes(sig[32:])
	header := "atproto-space=:" + base64.StdEncoding.EncodeToString(sig) + ":"
	assert.NoError(t, clientVerifySignature(header, public, base))
	assert.Error(t, clientVerifySignature(header, public, strings.ReplaceAll(base, "\n", `\n`)), "literal backslash-n is not the signature base")
	assert.Error(t, clientVerifySignature(header, public, strings.Replace(base, `("authorization" "atproto-space-audience")`, `("atproto-space-audience" "authorization")`, 1)), "component order is exact")
}

// Keep credential-auth failures visible instead of silently exchanging a token.
func TestExpiredCredentialReadDoesNotRefresh(t *testing.T) {
	cred, _ := clientTestCredential(t, "expired-token")
	author := syntax.DID("did:web:alice.example")
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/xrpc/com.atproto.space.listBlobs", req.URL.Path)
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"ExpiredToken"}`)
	}))
	defer server.Close()
	repo := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: server.Client()}).Space(clientTestRef(), cred).Repo(author)
	_, err := repo.ListBlobs(context.Background(), ListBlobsOptions{})
	var apiErr *atclient.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "ExpiredToken", apiErr.Name)
	assert.EqualValues(t, 1, calls.Load())
	assert.Equal(t, "expired-token", cred.Token())
}

func TestCredentialSignatureOverwritesAllAuthHeaderValues(t *testing.T) {
	cred, public := clientTestCredential(t, "issued-token")
	author := syntax.DID("did:web:alice.example")
	req, err := http.NewRequest(http.MethodGet, "https://unused.example", nil)
	require.NoError(t, err)
	req.Header["Authorization"] = []string{"first-stale", "second-stale"}
	req.Header["Atproto-Space-Audience"] = []string{"did:web:wrong.example", "did:web:also-wrong.example"}
	transport := credentialCaptureTransport{capture: func(r *http.Request) {
		assert.Len(t, r.Header.Values("Authorization"), 1)
		assert.Len(t, r.Header.Values("Atproto-Space-Audience"), 1)
		assert.NoError(t, clientVerifyRequest(r, public, cred.Token(), author))
	}}
	a := &credentialAuth{credential: cred, audience: author}
	resp, err := a.DoWithAuth(&http.Client{Transport: transport}, req, "com.atproto.space.getRepo")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

type credentialCaptureTransport struct {
	capture func(*http.Request)
}

func (tr credentialCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr.capture(req)
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
}
