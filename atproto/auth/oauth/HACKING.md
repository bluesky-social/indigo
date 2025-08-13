
## Package Structure

`oauth.ClientApp`
- represents an overall application or service; helps establish and manage oauth.ClientSession
- wraps and manages client metadata, client attestation secret (for confidential clients), request and session storage

`oauth.ClientSession`
- represents an established user session, wrapping DPoP key, tokens, and other metadata
- implements client.AuthMethod, for use with ApiClient
- automates token refresh; for confidential clients requires ref to client secret
- triggers callback when session data are updated (nonce, tokens)

`oauth.ClientAuthStore`
- interface for persistent storage systems for auth request and session metadata, including secrets and DPoP private keys

`oauth.Resolver`
- currently always resolves direct from the network; may add flexible caching or interface abstraction in the future


## Implementation Details

- starts DPoP at PAR (specification is flexible about this)
- requires ES256 (P-256) for DPoP and client attestation private keys; though flexible interface types are used in the API
- scopes are configured as part of client metadata, and the same for each session

