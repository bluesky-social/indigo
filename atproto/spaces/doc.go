// Package spaces implements the draft AT Protocol Spaces client.
// It follows proposal revision 2755e73a31511c54729921b50340944ba8b02c93
// and the alpha schemas at atproto revision
// 83d473dec69bbf19fe95fcbbb20a39842779db82. Both sources remain drafts.
//
// Client, Credential, Space, and Repo are immutable handles. Construction makes
// no network requests. NewCredential accepts an issued token and its P-256
// signer. It does not authenticate the token issuer. GetDelegationToken obtains
// a delegation through the caller's OAuth client. ExchangeCredential calls
// authorizeSpaceSync with a fresh signer and optional assertions. Public spaces
// can use an empty delegation. Existing handles retain their credential.
//
// CredentialExchangeOptions.Notify registers a base HTTPS endpoint and secret
// token during authorization. The caller serves confirmRegistration and
// notifySpaceUpdate. Use a separate token for each space and keep it stable
// across authorizations. Authorization renews registration until the credential
// expires. Credential.SpaceRevision returns the authority revision at issuance.
//
// Requests use the authority's PDS for space operations and the author's PDS
// for repo operations. Each credential request carries an HTTP Message Signature
// over its authorization and audience. The audience is the authority or author
// DID, respectively. Signers, directories, and HTTP clients belong to the caller.
//
// Response MACs are HTTP Message Signatures requested through Accept-Signature.
// A MAC is a message authentication code that uses a shared secret. Each request
// derives a fresh secret with HPKE export-only P-256 and the author's
// #atproto_kem key. The client selects signatures by tag, normalizes the covered
// Structured Field dictionary, and compares it with the returned commit or
// record. MACs provide no transferable proof of authorship. On failure, callers
// can refresh the author's identity and retry with a new request. The library
// does not refresh identities or retry automatically.
//
// ListWriters returns writes and removals from the authority's writer set.
// This is discovery metadata, not proof of repo content. Entries can reappear
// during pagination. After more than 72 hours without a space revision, list
// the whole set and drop missing or removed repos. Authorize again when the
// authority's PDS changes. Keep consumer access rules separate from discovery.
//
// GetLatestCommit authenticates a commit. ListOps authenticates a commit on every
// page. Compare the accumulated LtHash digest with that commit after the final
// page. GetRecord authenticates the requested URI, CID, and record content while
// retaining its raw JSON. Lists do not authenticate record content by themselves.
//
// GetRepo returns a STAR stream and response Authentication. Pass both to
// VerifyRepo with the expected Space and Author. VerifyRepo uses dasl/star to
// read metadata and pairs. It checks the commit, record order, canonical CBOR,
// and the final LtHash digest. Success requires the end marker and clean EOF.
// Callback bytes are borrowed. Copy retained bytes and stage callback output
// until verification succeeds. Use ListRecords with ExcludeValues to inspect
// paths and CIDs. The caller closes successful GetRepo and GetBlob streams.
// Error paths close their response bodies and retain named API errors.
// VerifyBlob output also remains provisional until verification succeeds.
//
// Consumers own scheduling, persistence, checkpoints, account status tracking,
// authorization renewal, and recovery. Notifications are prompts. Confirm space
// deletion through authorizeSpaceSync returning SpaceDeleted before deleting
// local data. Other authorization failures do not establish deletion. Keep
// shared secrets private. This package does not implement a webhook receiver,
// a sync loop, or end-to-end encryption. No live Spaces service was exercised.
package spaces
