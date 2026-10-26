package spaces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
)

// Client provides stateless protocol operations. Consumers own credential
// replacement, pagination, checkpoints, scheduling, and retry policy.
type Client struct {
	directory  identity.Directory
	httpClient *http.Client
	headers    http.Header
}

// Space and its Repo handles retain the credential supplied at construction.
// Constructing either handle performs no resolution or credential exchange.
type Space struct {
	client     *Client
	ref        syntax.SpaceRef
	credential *Credential
}

type Repo struct {
	space  *Space
	author syntax.DID
}

// NewClient performs no network requests. Nil transport and directory options
// select Indigo's existing HTTP and identity defaults.
func NewClient(opts ClientOptions) *Client {
	base := atclient.NewAPIClient("")
	for name, values := range opts.Headers.Clone() {
		base.Headers[http.CanonicalHeaderKey(name)] = values
	}
	dir := opts.Directory
	if dir == nil {
		dir = identity.DefaultDirectory()
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = base.Client
	}
	return &Client{directory: dir, httpClient: httpClient, headers: base.Headers}
}

// Space accepts nil credentials for resolution-only use.
func (c *Client) Space(ref syntax.SpaceRef, credential *Credential) *Space {
	return &Space{client: c, ref: ref, credential: credential}
}

func (s *Space) Repo(author syntax.DID) *Repo {
	return &Repo{space: s, author: author}
}

func (c *Client) ResolveSpaceHost(ctx context.Context, ref syntax.SpaceRef) (string, error) {
	ident, err := c.directory.LookupDID(ctx, ref.SpaceAuthority())
	if err != nil {
		return "", err
	}
	host := ident.PDSEndpoint()
	if host == "" {
		return "", fmt.Errorf("space authority does not declare a PDS host: %s", ref.SpaceAuthority())
	}
	return host, nil
}

func (c *Client) ResolveRepoHost(ctx context.Context, author syntax.DID) (string, error) {
	ident, err := c.directory.LookupDID(ctx, author)
	if err != nil {
		return "", err
	}
	host := ident.PDSEndpoint()
	if host == "" {
		return "", fmt.Errorf("repo account does not declare a PDS host: %s", author)
	}
	return host, nil
}

func (s *Space) ResolveHost(ctx context.Context) (string, error) {
	return s.client.ResolveSpaceHost(ctx, s.ref)
}

func (r *Repo) ResolveHost(ctx context.Context) (string, error) {
	return r.space.client.ResolveRepoHost(ctx, r.author)
}

func (c *Client) apiClient(host string, auth atclient.AuthMethod) *atclient.APIClient {
	return &atclient.APIClient{
		Client: c.httpClient, Host: host, Headers: c.headers, Auth: auth,
	}
}

func (s *Space) authenticatedClient(ctx context.Context) (*atclient.APIClient, error) {
	if s.credential == nil || s.credential.signer == nil || s.credential.token == "" {
		return nil, ErrMissingCredential
	}
	host, err := s.ResolveHost(ctx)
	if err != nil {
		return nil, err
	}
	return s.client.apiClient(host, &credentialAuth{
		credential: s.credential, audience: s.ref.SpaceAuthority(),
	}), nil
}

func (r *Repo) authenticatedClient(ctx context.Context) (*atclient.APIClient, error) {
	rpc, _, err := r.requestClient(ctx, "")
	return rpc, err
}

func (r *Repo) requestClient(ctx context.Context, macType string) (*atclient.APIClient, *macRequest, error) {
	if r.space.credential == nil || r.space.credential.signer == nil || r.space.credential.token == "" {
		return nil, nil, ErrMissingCredential
	}
	ident, err := r.space.client.directory.LookupDID(ctx, r.author)
	if err != nil {
		return nil, nil, err
	}
	host := ident.PDSEndpoint()
	if host == "" {
		return nil, nil, fmt.Errorf("repo account does not declare a PDS host: %s", r.author)
	}
	var request *macRequest
	if macType != "" {
		key, err := ident.KEMPublicKey()
		if err != nil {
			return nil, nil, fmt.Errorf("resolving repo KEM key for %s: %w", r.author, err)
		}
		request, err = newMACRequest(key, macType)
		if err != nil {
			return nil, nil, err
		}
	}
	return r.space.client.apiClient(host, &credentialAuth{
		credential: r.space.credential, audience: r.author,
	}), request, nil
}

func (s *Space) ListRepos(ctx context.Context, opts ListReposOptions) (*RepoPage, error) {
	rpc, err := s.authenticatedClient(ctx)
	if err != nil {
		return nil, err
	}
	out, err := comatproto.SpaceListRepos(ctx, rpc, opts.Cursor, opts.Limit, s.ref.String())
	if err != nil {
		return nil, err
	}
	if out.Repos == nil {
		return nil, fmt.Errorf("repo discovery response is missing the required repos array")
	}
	page := &RepoPage{Repos: make([]RepoInfo, len(out.Repos)), Cursor: out.Cursor}
	for i, entry := range out.Repos {
		author, err := syntax.ParseDID(entry.Did)
		if err != nil {
			return nil, fmt.Errorf("invalid repo discovery author: %w", err)
		}
		repoRev, err := syntax.ParseTID(entry.RepoRev)
		if err != nil {
			return nil, fmt.Errorf("invalid repo discovery revision: %w", err)
		}
		spaceRev, err := syntax.ParseTID(entry.SpaceRev)
		if err != nil {
			return nil, fmt.Errorf("invalid space discovery revision: %w", err)
		}
		if len(entry.Data) != 32 {
			return nil, fmt.Errorf("repo discovery data must be 32 bytes, got %d", len(entry.Data))
		}
		page.Repos[i] = RepoInfo{
			Author: author, RepoRev: repoRev, SpaceRev: spaceRev, Data: [32]byte(entry.Data),
		}
	}
	return page, nil
}

// GetRepo returns a raw STAR stream and its response authentication material.
// Pass Authentication to VerifyRepo before accepting the archive. The caller
// owns the successful stream; failures close their bodies and retain APIError.
func (r *Repo) GetRepo(ctx context.Context) (*RepoDownload, error) {
	rpc, request, err := r.requestClient(ctx, repoMACType)
	if err != nil {
		return nil, err
	}
	req := r.streamRequest("com.atproto.space.getRepo", "application/octet-stream")
	req.Headers.Set(wantMACHeader, request.header)
	resp, err := doResponse(ctx, rpc, req)
	if err != nil {
		return nil, err
	}
	authentication, err := request.verification(resp.Header)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	return &RepoDownload{ReadCloser: resp.Body, Authentication: authentication}, nil
}

func (r *Repo) ListOps(ctx context.Context, opts ListOpsOptions) (*OpsPage, error) {
	req := r.streamRequest("com.atproto.space.listRepoOps", "application/json")
	// Transport cursors are opaque and take precedence over since revisions.
	if opts.Cursor != nil {
		req.QueryParams.Set("cursor", *opts.Cursor)
	} else if opts.Since != nil {
		req.QueryParams.Set("since", opts.Since.String())
	}
	if opts.Limit != nil {
		req.QueryParams.Set("limit", fmt.Sprint(*opts.Limit))
	}
	if opts.ExcludeValues != nil {
		req.QueryParams.Set("excludeValues", fmt.Sprint(*opts.ExcludeValues))
	}
	var out comatproto.SpaceListRepoOps_Output
	request, headers, err := r.getMACJSON(ctx, req, repoMACType, &out)
	if err != nil {
		return nil, err
	}
	page := &OpsPage{Ops: make([]Operation, len(out.Ops)), Cursor: out.Cursor}
	for i, entry := range out.Ops {
		rev, err := syntax.ParseTID(entry.Rev)
		if err != nil {
			return nil, fmt.Errorf("invalid operation revision: %w", err)
		}
		collection, err := syntax.ParseNSID(entry.Collection)
		if err != nil {
			return nil, fmt.Errorf("invalid operation collection: %w", err)
		}
		rkey, err := syntax.ParseRecordKey(entry.Rkey)
		if err != nil {
			return nil, fmt.Errorf("invalid operation record key: %w", err)
		}
		current, err := optionalRecordCID(entry.Cid)
		if err != nil {
			return nil, fmt.Errorf("invalid operation CID: %w", err)
		}
		prev, err := optionalRecordCID(entry.Prev)
		if err != nil {
			return nil, fmt.Errorf("invalid previous operation CID: %w", err)
		}
		page.Ops[i] = Operation{
			Revision: rev, Collection: collection, Rkey: rkey, CID: current, Prev: prev,
			Value: optionalRawValue(entry.Value),
		}
	}
	if out.Commit != nil {
		page.Head, err = commitFromWire(out.Commit)
		if err != nil {
			return nil, err
		}
		authentication, err := request.verification(headers)
		if err != nil {
			return nil, err
		}
		if err := page.Head.Verify(r.space.ref, r.author, authentication); err != nil {
			return nil, err
		}
	} else if len(headers.Values(responseMACHeader)) != 0 {
		return nil, fmt.Errorf("operation page without a commit has a MAC header")
	}
	return page, nil
}

func (r *Repo) GetRecord(ctx context.Context, collection syntax.NSID, rkey syntax.RecordKey) (*Record, error) {
	req := r.streamRequest("com.atproto.space.getRecord", "application/json")
	req.QueryParams.Set("collection", collection.String())
	req.QueryParams.Set("rkey", rkey.String())
	var out comatproto.SpaceGetRecord_Output
	request, headers, err := r.getMACJSON(ctx, req, recordMACType, &out)
	if err != nil {
		return nil, err
	}
	uri, err := syntax.ParseSpaceURI(out.Uri)
	if err != nil {
		return nil, fmt.Errorf("invalid returned space record URI: %w", err)
	}
	if uri != syntax.NewSpaceURI(r.space.ref, r.author, collection, rkey) {
		return nil, fmt.Errorf("returned space record URI does not match the request")
	}
	recordCID, err := parseRecordCID(out.Cid)
	if err != nil {
		return nil, fmt.Errorf("invalid returned record CID: %w", err)
	}
	authentication, err := request.verification(headers)
	if err != nil {
		return nil, err
	}
	if err := verifyRecordMAC(uri, recordCID, authentication); err != nil {
		return nil, err
	}
	return &Record{URI: uri, CID: recordCID, Value: out.Value}, nil
}

func (r *Repo) ListRecords(ctx context.Context, opts ListRecordsOptions) (*RecordPage, error) {
	rpc, err := r.authenticatedClient(ctx)
	if err != nil {
		return nil, err
	}
	var collection *string
	if opts.Collection != nil {
		value := opts.Collection.String()
		collection = &value
	}
	out, err := comatproto.SpaceListRecords(ctx, rpc, collection, opts.Cursor, opts.ExcludeValues, opts.Limit, r.author.String(), opts.Reverse, r.space.ref.String())
	if err != nil {
		return nil, err
	}
	page := &RecordPage{Records: make([]RecordEntry, len(out.Records)), Cursor: out.Cursor}
	for i, entry := range out.Records {
		collection, err := syntax.ParseNSID(entry.Collection)
		if err != nil {
			return nil, fmt.Errorf("invalid listed record collection: %w", err)
		}
		rkey, err := syntax.ParseRecordKey(entry.Rkey)
		if err != nil {
			return nil, fmt.Errorf("invalid listed record key: %w", err)
		}
		recordCID, err := parseRecordCID(entry.Cid)
		if err != nil {
			return nil, fmt.Errorf("invalid listed record CID: %w", err)
		}
		page.Records[i] = RecordEntry{
			Collection: collection, Rkey: rkey, CID: recordCID, Value: optionalRawValue(entry.Value),
		}
	}
	return page, nil
}

func (r *Repo) GetLatestCommit(ctx context.Context) (*Commit, error) {
	req := r.streamRequest("com.atproto.space.getLatestCommit", "application/json")
	var out comatproto.SpaceGetLatestCommit_Output
	request, headers, err := r.getMACJSON(ctx, req, repoMACType, &out)
	if err != nil {
		return nil, err
	}
	commit, err := commitFromWire(&out.Commit)
	if err != nil {
		return nil, err
	}
	authentication, err := request.verification(headers)
	if err != nil {
		return nil, err
	}
	if err := commit.Verify(r.space.ref, r.author, authentication); err != nil {
		return nil, err
	}
	return commit, nil
}

func (r *Repo) ListBlobs(ctx context.Context, opts ListBlobsOptions) (*BlobPage, error) {
	rpc, err := r.authenticatedClient(ctx)
	if err != nil {
		return nil, err
	}
	var since *string
	if opts.Since != nil {
		value := opts.Since.String()
		since = &value
	}
	out, err := comatproto.SpaceListBlobs(ctx, rpc, opts.Cursor, opts.Limit, r.author.String(), since, r.space.ref.String())
	if err != nil {
		return nil, err
	}
	page := &BlobPage{CIDs: make([]cid.Cid, len(out.Cids)), Cursor: out.Cursor}
	for i, encoded := range out.Cids {
		blobCID, err := parseBlobCID(encoded)
		if err != nil {
			return nil, fmt.Errorf("invalid listed blob CID: %w", err)
		}
		page.CIDs[i] = blobCID
	}
	return page, nil
}

// GetBlob returns an unverified blob stream. The caller must close it on success.
func (r *Repo) GetBlob(ctx context.Context, blobCID cid.Cid) (io.ReadCloser, error) {
	if err := validateBlobCID(blobCID); err != nil {
		return nil, err
	}
	rpc, err := r.authenticatedClient(ctx)
	if err != nil {
		return nil, err
	}
	req := r.streamRequest("com.atproto.space.getBlob", "*/*")
	req.QueryParams.Set("cid", blobCID.String())
	return getStream(ctx, rpc, req)
}

func (r *Repo) streamRequest(endpoint syntax.NSID, accept string) *atclient.APIRequest {
	req := atclient.NewAPIRequest(http.MethodGet, endpoint, nil)
	req.Headers.Set("Accept", accept)
	req.QueryParams.Set("space", r.space.ref.String())
	req.QueryParams.Set("repo", r.author.String())
	return req
}

func getStream(ctx context.Context, rpc *atclient.APIClient, req *atclient.APIRequest) (io.ReadCloser, error) {
	resp, err := doResponse(ctx, rpc, req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func doResponse(ctx context.Context, rpc *atclient.APIClient, req *atclient.APIRequest) (*http.Response, error) {
	resp, err := rpc.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	var body atclient.ErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, &atclient.APIError{StatusCode: resp.StatusCode}
	}
	return nil, body.APIError(resp.StatusCode)
}

func (r *Repo) getMACJSON(ctx context.Context, req *atclient.APIRequest, kind string, out any) (*macRequest, http.Header, error) {
	rpc, request, err := r.requestClient(ctx, kind)
	if err != nil {
		return nil, nil, err
	}
	req.Headers.Set(wantMACHeader, request.header)
	resp, err := doResponse(ctx, rpc, req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, nil, fmt.Errorf("decoding Spaces response: %w", err)
	}
	return request, resp.Header, nil
}

func optionalRecordCID(encoded *string) (*cid.Cid, error) {
	if encoded == nil {
		return nil, nil
	}
	parsed, err := parseRecordCID(*encoded)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func optionalRawValue(value *json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return *value
}

func commitFromWire(wire *comatproto.SpaceDefs_Commit) (*Commit, error) {
	if wire.LexiconTypeID != "" && wire.LexiconTypeID != "com.atproto.space.defs#commit" {
		return nil, fmt.Errorf("unexpected space commit type: %s", wire.LexiconTypeID)
	}
	if len(wire.Data) != 32 {
		return nil, fmt.Errorf("space commit data must be 32 bytes (got %d)", len(wire.Data))
	}
	commit := &Commit{
		Version: wire.Ver, Data: [32]byte(wire.Data), Repo: syntax.DID(wire.Repo),
		Space: syntax.SpaceRef(wire.Space), Revision: syntax.TID(wire.Rev),
	}
	if err := commit.validate(); err != nil {
		return nil, err
	}
	return commit, nil
}
