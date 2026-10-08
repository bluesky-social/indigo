# Generic Spaces library: Indigo implementation plan

Status: implemented in Indigo on `spaces-library`. Local HTTP routing/authentication/streaming scenarios and independently constructed signature, HMAC, and CAR verification scenarios have been exercised. No live Spaces service has been exercised.

This continues [the original design handoff](../spaces-go/DESIGN.md). Build the library in Indigo by extracting the existing Indigo/Cobalt draft, not by importing Cobalt's client lifecycle wholesale or starting another protocol implementation.

## Scope and boundary

The library provides protocol operations, identity/host resolution, authentication, decoding, and verification. It should support exporters, backup tools, CLIs, indexing applications, and a separately implemented syncer.

The current review is limited to protocol building blocks for a syncing tool. Simplespace management schemas and generated RPCs are excluded and parked separately; requesting and using space credentials does not depend on them.

Consumers own scheduling, persistence, checkpoints, reconciliation, concurrency policy, and credential-renewal policy.

Do not add a sync loop, `Sync()`/`Run()` method, store interface, worker pool, scheduler, automatic renewal, automatic recovery, webhook server, or automatic deletion action. OAuth-authenticated own-repo writes and space management remain distinct from credential-authenticated whole-space reads.

## Inspected sources and reusable work

The checked-out revisions match the original handoff:

- Indigo main: `b2619d864df00a66455bd9f5f0c83c1ac13ad73d`.
- Cobalt spaces branch: `c6e7954c1a6ecc27473541967b8f4b8a3a365364`.
- Cobalt's pinned Indigo draft: `6bbf05369ebcbbb45ef26e7f4c295dd9eb4e2b8b`.

Local sources:

- Main proposal: `../proposals/0016-permissioned-data/README.md`. Use this, not the companion syncer note that describes DPoP.
- Cobalt draft: `../cobalt/atspace`.
- Draft Indigo: `/home/ubuntu/go/pkg/mod/github.com/bluesky-social/indigo@v0.0.0-20260923021207-6bbf05369ebc`.

Reusable pieces:

| Source | Reuse |
|---|---|
| Cobalt `atspace/client.go` | Author-PDS resolution, raw CAR/blob downloads, record and commit retrieval |
| Cobalt `atspace/session.go` | Delegation exchange and audience-specific `atclient.AuthMethod` implementations |
| Cobalt `atspace/commit.go` | Commit construction, signing, and verification logic |
| Cobalt `atspace/lthash` | Incremental set hash and vectors |
| Cobalt `atspace/xsyntax` | Space reference/URI parsers and accessors |
| Cobalt `lexicons/com/atproto` | Draft Spaces RPC schemas |
| Draft Indigo | Authority key/host helpers, Spaces scopes, `SchemaSpace`, generator dispatch support |
| Indigo `atproto/atclient` | XRPC transport, generated-helper compatibility, pluggable authentication, raw responses, named JSON API errors |

Do not import Cobalt's PDS client, application code, or mutable session lifecycle into the generic library.

## Required corrections during extraction

1. Separate handle construction from credential issuance, and replace credentials explicitly instead of retaining the prototype's mutable `RefreshSession` lifecycle. Reading with an existing credential must require neither OAuth nor another exchange. See [Credential lifecycle rationale](#credential-lifecycle-rationale).
2. Preserve `*atclient.APIError` names and statuses on streaming failures. Cobalt's download error paths currently discard that information.
3. Correct the HTTP-signature base's literal `\\n` versus actual newline incompatibility recorded in the handoff.
4. Correct commit MAC construction: authenticate the repo hash with HMAC-SHA256, rather than appending an empty-message MAC to the hash.
5. Correct commit wire codecs. A fresh throwaway Go probe confirmed that Cobalt's `SpaceCommit` emits JSON arrays for fixed byte arrays and base64 strings for byte slices, and rejects independently constructed protocol `{"$bytes": ...}` objects.
6. Do not carry over LtHash's advertised `hash.Hash` surface. The same probe confirmed that `Sum(prefix)` places the prefix after the digest. Keep the set-oriented Add/Remove/digest/state operations instead.
7. Correct strict Spaces URI/schema validation, authority fallback behavior, missing Spaces permission-set support, and generated unknown open-union preservation.

The fresh probe exercised commit JSON encoding/decoding and LtHash prefix behavior only. The two cryptographic interoperability failures were taken from the already-exercised handoff and were not rerun. The probe was removed. No live Spaces service was exercised.

## Package placement

| Location | Responsibility |
|---|---|
| `atproto/spaces` | Client, space/repo handles, credentials, protocol verification |
| `atproto/atcrypto/lthash` | Cobalt's generic set-hash primitive, under Indigo's existing crypto package tree |
| `atproto/syntax` | `SpaceRef`, `SpaceURI`; no retained `xsyntax` package |
| `atproto/identity` | Authority key/host selection |
| `atproto/auth`, `atproto/lexicon` | Draft scope and schema support |
| `api/atproto` | Generated `space` RPCs |

Indigo already has `atproto/atcrypto` for cryptographic keys and operations (`atproto/atcrypto/docs.go`). Put LtHash beneath it as the algorithm-specific `lthash` subpackage, retaining Cobalt's narrow set-hash API rather than making a generic cryptographic primitive depend on the Spaces client. Space-specific commit context, index folding, and verification stay in `atproto/spaces`.

Dependency direction: syntax remains a leaf; identity/auth/lexicon reuse it; generated APIs do not depend on the high-level client; `spaces` consumes these foundations and `atcrypto/lthash`. Neither crypto nor schema validation imports the Spaces client. Retain `atspace` only in references to the original Cobalt sources, not as the Indigo package name.

Keep public-repository `ATURI` semantics unchanged. Lexicon `at-uri` validation should recognize the separate public and Spaces parsers. `space-ref` must use `ParseSpaceRef`, not generic `ParseURI`.

Port these identity signatures:

```go
func (i *Identity) SpacePublicKey() (atcrypto.PublicKey, error)
func (i *Identity) SpaceHostEndpoint() (string, error)
```

The second intentionally differs from the draft's string-only result. Missing and malformed declarations must remain distinguishable, including declaration information discarded by identity parsing. Fall back to account key/PDS entries only when dedicated entries are absent, never when present but malformed. Validate the selected dedicated service's type and absolute HTTP(S) endpoint without globally changing unrelated public-PDS behavior.

Migrate Cobalt's `SpaceRef` and `SpaceURI` APIs into `atproto/syntax`, retaining the constructor/parser/accessor/text-codec shape. Use `RecordKey` for skey. Space references require exactly the authority/type/skey prefix. The draft `SpaceURI` parser's repo/collection-prefix convenience must not be mistaken for a complete record address.

## Client and handle contract

Use opaque, immutable `Client`, `Credential`, `Space`, and `Repo` types. The declarations below describe the implemented API.

```go
type ClientOptions struct {
    Directory  identity.Directory
    HTTPClient *http.Client
    Headers    http.Header
}

func NewClient(opts ClientOptions) *Client

func NewCredential(token string, signer atcrypto.PrivateKey) (*Credential, error)
func (cred *Credential) Token() string

func (c *Client) Space(ref syntax.SpaceRef, credential *Credential) *Space
func (s *Space) Repo(author syntax.DID) *Repo

func (c *Client) ResolveSpaceHost(ctx context.Context, ref syntax.SpaceRef) (string, error)
func (c *Client) ResolveRepoHost(ctx context.Context, author syntax.DID) (string, error)
func (s *Space) ResolveHost(ctx context.Context) (string, error)
func (r *Repo) ResolveHost(ctx context.Context) (string, error)

func (s *Space) ListRepos(ctx context.Context, opts ListReposOptions) (*RepoPage, error)

func (r *Repo) GetRepo(ctx context.Context, opts GetRepoOptions) (io.ReadCloser, error)
func (r *Repo) ListOps(ctx context.Context, opts ListOpsOptions) (*OpsPage, error)
func (r *Repo) GetRecord(ctx context.Context, collection syntax.NSID, rkey syntax.RecordKey) (*Record, error)
func (r *Repo) ListRecords(ctx context.Context, opts ListRecordsOptions) (*RecordPage, error)
func (r *Repo) GetLatestCommit(ctx context.Context) (*Commit, error)
func (r *Repo) ListBlobs(ctx context.Context, opts ListBlobsOptions) (*BlobPage, error)
func (r *Repo) GetBlob(ctx context.Context, cid cid.Cid) (io.ReadCloser, error)
```

Construction performs no resolution or exchange. A nil credential permits resolution-only handles; authenticated operations require a credential. `NewCredential` requires a P-256 signer but does not certify the issuer's token signature. `atcrypto.PrivateKey` supports opaque/nonexportable signing implementations; no second signer interface is needed.

Use `github.com/ipfs/go-cid.Cid` for operational CIDs in these signatures. `syntax.CID` is a string wrapper: its current parser checks length, an allowed-character regex, and one CIDv0 prefix, but does not decode the multibase encoding, CID version/codec, or multihash (`atproto/syntax/cid.go:9-35`). Those parsed components and binary CID bytes are needed for CAR links, content hashing, and codec/hash-profile checks. Full CID parsing is still distinct from verifying the referenced content.

This reuses an existing dependency and convention, not a new CID implementation: `atproto/repo/commit.go` already uses `cid.Cid`, and `atproto/atdata/CIDLink` wraps that type for JSON/CBOR links (`atproto/atdata/cidlink.go`). Decode network CID strings once at the typed boundary, apply the required protocol-profile checks, and reuse the parsed value during verification.

Expanding `syntax.CID` is viable. Its parser could delegate to the existing CID library, and a conversion helper could expose a parsed CID. That would improve validation for string-facing callers, but a string wrapper would still need decoding for cryptographic operations; strengthening the existing parser would also change which inputs existing callers accept. The proposed default therefore stays with `cid.Cid` for Spaces operations and keeps `syntax.CID` for lexical/pass-through uses. No `syntax.CID` expansion is required for this extraction, and no second CID decoder or hash implementation should be introduced.

Space-host requests target the authority DID as their audience. Repo requests resolve the author's PDS and target the author DID as their audience. Distinct author repos remain distinct even when they share a PDS.

### Credential lifecycle rationale

Cobalt's `NewSpaceClient` requires an authenticated PDS client and immediately calls `RefreshSession` (`../cobalt/atspace/client.go:34-77`). That fetches a delegation token, generates a new P-256 keypair, exchanges the token with the authority, and replaces the client's auth-session pointers. The constructor therefore cannot simply use an already-issued token and its bound signer; even an exporter supplied with that pair must first authenticate to the user's PDS and contact the authority again.

Split those two use cases. `NewCredential(existingToken, existingSigner)` plus `client.Space(ref, credential)` must work without an OAuth session or an issuance request. Consumers that need a credential can still call `GetDelegationToken` and `ExchangeCredential` explicitly. This is not a ban on obtaining replacement credentials.

When a consumer decides to replace a credential, it supplies a fresh signer to a new exchange and constructs new space/repo handles using the returned credential. Each existing handle keeps its original token/key pair; replacement does not mutate credentials used by other handles or in-flight requests. The consumer chooses when to obtain and install the replacement and when to retire the old handles. Expiry and request failures remain visible; the library does not silently exchange, retry, or choose a renewal time. This keeps renewal policy outside the library while preserving the protocol's exchange operation.


### Options

Preserve optional parameters rather than folding omission into zero/false:

```go
type ListReposOptions struct {
    Cursor *string
    Limit  *int64
}

type GetRepoOptions struct {
    ExcludeValues *bool
}

type ListOpsOptions struct {
    Since         *syntax.TID
    Cursor        *string
    Limit         *int64
    ExcludeValues *bool
}

type ListRecordsOptions struct {
    Collection    *syntax.NSID
    Cursor        *string
    Limit         *int64
    Reverse       *bool
    ExcludeValues *bool
}

type ListBlobsOptions struct {
    Since  *syntax.TID
    Cursor *string
    Limit  *int64
}
```

`GetRepo` accepts options because the actual draft already supports a commit/index-only CAR via `excludeValues`. Full contents remain the default.

`ListOps` maps to `com.atproto.space.listRepoOps`. Its opaque cursor takes precedence over `since` when both are supplied. Do not reinterpret that cursor as a repo revision or space revision.

### Results

- `RepoPage`: `Repos []RepoInfo`, `Cursor *string`.
- `RepoInfo`: author DID, `RepoRev syntax.TID`, `SpaceRev syntax.TID`, `Hash [32]byte`.
- `OpsPage`: `Ops []Operation`, `Cursor *string`, `Head *Commit`.
- `Operation`: revision, collection, rkey, nullable CID/previous CID as `*cid.Cid`, optional value as `json.RawMessage`.
- `Record`: returned `URI syntax.SpaceURI`, `CID cid.Cid`, `Value json.RawMessage`.
- `RecordPage`: entries containing collection, rkey, CID and optional raw value; `Cursor *string`.
- `BlobPage`: `CIDs []cid.Cid`, `Cursor *string`.
- `Commit`: draft version/revision/signature fields, fixed-size hash/IKM/MAC, with explicit protocol JSON/CBOR codecs.

Use `atdata.Bytes` or the existing equivalent wire representation inside the codecs; do not let default array/base64 JSON encoding reach the wire.

Discovery metadata is the authority's report, not proof of an author's repo. `listRepos` is ordered by space revision and a repo can reappear while paging; it is not a frozen inventory or exactly-once stream.

Keep operation revisions so consumers can identify atomic batches. Missing inlined values can mean superseded content and are not automatically errors. Head commits are optional on intermediate oplog pages.

## Separate exchange and notification operations

```go
type CredentialExchangeOptions struct {
    ClientAttestation *string
}

func GetDelegationToken(
    ctx context.Context, pds lexutil.LexClient, ref syntax.SpaceRef,
) (string, error)

func (c *Client) ExchangeCredential(
    ctx context.Context, ref syntax.SpaceRef, delegationToken string,
    signer atcrypto.PrivateKey, opts CredentialExchangeOptions,
) (*Credential, error)

func (s *Space) RegisterNotify(ctx context.Context, service string) (syntax.Datetime, error)
func (s *Space) UnregisterNotify(ctx context.Context, service string) error

func VerifyNotificationAuth(
    ctx context.Context, validator *auth.ServiceAuthValidator,
    token string, endpoint syntax.NSID, expectedIssuer syntax.DID,
) error
```

The exchange caller supplies a fresh P-256 signer and owns its lifetime. Reading with an issued credential is independent of this exchange. Do not add a renewal method.

Credential authentication uses HTTP Message Signatures, not DPoP. Sign Authorization and Atproto-Space-Audience in the proposal's exact order. HTTP-signature verification must accept both low-S and high-S ECDSA signatures, using Indigo's lenient verification primitive where appropriate.

Notification registration takes a service identifier, including an optional DID fragment, not a callback URL. Registration expiry is returned to the caller without scheduling a renewal.

Notification payloads use the generated notification types. Authentication must check expected issuer as well as audience and `lxm`; an arbitrary valid service JWT is insufficient. Authentication helpers do not implement a notification receiver or trigger deletion/recovery.

OAuth-authenticated own-repo writes continue through the existing authenticated PDS client and generated RPCs, not the space-credential read handle. Simplespace management is outside this review.

## Verification boundary

Keep raw retrieval separate from verified consumption:

```go
func DecodeCommitJSON(data []byte) (*Commit, error)
func DecodeCommitCBOR(data []byte) (*Commit, error)
func (commit *Commit) Verify(ref syntax.SpaceRef, author syntax.DID, key atcrypto.PublicKey) error

func VerifyRecord(cbor []byte, expected cid.Cid) error
func VerifyBlob(ctx context.Context, src io.Reader, dst io.Writer, expected cid.Cid) error

func VerifyRepo(
    ctx context.Context, src io.Reader, opts RepoVerificationOptions,
    consume func(RecordBlock) error,
) (*RepoVerification, error)
```

`RepoVerificationOptions` binds the space, author, public key, block-size limit, and explicit index-only mode. `RecordBlock` identifies the collection/rkey/CID and carries the record's CBOR bytes. The callback's buffer-lifetime contract must be documented; consumers retain data by copying when required.

`RepoVerification` contains the verified commit/index and whether record blocks were verified. Success requires consuming and checking the expected CAR through EOF. Index-only mode must not silently accept a truncated full repo.

Verification checks:

1. CAR version and the two ordered roots.
2. Commit structure, version, signature and MAC bound to the expected space/author/revision.
3. Canonical index key ordering and valid record paths/CIDs.
4. LtHash digest reconstructed from index entries against the commit hash.
5. In full mode, every expected record block in index order against its CID, with no missing or unexpected blocks.

Reuse existing CAR dependencies. Do not use public-repo `LoadRepoFromCAR`: it materializes a blockstore and expects an MST, whereas Spaces uses two roots and a flat index. Do not import Cobalt's separate CAR infrastructure wholesale.

Download callers close successful CAR/blob streams. Library error paths close response bodies. Offline verification helpers do not take ownership of a caller-supplied stream. Returning raw data is never a verification claim. Blobs are separate from repo CARs.

## Generated RPC integration

Use the newer `lex/lexgen` engine that Cobalt already uses, driven through the existing Indigo `cmd/lexgen` command. Do not create a third generator or import Cobalt's CLI wholesale.

- Emit new com.atproto definitions/helpers into `api/atproto`.
- Keep output-package naming independent of `LegacyMode`; selecting legacy mode only to obtain the package name changes optional-parameter semantics.
- Honor package targeting/mappings and avoid same-output-package imports.
- Use raw unknown values (`json.RawMessage`) rather than requiring globally registered record lexicons or eagerly converting values to maps/floating-point numbers.
- Preserve unknown open-union variants and their JSON round trips; retain closed-union rejection and clear old variant state when decoding into a reused destination.
- Do not blanket-regenerate unrelated established APIs or create duplicated agnostic Spaces APIs.
- Do not use generated binary helpers for large downloads: they buffer the body into `[]byte`. The high-level streams use `APIClient.Do`.

## Implementation sequence

### 1. Foundations

Extract address types and draft identity/schema/scope support. Fix strict URI validation, absent-only fallback, and missing Spaces permission-set support. Reuse existing parsers, types, and catalog checks; do not introduce authorization-policy evaluation into scope parsing.

### 2. Generated RPCs

Integrate the newer generator into the existing driver. Generate Spaces RPCs into `api/atproto` with raw unknown values and nonlegacy optional-parameter semantics. Fix output package selection, self-import behavior, and open-union preservation before exposing those generated reads.

### 3. Client and authentication

Replace `SpaceClient`/`SpaceSession` with the immutable handles described above. Reuse `APIClient` transport and auth hooks. Implement paged reads, raw streams, explicit exchange, and notification registration/authentication. Preserve protocol errors and resource ownership.

### 4. Cryptographic verification and proof

Extract LtHash and commit logic, correct the known interoperability failures, and add Spaces CAR verification.

Exercise local HTTP scenarios covering authority-versus-author routing, shared-PDS authors, supplied credentials without exchange, unknown record data, paging, missing operation values, optional head commits, streamed bodies, named errors, and index-only retrieval.

Use independently constructed HTTP-signature bases and commit/HMAC vectors, not only sign/verify round trips through the same code. Keep consumer-visible regression coverage for the real interoperability failures and relevant malformed/truncated inputs. Update Indigo docs after the behavior is proven.

## Protocol gap and non-guarantees

The draft `listRepoOps.json` defines no dedicated "since revision unavailable/history reset" error despite describing that situation. Preserve server errors and optional head commits. Do not invent a portable `HistoryLost` flag or silently initiate recovery; the protocol's exact signaling needs clarification before promising such a classification.

There is no single-host full-space dump or globally atomic snapshot. Full-space traversal remains consumer orchestration: discover writers, resolve each author, and retrieve each distinct repo.

`SpaceDeleted` remains a named protocol error, distinct from network/authentication failures. Neither failures nor notifications automatically delete consumer data.
