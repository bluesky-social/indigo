package spaces

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

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
	host, err := ident.SpaceHostEndpoint()
	if err != nil {
		return "", fmt.Errorf("resolving space host for %s: %w", ref, err)
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
	if r.space.credential == nil || r.space.credential.signer == nil || r.space.credential.token == "" {
		return nil, ErrMissingCredential
	}
	host, err := r.ResolveHost(ctx)
	if err != nil {
		return nil, err
	}
	return r.space.client.apiClient(host, &credentialAuth{
		credential: r.space.credential, audience: r.author,
	}), nil
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
		if len(entry.Hash) != 32 {
			return nil, fmt.Errorf("repo discovery hash must be 32 bytes, got %d", len(entry.Hash))
		}
		page.Repos[i] = RepoInfo{
			Author: author, RepoRev: repoRev, SpaceRev: spaceRev, Hash: [32]byte(entry.Hash),
		}
	}
	return page, nil
}

// GetRepo returns an unverified CAR stream. The caller owns and must close the
// successful stream; failures close their response bodies and retain APIError.
func (r *Repo) GetRepo(ctx context.Context, opts GetRepoOptions) (io.ReadCloser, error) {
	rpc, err := r.authenticatedClient(ctx)
	if err != nil {
		return nil, err
	}
	req := r.streamRequest("com.atproto.space.getRepo", "application/vnd.ipld.car")
	if opts.ExcludeValues != nil {
		req.QueryParams.Set("excludeValues", strconv.FormatBool(*opts.ExcludeValues))
	}
	return getStream(ctx, rpc, req)
}

func (r *Repo) ListOps(ctx context.Context, opts ListOpsOptions) (*OpsPage, error) {
	rpc, err := r.authenticatedClient(ctx)
	if err != nil {
		return nil, err
	}
	var since *string
	// The opaque transport cursor wins even when the explicitly supplied
	// cursor is empty. It is never interpreted as a repo or space revision.
	if opts.Cursor == nil && opts.Since != nil {
		value := opts.Since.String()
		since = &value
	}
	out, err := comatproto.SpaceListRepoOps(ctx, rpc, opts.Cursor, opts.ExcludeValues, opts.Limit, r.author.String(), since, r.space.ref.String())
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
	}
	return page, nil
}

func (r *Repo) GetRecord(ctx context.Context, collection syntax.NSID, rkey syntax.RecordKey) (*Record, error) {
	rpc, err := r.authenticatedClient(ctx)
	if err != nil {
		return nil, err
	}
	out, err := comatproto.SpaceGetRecord(ctx, rpc, collection.String(), r.author.String(), rkey.String(), r.space.ref.String())
	if err != nil {
		return nil, err
	}
	uri, err := syntax.ParseSpaceURI(out.Uri)
	if err != nil {
		return nil, fmt.Errorf("invalid returned space record URI: %w", err)
	}
	recordCID, err := parseRecordCID(out.Cid)
	if err != nil {
		return nil, fmt.Errorf("invalid returned record CID: %w", err)
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
	rpc, err := r.authenticatedClient(ctx)
	if err != nil {
		return nil, err
	}
	out, err := comatproto.SpaceGetLatestCommit(ctx, rpc, r.author.String(), r.space.ref.String())
	if err != nil {
		return nil, err
	}
	return commitFromWire(&out.Commit)
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
	resp, err := rpc.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.Body, nil
	}
	defer resp.Body.Close()
	var body atclient.ErrorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, &atclient.APIError{StatusCode: resp.StatusCode}
	}
	return nil, body.APIError(resp.StatusCode)
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

func commitFromWire(wire *comatproto.SpaceDefs_SignedCommit) (*Commit, error) {
	if wire.LexiconTypeID != "" && wire.LexiconTypeID != "com.atproto.space.defs#signedCommit" {
		return nil, fmt.Errorf("unexpected space commit type: %s", wire.LexiconTypeID)
	}
	if len(wire.Hash) != 32 || len(wire.Ikm) != 32 || len(wire.Mac) != 32 {
		return nil, fmt.Errorf("space commit hash, IKM, and MAC must each be 32 bytes (got %d, %d, %d)", len(wire.Hash), len(wire.Ikm), len(wire.Mac))
	}
	commit := &Commit{
		Version: wire.Ver, Hash: [32]byte(wire.Hash), IKM: [32]byte(wire.Ikm),
		MAC: [32]byte(wire.Mac), Signature: wire.Sig, Revision: syntax.TID(wire.Rev),
	}
	if err := commit.validate(); err != nil {
		return nil, err
	}
	return commit, nil
}
