package spaces

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/dunglas/httpsfv"
	"github.com/ipfs/go-cid"
	mh "github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const clientRawRecord = `{"$type": "org.example.future", "integer": 9007199254740993, "unknown": [true, {"next": "value"}]}`

func clientTestRef() syntax.SpaceRef {
	return syntax.NewSpaceRef("did:web:authority.example", "org.example.space", "self")
}

func clientTestCID(t *testing.T, codec uint64) cid.Cid {
	t.Helper()
	c, err := (cid.Prefix{Version: 1, Codec: codec, MhType: mh.SHA2_256, MhLength: 32}).Sum([]byte("client-test-content"))
	require.NoError(t, err)
	return c
}

func clientTestSigner(t *testing.T) atcrypto.PrivateKey {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	return &clientOpaqueSigner{PrivateKey: key}
}

// No private-key export method is available through this implementation.
type clientOpaqueSigner struct {
	atcrypto.PrivateKey
}

func clientTestCredential(t *testing.T, token string) (*Credential, atcrypto.PublicKey) {
	t.Helper()
	signer := clientTestSigner(t)
	cred, err := NewCredential(token, signer)
	require.NoError(t, err)
	pub, err := signer.PublicKey()
	require.NoError(t, err)
	return cred, pub
}

func clientTestDirectory(host string, authors ...syntax.DID) *identity.MockDirectory {
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: clientTestRef().SpaceAuthority(), Handle: syntax.HandleInvalid,
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: host},
		},
	})
	public, err := atcrypto.ParsePublicUncompressedBytesP256(clientKEMKey.PublicKey().Bytes())
	if err != nil {
		panic(err)
	}
	for _, author := range authors {
		doc := identity.DIDDocument{
			DID: author,
			VerificationMethod: []identity.DocVerificationMethod{{
				ID: author.String() + "#atproto_kem", Controller: author.String(),
				Type: "Multikey", PublicKeyMultibase: public.Multibase(),
			}},
			Service: []identity.DocService{{ID: "#atproto_pds", Type: "AtprotoPersonalDataServer", ServiceEndpoint: host}},
		}
		dir.Insert(identity.ParseIdentity(&doc))
	}
	return dir
}

func clientTestCommitJSON() string {
	return fmt.Sprintf(`{"ver":1,"repo":"did:web:alice.example","space":%q,"rev":%q,"data":{"$bytes":%q}}`,
		clientTestRef(), syntax.NewTIDFromInteger(42), base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
}

var clientKEMKey = func() *ecdh.PrivateKey {
	var scalar [32]byte
	scalar[31] = 7
	key, err := ecdh.P256().NewPrivateKey(scalar[:])
	if err != nil {
		panic(err)
	}
	return key
}()

// The fixture uses a host-side ECDH exchange and a direct two-step HKDF rather
// than the client's shared-secret helper or its MAC context builders.
func clientSetMAC(t *testing.T, w http.ResponseWriter, req *http.Request, kind string, message []byte) {
	t.Helper()
	dictionary, err := httpsfv.UnmarshalDictionary(req.Header.Values("Atproto-Space-Want-Mac"))
	require.NoError(t, err)
	typeMember, ok := dictionary.Get("type")
	require.True(t, ok)
	assert.Equal(t, kind, typeMember.(httpsfv.Item).Value)
	version, ok := dictionary.Get("ver")
	require.True(t, ok)
	assert.Equal(t, int64(1), version.(httpsfv.Item).Value)
	shareMember, ok := dictionary.Get("key_share")
	require.True(t, ok)
	share, err := atcrypto.ParsePublicDIDKey(shareMember.(httpsfv.Item).Value.(string))
	require.NoError(t, err)
	public, err := ecdh.P256().NewPublicKey(share.UncompressedBytes())
	require.NoError(t, err)
	dh, err := clientKEMKey.ECDH(public)
	require.NoError(t, err)
	extract := hmac.New(sha256.New, make([]byte, 32))
	extract.Write(append([]byte("HPKE-v1KEM\x00\x10eae_prk"), dh...))
	info := append([]byte("\x00\x20HPKE-v1KEM\x00\x10shared_secret"), public.Bytes()...)
	info = append(info, clientKEMKey.PublicKey().Bytes()...)
	expand := hmac.New(sha256.New, extract.Sum(nil))
	expand.Write(append(info, 1))
	mac := hmac.New(sha256.New, expand.Sum(nil))
	mac.Write(message)
	w.Header().Set("Atproto-Space-Mac", `mac=:`+base64.StdEncoding.EncodeToString(mac.Sum(nil))+`: , ver=1, type="`+kind+`"`)
	w.Header().Set("Vary", "Atproto-Space-Want-Mac")
}

func clientCommitContext() []byte {
	message := []byte("atproto-space-repo-v1")
	for _, field := range []string{clientTestRef().String(), "did:web:alice.example", syntax.NewTIDFromInteger(42).String()} {
		message = binary.BigEndian.AppendUint16(message, uint16(len(field)))
		message = append(message, field...)
	}
	return append(message, bytes.Repeat([]byte{1}, 32)...)
}

func clientRecordContext(uri syntax.SpaceURI, recordCID cid.Cid) []byte {
	message := []byte("atproto-space-record-v1")
	message = binary.BigEndian.AppendUint16(message, uint16(len(uri)))
	message = append(message, uri...)
	return append(message, recordCID.Bytes()...)
}

// Independent RFC 9421 verifier: this deliberately does not use the client's
// signature-base builder or its crypto verification method. Standard ECDSA
// verification accepts both low-S and high-S signatures as required here.
func clientVerifyRequest(req *http.Request, public atcrypto.PublicKey, token string, audience syntax.DID) error {
	wantAuth := "Atproto-Space-Credential " + token
	if req.Header.Get("Authorization") != wantAuth || req.Header.Get("Atproto-Space-Audience") != audience.String() {
		return fmt.Errorf("wrong authorization or audience: %q / %q", req.Header.Get("Authorization"), req.Header.Get("Atproto-Space-Audience"))
	}
	const params = `("authorization" "atproto-space-audience");tag="atproto-space-credential"`
	if req.Header.Get("Signature-Input") != "atproto-space-credential="+params {
		return fmt.Errorf("wrong signature components: %q", req.Header.Get("Signature-Input"))
	}
	base := fmt.Sprintf("\"authorization\": %s\n\"atproto-space-audience\": %s\n\"@signature-params\": %s", wantAuth, audience, params)
	return clientVerifySignature(req.Header.Get("Signature"), public, base)
}

func clientVerifySignature(header string, public atcrypto.PublicKey, base string) error {
	if !strings.HasPrefix(header, "atproto-space-credential=:") || !strings.HasSuffix(header, ":") {
		return fmt.Errorf("malformed Signature header: %q", header)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(header, "atproto-space-credential=:"), ":"))
	if err != nil || len(sig) != 64 {
		return fmt.Errorf("invalid signature bytes: %v (length %d)", err, len(sig))
	}
	x, y := elliptic.UnmarshalCompressed(elliptic.P256(), public.Bytes())
	if x == nil {
		return fmt.Errorf("not a P-256 public key")
	}
	hash := sha256.Sum256([]byte(base))
	if !ecdsa.Verify(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, hash[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		return fmt.Errorf("signature does not verify against independent newline-delimited base")
	}
	return nil
}

type clientCountingDirectory struct {
	identity.Directory
	lookups atomic.Int64
}

func (d *clientCountingDirectory) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	d.lookups.Add(1)
	return d.Directory.LookupDID(ctx, did)
}

func TestClientRoutesSpaceAndSharedPDSAuthorsWithoutIssuance(t *testing.T) {
	ctx := context.Background()
	ref := clientTestRef()
	authorA := syntax.DID("did:web:alice.example")
	authorB := syntax.DID("did:web:bob.example")
	cred, pub := clientTestCredential(t, "already-issued-not-validated-as-a-jwt")
	recordCID := clientTestCID(t, cid.DagCBOR)
	var authorityCalls, repoCalls atomic.Int64
	authority := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authorityCalls.Add(1)
		assert.Equal(t, "/xrpc/com.atproto.space.listRepos", req.URL.Path)
		assert.Equal(t, ref.String(), req.URL.Query().Get("space"))
		assert.NoError(t, clientVerifyRequest(req, pub, cred.Token(), ref.SpaceAuthority()))
		assert.Equal(t, "before", req.Header.Get("X-Snapshot"))
		fmt.Fprintf(w, `{"repos":[{"did":%q,"repoRev":%q,"spaceRev":%q,"data":{"$bytes":%q}}],"cursor":"authority-next"}`,
			authorA, syntax.NewTIDFromInteger(42), syntax.NewTIDFromInteger(43), base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	}))
	defer authority.Close()
	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		repoCalls.Add(1)
		assert.Equal(t, "/xrpc/com.atproto.space.getRecord", req.URL.Path)
		author := syntax.DID(req.URL.Query().Get("repo"))
		assert.Contains(t, []syntax.DID{authorA, authorB}, author)
		assert.NoError(t, clientVerifyRequest(req, pub, cred.Token(), author))
		assert.Equal(t, "before", req.Header.Get("X-Snapshot"))
		assert.Equal(t, "org.example.record", req.URL.Query().Get("collection"))
		assert.Equal(t, "record-key", req.URL.Query().Get("rkey"))
		uri := syntax.NewSpaceURI(ref, author, "org.example.record", "record-key")
		clientSetMAC(t, w, req, "atproto-space-record", clientRecordContext(uri, recordCID))
		fmt.Fprintf(w, `{"uri":%q,"cid":%q,"value":%s}`, uri, recordCID, clientRawRecord)
	}))
	defer pds.Close()
	dir := clientTestDirectory(pds.URL, authorA, authorB)
	dir.Insert(identity.Identity{DID: ref.SpaceAuthority(), Handle: syntax.HandleInvalid, Services: map[string]identity.ServiceEndpoint{
		"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: authority.URL},
	}})
	counting := &clientCountingDirectory{Directory: dir}
	headers := http.Header{
		"X-Snapshot": {"before"}, "Authorization": {"stale-client-auth"},
		"Atproto-Space-Audience": {"did:web:wrong.example"},
	}
	client := NewClient(ClientOptions{Directory: counting, HTTPClient: pds.Client(), Headers: headers})
	space := client.Space(ref, cred)
	repoA, repoB := space.Repo(authorA), space.Repo(authorB)
	require.Zero(t, counting.lookups.Load(), "constructors must not resolve identities")
	require.Zero(t, authorityCalls.Load()+repoCalls.Load(), "supplied credentials need no issuance")
	headers["X-Snapshot"][0] = "mutated-in-place"
	headers.Set("X-Snapshot", "after")
	page, err := space.ListRepos(ctx, ListReposOptions{})
	require.NoError(t, err)
	require.Len(t, page.Repos, 1)
	assert.Equal(t, authorA, page.Repos[0].Author)
	assert.Equal(t, syntax.NewTIDFromInteger(42), page.Repos[0].RepoRev)
	assert.Equal(t, syntax.NewTIDFromInteger(43), page.Repos[0].SpaceRev)
	require.NotNil(t, page.Cursor)
	assert.Equal(t, "authority-next", *page.Cursor)
	for _, repo := range []*Repo{repoA, repoB} {
		record, err := repo.GetRecord(ctx, "org.example.record", "record-key")
		require.NoError(t, err)
		assert.Equal(t, syntax.NewSpaceURI(ref, repo.author, "org.example.record", "record-key"), record.URI)
		assert.True(t, recordCID.Equals(record.CID))
		assert.Equal(t, clientRawRecord, string(record.Value), "unknown JSON must not be decoded or re-marshaled")
	}
	assert.EqualValues(t, 1, authorityCalls.Load())
	assert.EqualValues(t, 2, repoCalls.Load())
}

func TestResolutionOnlyHandles(t *testing.T) {
	ref := clientTestRef()
	author := syntax.DID("did:web:alice.example")
	dir := clientTestDirectory("https://pds.example", author)
	counting := &clientCountingDirectory{Directory: dir}
	space := NewClient(ClientOptions{Directory: counting}).Space(ref, nil)
	repo := space.Repo(author)
	require.Zero(t, counting.lookups.Load())
	host, err := space.ResolveHost(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://pds.example", host)
	host, err = repo.ResolveHost(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://pds.example", host)
	_, err = space.ListRepos(context.Background(), ListReposOptions{})
	assert.ErrorIs(t, err, ErrMissingCredential)
	_, err = repo.GetRecord(context.Background(), "org.example.record", "one")
	assert.ErrorIs(t, err, ErrMissingCredential)
	assert.EqualValues(t, 2, counting.lookups.Load(), "unauthenticated reads must fail before resolution")
}

func TestCredentialReplacementKeepsOldAndInFlightHandles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	author := syntax.DID("did:web:alice.example")
	oldCredential, oldPublic := clientTestCredential(t, "old-issued-token")
	newCredential, newPublic := clientTestCredential(t, "new-issued-token")
	recordCID := clientTestCID(t, cid.DagCBOR)
	started, release := make(chan struct{}), make(chan struct{})
	var oldCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var credential *Credential
		var public atcrypto.PublicKey
		switch req.Header.Get("Authorization") {
		case "Atproto-Space-Credential old-issued-token":
			credential, public = oldCredential, oldPublic
			if oldCalls.Add(1) == 1 {
				close(started)
				<-release
			}
		case "Atproto-Space-Credential new-issued-token":
			credential, public = newCredential, newPublic
		default:
			t.Errorf("unexpected authorization %q", req.Header.Get("Authorization"))
			http.Error(w, "wrong credential", http.StatusUnauthorized)
			return
		}
		assert.NoError(t, clientVerifyRequest(req, public, credential.Token(), author))
		clientSetMAC(t, w, req, "atproto-space-record", clientRecordContext(syntax.NewSpaceURI(clientTestRef(), author, "org.example.record", "one"), recordCID))
		fmt.Fprintf(w, `{"uri":%q,"cid":%q,"value":%s}`, syntax.NewSpaceURI(clientTestRef(), author, "org.example.record", "one"), recordCID, clientRawRecord)
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	client := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: server.Client()})
	oldSpace := client.Space(clientTestRef(), oldCredential)
	oldRepo := oldSpace.Repo(author)
	inFlight := make(chan error, 1)
	go func() {
		_, err := oldRepo.GetRecord(ctx, "org.example.record", "one")
		inFlight <- err
	}()
	select {
	case <-started:
	case err := <-inFlight:
		t.Fatalf("old credential request did not reach the server: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	newRepo := client.Space(clientTestRef(), newCredential).Repo(author)
	_, newErr := newRepo.GetRecord(ctx, "org.example.record", "one")
	close(release)
	require.NoError(t, newErr)
	require.NoError(t, <-inFlight)
	_, err := oldRepo.GetRecord(ctx, "org.example.record", "one")
	require.NoError(t, err)
	_, err = oldSpace.Repo(author).GetRecord(ctx, "org.example.record", "one")
	require.NoError(t, err)
	assert.Equal(t, "old-issued-token", oldCredential.Token())
	assert.EqualValues(t, 3, oldCalls.Load())
}

type clientTrackedBody struct {
	io.ReadCloser
	closes atomic.Int64
}

func (b *clientTrackedBody) Close() error {
	b.closes.Add(1)
	return b.ReadCloser.Close()
}

type clientTrackingTransport struct {
	base   http.RoundTripper
	bodies chan *clientTrackedBody
}

func (tr *clientTrackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := tr.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	body := &clientTrackedBody{ReadCloser: resp.Body}
	resp.Body = body
	tr.bodies <- body
	return resp, nil
}

func TestRawStreamsRetainNamedErrorsAndBodyOwnership(t *testing.T) {
	ctx := context.Background()
	author := syntax.DID("did:web:alice.example")
	cred, public := clientTestCredential(t, "issued-token")
	blobCID := clientTestCID(t, cid.Raw)
	var responseMode atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.NoError(t, clientVerifyRequest(req, public, cred.Token(), author))
		assert.Equal(t, author.String(), req.URL.Query().Get("repo"))
		assert.Equal(t, clientTestRef().String(), req.URL.Query().Get("space"))
		switch responseMode.Load() {
		case 1:
			w.WriteHeader(http.StatusGone)
			io.WriteString(w, `{"error":"SpaceDeleted","message":"deleted by authority"}`)
		case 2:
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"ExpiredToken","message":"replace explicitly"}`)
		case 3:
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, "not JSON")
		default:
			if strings.HasSuffix(req.URL.Path, "getRepo") {
				assert.Equal(t, "application/octet-stream", req.Header.Get("Accept"))
				clientSetMAC(t, w, req, "atproto-space-repo", clientCommitContext())
			} else {
				assert.Equal(t, blobCID.String(), req.URL.Query().Get("cid"))
			}
			io.WriteString(w, "raw-stream")
		}
	}))
	defer server.Close()
	transport := &clientTrackingTransport{base: server.Client().Transport, bodies: make(chan *clientTrackedBody, 1)}
	client := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: &http.Client{Transport: transport}})
	repo := client.Space(clientTestRef(), cred).Repo(author)
	for _, download := range []struct {
		name string
		get  func() (io.ReadCloser, error)
	}{
		{"repo", func() (io.ReadCloser, error) {
			download, err := repo.GetRepo(ctx)
			if err != nil {
				return nil, err
			}
			return download, nil
		}},
		{"blob", func() (io.ReadCloser, error) { return repo.GetBlob(ctx, blobCID) }},
	} {
		t.Run(download.name, func(t *testing.T) {
			responseMode.Store(0)
			stream, err := download.get()
			require.NoError(t, err)
			body := <-transport.bodies
			assert.Zero(t, body.closes.Load(), "caller owns successful stream")
			content, err := io.ReadAll(stream)
			require.NoError(t, err)
			assert.Equal(t, "raw-stream", string(content))
			assert.Zero(t, body.closes.Load(), "reading does not transfer close ownership")
			require.NoError(t, stream.Close())
			assert.EqualValues(t, 1, body.closes.Load())
			for _, failure := range []struct {
				mode   int64
				status int
				name   string
			}{
				{1, http.StatusGone, "SpaceDeleted"},
				{2, http.StatusUnauthorized, "ExpiredToken"},
				{3, http.StatusServiceUnavailable, ""},
			} {
				responseMode.Store(failure.mode)
				stream, err := download.get()
				assert.Nil(t, stream)
				var apiErr *atclient.APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, failure.status, apiErr.StatusCode)
				assert.Equal(t, failure.name, apiErr.Name)
				body := <-transport.bodies
				assert.EqualValues(t, 1, body.closes.Load(), "failure body must be closed")
			}
		})
	}
}

func TestPagedReadOptionsAndRawValues(t *testing.T) {
	ctx := context.Background()
	author := syntax.DID("did:web:alice.example")
	cred, public := clientTestCredential(t, "issued-token")
	recordCID, blobCID := clientTestCID(t, cid.DagCBOR), clientTestCID(t, cid.Raw)
	rev := syntax.NewTIDFromInteger(42)
	cursor, next, empty := "opaque/cursor?not-a-TID", "next", ""
	collection := syntax.NSID("org.example.record")
	limit, no, yes := int64(0), false, true
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		audience := author
		if strings.HasSuffix(req.URL.Path, "listRepos") {
			audience = clientTestRef().SpaceAuthority()
		}
		assert.NoError(t, clientVerifyRequest(req, public, cred.Token(), audience))
		requests <- req.Clone(context.Background())
		switch req.URL.Path {
		case "/xrpc/com.atproto.space.listRepos":
			io.WriteString(w, `{"repos":[],"cursor":"next"}`)
		case "/xrpc/com.atproto.space.getRepo":
			clientSetMAC(t, w, req, "atproto-space-repo", clientCommitContext())
			io.WriteString(w, "STAR bytes")
		case "/xrpc/com.atproto.space.listRepoOps":
			if req.URL.Query().Get("cursor") == "next" {
				clientSetMAC(t, w, req, "atproto-space-repo", clientCommitContext())
				fmt.Fprintf(w, `{"ops":[],"commit":%s}`, clientTestCommitJSON())
			} else {
				fmt.Fprintf(w, `{"ops":[{"rev":%q,"collection":%q,"rkey":"one","cid":%q,"prev":null,"value":%s},{"rev":%q,"collection":%q,"rkey":"two","cid":null,"prev":%q}],"cursor":"next"}`, rev, collection, recordCID, clientRawRecord, rev, collection, recordCID)
			}
		case "/xrpc/com.atproto.space.listRecords":
			fmt.Fprintf(w, `{"records":[{"collection":%q,"rkey":"one","cid":%q,"value":%s},{"collection":%q,"rkey":"two","cid":%q}],"cursor":"next"}`, collection, recordCID, clientRawRecord, collection, recordCID)
		case "/xrpc/com.atproto.space.listBlobs":
			fmt.Fprintf(w, `{"cids":[%q],"cursor":"next"}`, blobCID)
		case "/xrpc/com.atproto.space.getLatestCommit":
			clientSetMAC(t, w, req, "atproto-space-repo", clientCommitContext())
			fmt.Fprintf(w, `{"commit":%s}`, clientTestCommitJSON())
		default:
			t.Errorf("unexpected endpoint %s", req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	space := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: server.Client()}).Space(clientTestRef(), cred)
	repo := space.Repo(author)
	check := func(expected url.Values) {
		t.Helper()
		query := (<-requests).URL.Query()
		assert.Equal(t, expected, query)
	}
	base := func(repo bool) url.Values {
		q := url.Values{"space": {clientTestRef().String()}}
		if repo {
			q.Set("repo", author.String())
		}
		return q
	}
	_, err := space.ListRepos(ctx, ListReposOptions{})
	require.NoError(t, err)
	check(base(false))
	_, err = space.ListRepos(ctx, ListReposOptions{Cursor: &empty, Limit: &limit})
	require.NoError(t, err)
	q := base(false)
	q.Set("cursor", "")
	q.Set("limit", "0")
	check(q)
	stream, err := repo.GetRepo(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	check(base(true))
	ops, err := repo.ListOps(ctx, ListOpsOptions{Since: &rev, Cursor: &cursor, Limit: &limit, ExcludeValues: &no})
	require.NoError(t, err)
	q = base(true)
	q.Set("cursor", cursor)
	q.Set("limit", "0")
	q.Set("excludeValues", "false")
	check(q)
	require.Len(t, ops.Ops, 2)
	assert.Nil(t, ops.Head)
	assert.Equal(t, rev, ops.Ops[0].Revision)
	assert.NotNil(t, ops.Ops[0].CID)
	assert.Nil(t, ops.Ops[0].Prev)
	assert.Nil(t, ops.Ops[1].CID)
	require.NotNil(t, ops.Ops[1].Prev)
	assert.True(t, recordCID.Equals(*ops.Ops[1].Prev))
	assert.Nil(t, ops.Ops[1].Value, "superseded/deleted values may be absent")
	assert.Equal(t, clientRawRecord, string(ops.Ops[0].Value))
	require.NotNil(t, ops.Cursor)
	assert.Equal(t, "next", *ops.Cursor)
	for _, opts := range []ListOpsOptions{{}, {Since: &rev}, {Since: &rev, Cursor: &empty}} {
		_, err := repo.ListOps(ctx, opts)
		require.NoError(t, err)
		q := base(true)
		if opts.Cursor != nil {
			q.Set("cursor", *opts.Cursor)
		} else if opts.Since != nil {
			q.Set("since", rev.String())
		}
		check(q)
	}
	ops, err = repo.ListOps(ctx, ListOpsOptions{Cursor: &next})
	require.NoError(t, err)
	q = base(true)
	q.Set("cursor", "next")
	check(q)
	require.NotNil(t, ops.Head)
	assert.Equal(t, rev, ops.Head.Revision)
	assert.Nil(t, ops.Cursor)
	assert.EqualValues(t, 1, ops.Head.Version)
	assert.Equal(t, bytes.Repeat([]byte{1}, 32), ops.Head.Data[:])
	for _, opts := range []ListRecordsOptions{{}, {Collection: &collection, Cursor: &empty, Limit: &limit, Reverse: &no, ExcludeValues: &yes}} {
		page, err := repo.ListRecords(ctx, opts)
		require.NoError(t, err)
		q := base(true)
		if opts.Collection != nil {
			q.Set("collection", collection.String())
			q.Set("cursor", "")
			q.Set("limit", "0")
			q.Set("reverse", "false")
			q.Set("excludeValues", "true")
		}
		check(q)
		require.Len(t, page.Records, 2)
		assert.Equal(t, clientRawRecord, string(page.Records[0].Value))
		assert.Nil(t, page.Records[1].Value)
		require.NotNil(t, page.Cursor)
		assert.Equal(t, "next", *page.Cursor)
	}
	for _, opts := range []ListBlobsOptions{{}, {Since: &rev, Cursor: &empty, Limit: &limit}} {
		page, err := repo.ListBlobs(ctx, opts)
		require.NoError(t, err)
		q := base(true)
		if opts.Since != nil {
			q.Set("since", rev.String())
			q.Set("cursor", "")
			q.Set("limit", "0")
		}
		check(q)
		require.Len(t, page.CIDs, 1)
		assert.True(t, blobCID.Equals(page.CIDs[0]))
	}
	commit, err := repo.GetLatestCommit(ctx)
	require.NoError(t, err)
	check(base(true))
	assert.Equal(t, ops.Head, commit, "latest and optional head commits use the same typed conversion")
}

func TestGetRecordPreservesArbitraryUnknownJSON(t *testing.T) {
	author := syntax.DID("did:web:alice.example")
	cred, _ := clientTestCredential(t, "issued-token")
	recordCID := clientTestCID(t, cid.DagCBOR)
	values := []string{clientRawRecord, `[1, {"new": true}]`, `9007199254740993`, `"a string"`, `null`}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				clientSetMAC(t, w, req, "atproto-space-record", clientRecordContext(syntax.NewSpaceURI(clientTestRef(), author, "org.example.record", "one"), recordCID))
				fmt.Fprintf(w, `{"uri":%q,"cid":%q,"value":%s}`, syntax.NewSpaceURI(clientTestRef(), author, "org.example.record", "one"), recordCID, value)
			}))
			defer server.Close()
			repo := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: server.Client()}).Space(clientTestRef(), cred).Repo(author)
			record, err := repo.GetRecord(context.Background(), "org.example.record", "one")
			require.NoError(t, err)
			assert.Equal(t, value, string(record.Value))
		})
	}
}

func TestReadBoundariesRejectMalformedMetadataAndCIDProfiles(t *testing.T) {
	author := syntax.DID("did:web:alice.example")
	cred, _ := clientTestCredential(t, "issued-token")
	recordCID, blobCID := clientTestCID(t, cid.DagCBOR), clientTestCID(t, cid.Raw)
	validURI := syntax.NewSpaceURI(clientTestRef(), author, "org.example.record", "one")
	cases := []struct {
		name     string
		response string
		read     func(*Space, *Repo) error
	}{
		{"public URI", fmt.Sprintf(`{"uri":"at://did:web:alice.example/org.example.record/one","cid":%q,"value":{}}`, recordCID), func(_ *Space, r *Repo) error {
			_, e := r.GetRecord(context.Background(), "org.example.record", "one")
			return e
		}},
		{"partial URI", fmt.Sprintf(`{"uri":%q,"cid":%q,"value":{}}`, clientTestRef().String()+"/"+author.String(), recordCID), func(_ *Space, r *Repo) error {
			_, e := r.GetRecord(context.Background(), "org.example.record", "one")
			return e
		}},
		{"record raw codec", fmt.Sprintf(`{"uri":%q,"cid":%q,"value":{}}`, validURI, blobCID), func(_ *Space, r *Repo) error {
			_, e := r.GetRecord(context.Background(), "org.example.record", "one")
			return e
		}},
		{"invalid CID encoding", fmt.Sprintf(`{"uri":%q,"cid":"bzzzz","value":{}}`, validURI), func(_ *Space, r *Repo) error {
			_, e := r.GetRecord(context.Background(), "org.example.record", "one")
			return e
		}},
		{"listed record profile", fmt.Sprintf(`{"records":[{"collection":"org.example.record","rkey":"one","cid":%q}]}`, blobCID), func(_ *Space, r *Repo) error {
			_, e := r.ListRecords(context.Background(), ListRecordsOptions{})
			return e
		}},
		{"blob dag-cbor codec", fmt.Sprintf(`{"cids":[%q]}`, recordCID), func(_ *Space, r *Repo) error { _, e := r.ListBlobs(context.Background(), ListBlobsOptions{}); return e }},
		{"operation previous CID", fmt.Sprintf(`{"ops":[{"rev":%q,"collection":"org.example.record","rkey":"one","cid":null,"prev":%q}]}`, syntax.NewTIDFromInteger(42), blobCID), func(_ *Space, r *Repo) error { _, e := r.ListOps(context.Background(), ListOpsOptions{}); return e }},
		{"operation revision", `{"ops":[{"rev":"not-a-tid","collection":"org.example.record","rkey":"one","cid":null,"prev":null}]}`, func(_ *Space, r *Repo) error { _, e := r.ListOps(context.Background(), ListOpsOptions{}); return e }},
		{"discovery author", `{"repos":[{"did":"not-a-did","repoRev":"2222222222222","spaceRev":"2222222222222","data":{"$bytes":""}}]}`, func(s *Space, _ *Repo) error { _, e := s.ListRepos(context.Background(), ListReposOptions{}); return e }},
		{"discovery data", `{"repos":[{"did":"did:web:alice.example","repoRev":"2222222222222","spaceRev":"2222222222222","data":{"$bytes":""}}]}`, func(s *Space, _ *Repo) error { _, e := s.ListRepos(context.Background(), ListReposOptions{}); return e }},
		{"missing discovery repos", `{}`, func(s *Space, _ *Repo) error { _, e := s.ListRepos(context.Background(), ListReposOptions{}); return e }},
		{"null discovery repos", `{"repos":null}`, func(s *Space, _ *Repo) error { _, e := s.ListRepos(context.Background(), ListReposOptions{}); return e }},
		{"missing commit bytes", `{"commit":{"ver":1,"rev":"2222222222222"}}`, func(_ *Space, r *Repo) error { _, e := r.GetLatestCommit(context.Background()); return e }},
		{"unsupported commit version", `{"commit":` + strings.Replace(clientTestCommitJSON(), `"ver":1`, `"ver":2`, 1) + `}`, func(_ *Space, r *Repo) error { _, e := r.GetLatestCommit(context.Background()); return e }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { io.WriteString(w, tc.response) }))
			defer server.Close()
			space := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: server.Client()}).Space(clientTestRef(), cred)
			assert.Error(t, tc.read(space, space.Repo(author)))
		})
	}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { calls.Add(1) }))
	defer server.Close()
	repo := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: server.Client()}).Space(clientTestRef(), cred).Repo(author)
	_, err := repo.GetBlob(context.Background(), recordCID)
	assert.Error(t, err)
	assert.Zero(t, calls.Load(), "wrong blob profile must fail before a network request")
}

func TestJSONReadsKeepNamedAPIErrors(t *testing.T) {
	author := syntax.DID("did:web:alice.example")
	cred, _ := clientTestCredential(t, "issued-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusGone)
		io.WriteString(w, `{"error":"SpaceDeleted","message":"deleted"}`)
	}))
	defer server.Close()
	repo := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: server.Client()}).Space(clientTestRef(), cred).Repo(author)
	_, err := repo.ListRecords(context.Background(), ListRecordsOptions{})
	var apiErr *atclient.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, "SpaceDeleted", apiErr.Name)
	assert.Equal(t, http.StatusGone, apiErr.StatusCode)
	assert.Equal(t, "deleted", apiErr.Message)
}

func TestMACReadsRejectUnauthenticatedAndMisboundResponses(t *testing.T) {
	author := syntax.DID("did:web:alice.example")
	recordCID := clientTestCID(t, cid.DagCBOR)
	otherRecordCID, err := recordCID.Prefix().Sum([]byte("other-record-content"))
	require.NoError(t, err)
	uri := syntax.NewSpaceURI(clientTestRef(), author, "org.example.record", "one")
	recordBody := fmt.Sprintf(`{"uri":%q,"cid":%q,"value":%s}`, uri, recordCID, clientRawRecord)
	commitBody := `{"commit":` + clientTestCommitJSON() + `}`
	for _, tc := range []struct {
		name   string
		record bool
		body   string
		header *string
	}{
		{"missing header", true, recordBody, new("")},
		{"malformed dictionary", true, recordBody, new(`type="unterminated`)},
		{"wrong MAC type", true, recordBody, new(`type="atproto-space-repo", ver=1, mac=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:`)},
		{"unsupported version", true, recordBody, new(`type="atproto-space-record", ver=2, mac=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:`)},
		{"version is a string", true, recordBody, new(`type="atproto-space-record", ver="1", mac=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:`)},
		{"missing MAC", true, recordBody, new(`type="atproto-space-record", ver=1`)},
		{"short MAC", true, recordBody, new(`type="atproto-space-record", ver=1, mac=:AA==:`)},
		{"MAC is a string", true, recordBody, new(`type="atproto-space-record", ver=1, mac="not-bytes"`)},
		{"wrong record MAC", true, recordBody, new(`type="atproto-space-record", ver=1, mac=:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=:`)},
		{"record URI prefix", true, strings.Replace(recordBody, uri.String(), clientTestRef().String(), 1), nil},
		{"wrong record URI", true, strings.Replace(recordBody, "/one", "/other", 1), nil},
		{"wrong record CID", true, strings.Replace(recordBody, recordCID.String(), otherRecordCID.String(), 1), nil},
		{"wrong commit repo", false, strings.Replace(commitBody, author.String(), "did:web:other.example", 1), nil},
		{"wrong commit space", false, strings.Replace(commitBody, "/self", "/other", 1), nil},
		{"wrong commit digest", false, strings.Replace(commitBody, base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)), 1), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				kind, message := "atproto-space-repo", clientCommitContext()
				if tc.record {
					kind, message = "atproto-space-record", clientRecordContext(uri, recordCID)
				}
				clientSetMAC(t, w, req, kind, message)
				if tc.header != nil {
					w.Header().Del("Atproto-Space-Mac")
					if *tc.header != "" {
						w.Header().Set("Atproto-Space-Mac", *tc.header)
					}
				}
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			credential, _ := clientTestCredential(t, "issued")
			repo := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: server.Client()}).Space(clientTestRef(), credential).Repo(author)
			if tc.record {
				record, err := repo.GetRecord(context.Background(), "org.example.record", "one")
				require.Error(t, err)
				assert.Nil(t, record)
			} else {
				commit, err := repo.GetLatestCommit(context.Background())
				require.Error(t, err)
				assert.Nil(t, commit)
			}
		})
	}
}

func TestMACRequestsUseFreshKeysForEveryPageAndCall(t *testing.T) {
	author := syntax.DID("did:web:alice.example")
	seen := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		share := req.Header.Get("Atproto-Space-Want-Mac")
		assert.False(t, seen[share], "ephemeral key was reused across requests")
		seen[share] = true
		if strings.HasSuffix(req.URL.Path, "listRepoOps") && req.URL.Query().Get("cursor") == "" {
			io.WriteString(w, `{"ops":[],"cursor":"next"}`)
			return
		}
		clientSetMAC(t, w, req, "atproto-space-repo", clientCommitContext())
		if strings.HasSuffix(req.URL.Path, "listRepoOps") {
			fmt.Fprintf(w, `{"ops":[],"commit":%s}`, clientTestCommitJSON())
		} else {
			fmt.Fprintf(w, `{"commit":%s}`, clientTestCommitJSON())
		}
	}))
	defer server.Close()
	credential, _ := clientTestCredential(t, "issued")
	repo := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: server.Client()}).Space(clientTestRef(), credential).Repo(author)
	page, err := repo.ListOps(context.Background(), ListOpsOptions{})
	require.NoError(t, err)
	require.Equal(t, "next", *page.Cursor)
	assert.Nil(t, page.Head)
	page, err = repo.ListOps(context.Background(), ListOpsOptions{Cursor: page.Cursor})
	require.NoError(t, err)
	require.NotNil(t, page.Head)
	assert.Equal(t, author, page.Head.Repo)
	for range 2 {
		commit, err := repo.GetLatestCommit(context.Background())
		require.NoError(t, err)
		assert.Equal(t, page.Head, commit)
	}
}

func TestRepoMACHeaderFailureClosesResponseBody(t *testing.T) {
	author := syntax.DID("did:web:alice.example")
	credential, _ := clientTestCredential(t, "issued")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, "untrusted STAR bytes")
	}))
	defer server.Close()
	transport := &clientTrackingTransport{base: server.Client().Transport, bodies: make(chan *clientTrackedBody, 1)}
	repo := NewClient(ClientOptions{Directory: clientTestDirectory(server.URL, author), HTTPClient: &http.Client{Transport: transport}}).Space(clientTestRef(), credential).Repo(author)
	download, err := repo.GetRepo(context.Background())
	require.Error(t, err)
	assert.Nil(t, download)
	assert.EqualValues(t, 1, (<-transport.bodies).closes.Load())
}
