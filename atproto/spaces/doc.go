// Package spaces implements the draft atproto permissioned Spaces protocol.
//
// Client, Credential, Space and Repo are immutable handles. Construction makes
// no network requests. NewCredential accepts an existing token and its bound
// P-256 signer without an OAuth session or credential exchange; it does not
// authenticate the issuer's token signature. GetDelegationToken and
// Client.ExchangeCredential provide explicit issuance. To replace credentials,
// construct new handles; existing handles retain their original token/key pair.
// Signers, directories and HTTP clients remain caller-owned implementations.
//
// Space operations resolve the authority's SpaceHostEndpoint and use the
// authority DID as their audience. Repo operations resolve the author's PDS and
// use the author DID as their audience, even when authors share a host. Requests
// use HTTP Message Signatures rather than DPoP. Notification registration names
// a service DID, optionally with a fragment, and returns its expiry without
// scheduling renewal. VerifyNotificationAuth checks issuer, audience and lxm.
// OAuth-authenticated own-repo writes use the generated api/atproto RPCs and the
// existing authenticated PDS client. Space management is outside this package.
//
// Retrieval and verification are separate. Callers close successful GetRepo and
// GetBlob streams. VerifyRepo authenticates the commit and canonical index, folds
// the LtHash digest, verifies expected record blocks in index order and requires
// EOF. Index-only mode must be selected explicitly and does not claim record
// verification. Callback bytes are borrowed; copy retained data, and stage all
// callback output until verification succeeds. Verification does not close
// caller-supplied streams. VerifyBlob output is provisional until it succeeds.
//
// Discovery pages are the authority's report, not proof of authors' repos or a
// frozen inventory; repos may reappear while paging. Oplog cursors are opaque,
// operation revisions identify batches, values may be absent, and intermediate
// pages may omit the head commit. Protocol errors, including SpaceDeleted,
// remain visible. Neither errors nor notifications delete consumer data.
//
// Consumers own scheduling, persistence, checkpoints, reconciliation,
// concurrency, credential-renewal policy and recovery. This package has no sync
// loop, automatic credential exchange, automatic recovery or webhook receiver.
// Spaces provides access control, not end-to-end encryption. The protocol is a
// draft; local HTTP and offline verification have been exercised, not a live
// Spaces service.
package spaces
