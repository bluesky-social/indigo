// Package spaces implements the draft atproto permissioned Spaces protocol.
// It follows proposal revision 75f5b5dfd9a1e4b140700c75274c5d147b9eea12 and
// the com.atproto.space schemas at atproto revision
// 3a0b8c669881efa9e75680275f8695d618c5f80f. The proposal is not a final standard.
//
// Client, Credential, Space and Repo are immutable handles. Construction makes
// no network requests. NewCredential accepts an existing token and its bound
// P-256 signer without OAuth or another exchange. It does not verify the token's
// issuer signature. GetDelegationToken and Client.ExchangeCredential provide
// explicit issuance. ExchangeCredential sends the key and optional assertions
// in its JSON body. An empty delegation token supports public or app-only spaces.
// To replace a credential, construct new handles. Existing handles keep their
// original token and key. Signers, directories and HTTP clients are caller-owned.
//
// Space operations use the authority's #atproto_pds endpoint and authority DID
// as their audience. Repo operations use the author's PDS and author DID, even
// when authors share a host. Credential requests use the Atproto-Space-Credential
// authorization scheme and tagged HTTP Message Signatures, not DPoP.
// Notification registration takes a service DID, optionally with a fragment,
// and returns its expiry. VerifyNotificationAuth checks issuer, audience and lxm.
// OAuth-authenticated writes use the generated api/atproto RPCs and the caller's
// authenticated PDS client. Space management is outside this package.
//
// Commits and individual record CIDs use a message authentication code (MAC),
// carried in the Atproto-Space-Mac response header. Each request for a commit or
// record makes a fresh P-256 key exchange with the author's #atproto_kem key.
// The resulting shared secret authenticates that response to its reader. It is
// not a proof that the reader can pass to third parties. MAC failures remain
// visible so callers can refresh identity data and decide whether to retry.
// The library does not refresh identities or retry automatically.
//
// GetLatestCommit authenticates the returned commit. ListOps authenticates the
// head commit when present, but callers must compare its Data with their own
// running LtHash digest before accepting accumulated operations. GetRecord checks
// the full requested URI and authenticates its CID. It retains the raw JSON
// value without converting it. Call VerifyRecord with the value's canonical
// DAG-CBOR bytes before accepting its content. Lists and intermediate operation
// pages do not authenticate their record contents by themselves.
//
// GetRepo returns a STAR-lite v1 archive stream and its Authentication material.
// Pass that material, the expected Space and Author to VerifyRepo. Verification
// checks the metadata and commit MAC, streams records in strict byte order,
// checks canonical record encodings, and compares the final LtHash digest.
// Success requires clean EOF. Callback bytes are borrowed. Copy retained bytes
// and stage callback output until verification succeeds. Use ListRecords with
// ExcludeValues for metadata-only recovery. CAR and index-only archives are not
// supported by the current protocol.
//
// Callers close successful GetRepo and GetBlob streams. Error paths close their
// response bodies and retain named API errors. Verification helpers do not close
// caller-supplied streams. VerifyBlob output is provisional until it succeeds.
// Keep per-response shared secrets private. Do not persist or log them.
//
// Discovery is the authority's report, not proof of a repo or a frozen inventory.
// Repos can reappear while paging. Operation cursors are opaque, revisions name
// atomic batches, values can be absent, and intermediate pages can omit commits.
// Errors and notifications do not delete consumer data.
//
// Consumers own scheduling, persistence, checkpoints, reconciliation, concurrency,
// credential renewal and recovery. This package has no sync loop, automatic
// credential exchange, automatic recovery or webhook receiver. Spaces provides
// access control, not end-to-end encryption. Local HTTP and archive verification
// were exercised, not a live Spaces service.
//
// Proposal: https://github.com/bluesky-social/proposals/blob/75f5b5dfd9a1e4b140700c75274c5d147b9eea12/0016-permissioned-data/README.md
package spaces
