package oauth

import (
	"errors"
	"fmt"
	"net/url"
	"slices"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

var ErrInvalidAuthServerMetadata = errors.New("invalid auth server metadata")

type JWKS struct {
	Keys []crypto.JWK `json:"keys"`
}

// Expected response type from looking up OAuth Protected Resource information on a server (eg, a PDS instance)
type ProtectedResourceMetadata struct {
	// TODO: are there other fields worth including?

	AuthorizationServers []string `json:"authorization_servers"`
}

type ClientMetadata struct {
	// Must exactly match the full URL used to fetch the client metadata file itself
	ClientID string `json:"client_id"`

	// Must be one of `web` or `native`, with `web` as the default if not specified.
	ApplicationType *string `json:"application_type,omitempty"`

	// `authorization_code` must always be included. `refresh_token` is optional, but must be included if the client will make token refresh requests.
	GrantTypes []string `json:"grant_types"`

	// All scope values which might be requested by the client are declared here. The `atproto` scope is required, so must be included here.
	Scope string `json:"scope"`

	// `code` must be included
	ResponseTypes []string `json:"response_types"`

	// At least one redirect URI is required.
	RedirectURIs []string `json:"redirect_uris"`

	// confidential clients must set this to `private_key_jwt`
	TokenEndpointAuthMethod *string `json:"token_endpoint_auth_method,omitempty"`

	// `none` is never allowed here. The current recommended and most-supported algorithm is ES256, but this may evolve over time.
	TokenEndpointAuthSigningAlg *string `json:"token_endpoint_auth_signing_alg,omitempty"`

	// DPoP is mandatory for all clients, so this must be present and true
	DpopBoundAccessTokens bool `json:"dpop_bound_access_tokens"`

	// confidential clients must supply at least one public key in JWK format for use with JWT client authentication. Either this field or the `jwks_uri` field must be provided for confidential clients, but not both.
	JWKS []crypto.JWK `json:"jwks,omitempty"`

	// URL pointing to a JWKS JSON object. See `jwks` above for details.
	JWKSUri *string `json:"jwks_uri,omitempty"`

	// human-readable name of the client
	ClientName *string `json:"client_name,omitempty"`

	// not to be confused with client_id, this is a homepage URL for the client. If provided, the client_uri must have the same hostname as client_id.
	ClientURI *string `json:"client_uri,omitempty"`

	// URL to client logo. Only https: URIs are allowed.
	LogoURI *string `json:"logo_uri,omitempty"`

	// URL to human-readable terms of service (ToS) for the client. Only https: URIs are allowed.
	TosURI *string `json:"tos_uri,omitempty"`

	// URL to human-readable privacy policy for the client. Only https: URIs are allowed.
	PolicyURI *string `json:"policy_uri,omitempty"`
}

func (m *ClientMetadata) Validate(clientID string) error {
	// XXX: validate field syntax, possibly including consistency against a provided URL / clientID
	// XXX: copy from python tutorial
	return nil
}

type AuthServerMetadata struct {

	// the "origin" URL of the Authorization Server. Must be a valid URL, with https scheme. A port number is allowed (if that matches the origin), but the default port (443 for HTTPS) must not be specified. There must be no path segments. Must match the origin of the URL used to fetch the metadata document itself.
	Issuer string `json:"issuer"`

	// endpoint URL for authorization redirects
	AuthorizationEndpoint string `json:"authorization_endpoint"`

	// endpoint URL for token requests
	TokenEndpoint string `json:"token_endpoint"`

	// must include code
	ResponseTypesSupported []string `json:"response_types_supported"`

	// must include authorization_code and refresh_token (refresh tokens must be supported)
	GrantTypesSupported []string `json:"grant_types_supported"`

	/// must include S256
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`

	// must include both none (public client s) and private_key_jwt (confidential clients)
	TokenEndpointAuthMethodsSupoorted []string `json:"token_endpoint_auth_methods_supported"`

	// must not include `none`. Must include ES256 for now.
	TokenEndpointAuthSigningAlgValuesSupported []string `json:"token_endpoint_auth_signing_alg_values_supported"`

	// must include atproto. If supporting the transitional grants, they should be included here as well.
	ScopesSupported []string `json:"scopes_supported"`

	// must be true
	AuthorizationReponseISSParameterSupported bool `json:"authorization_response_iss_parameter_supported"`

	// must be true
	RequirePushedAuthorizationRequests bool `json:"require_pushed_authorization_requests"`

	// correspnds be the PAR endpoint URL
	PushedAuthorizationRequestEndpoint string `json:"pushed_authorization_request_endpoint"`

	// currently must include ES256
	DPoPSigningAlgValuesSupported []string `json:"dpop_signing_alg_values_supported"`

	// default is true; does not need to be set explicitly, but must not be false
	RequireRequestURIRegistration *bool `json:"require_request_uri_registration,omitempty"`

	// must be true
	ClientIDMetadataDocumentSupported bool `json:"client_id_metadata_document_supported"`
}

func (m *AuthServerMetadata) Validate(serverURL string) error {
	// XXX: check that issues matches domain this metadata document was fetched from

	if m.Issuer == "" {
		return fmt.Errorf("%w: empty issuer", ErrInvalidAuthServerMetadata)
	}
	u, err := url.Parse(m.Issuer)
	if err != nil {
		return err
	}
	if u.Scheme != "https" || u.Port() != "" || u.Path != "" || u.Fragment != "" || u.RawQuery != "" {
		return fmt.Errorf("%w: issuer URL", ErrInvalidAuthServerMetadata)
	}
	if !slices.Contains(m.ResponseTypesSupported, "code") {
		return fmt.Errorf("%w: response_types_supported must include 'code'", ErrInvalidAuthServerMetadata)
	}
	if !slices.Contains(m.GrantTypesSupported, "authorization_code") {
		return fmt.Errorf("%w: grant_types_supported must include 'authorization_code'", ErrInvalidAuthServerMetadata)
	}
	if !slices.Contains(m.GrantTypesSupported, "refresh_token") {
		return fmt.Errorf("%w: grant_types_supported must include 'refresh_token'", ErrInvalidAuthServerMetadata)
	}
	if !slices.Contains(m.CodeChallengeMethodsSupported, "S256") {
		return fmt.Errorf("%w: code_challenge_method must include 'S256'", ErrInvalidAuthServerMetadata)
	}
	if !slices.Contains(m.TokenEndpointAuthMethodsSupoorted, "none") {
		return fmt.Errorf("%w: token_endpoint_auth_methods_supported must include 'none'", ErrInvalidAuthServerMetadata)
	}
	if !slices.Contains(m.TokenEndpointAuthMethodsSupoorted, "private_key_jwt") {
		return fmt.Errorf("%w: token_endpoint_auth_methods_supported must include 'private_key_jwt'", ErrInvalidAuthServerMetadata)
	}
	if !slices.Contains(m.TokenEndpointAuthSigningAlgValuesSupported, "ES256") {
		return fmt.Errorf("%w: token_endpoint_auth_signing_alg_values_supported must include 'ES256'", ErrInvalidAuthServerMetadata)
	}
	if !slices.Contains(m.ScopesSupported, "atproto") {
		return fmt.Errorf("%w: scopes_supported must include 'atproto'", ErrInvalidAuthServerMetadata)
	}
	if !m.AuthorizationReponseISSParameterSupported {
		return fmt.Errorf("%w: authorization_response_iss_parameter_supported must be true", ErrInvalidAuthServerMetadata)
	}
	if !m.RequirePushedAuthorizationRequests {
		return fmt.Errorf("%w: require_pushed_authorization_requests must be true", ErrInvalidAuthServerMetadata)
	}
	if m.PushedAuthorizationRequestEndpoint == "" {
		return fmt.Errorf("%w: pushed_authorization_request_endpoint is required", ErrInvalidAuthServerMetadata)
	}
	if !slices.Contains(m.DPoPSigningAlgValuesSupported, "ES256") {
		return fmt.Errorf("%w: dpop_signing_alg_values_supported must include 'ES256'", ErrInvalidAuthServerMetadata)
	}
	if m.RequireRequestURIRegistration != nil && *m.RequireRequestURIRegistration != true {
		return fmt.Errorf("%w: require_request_uri_registration must be undefined or true", ErrInvalidAuthServerMetadata)
	}
	if !m.ClientIDMetadataDocumentSupported {
		return fmt.Errorf("%w: client_id_metadata_document_supported must be true", ErrInvalidAuthServerMetadata)
	}
	return nil
}

// The fields which are included in a PAR request. These HTTP POST bodies are form-encoded, so use URL encoding syntax, not JSON.
type PushedAuthRequest struct {
	// Client ID, aka client metadata URL
	ClientID string `url:"client_id"`

	// Random identifier for this request, generated by client
	State string `url:"state"`

	// Client-specified URL that will get redirected to by auth server at end of user auth flow
	RedirectURI string `url:"redirect_uri"`

	// Requested auth scopes, as a space-delimited list
	Scope string `url:"scope"`

	// Optional account identifier (DID or handle) to help with user account login and/or account switching
	LoginHint *string `url:"login_hint,omitempty"`

	// Always "code"
	ResponseType string `url:"response_type"`

	// Always "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	ClientAssertionType string `url:"client_assertion_type"`

	// Confidential client signed JWT
	ClientAssertion string `url:"client_assertion"`

	// Client-generated PKCE challenge hash, derived from random "verifier" string
	CodeChallenge string `url:"code_challenge"`

	// Almost always "S256"
	CodeChallengeMethod string `url:"code_challenge_method"`
}

type PushedAuthResponse struct {
	// unique token in URI format, which will be used by the client in the auth flow redirect
	RequestURI string `json:"request_uri"`

	// positive integer indicating number of seconds the `request_uri` is valid for.
	ExpiresIn int `json:"expires_in"`
}

// Persisted information about an OAuth Auth Request.
type AuthRequestData struct {
	// The random identifier generated by the client for the auth request flow. Can be used as "primary key" for storing and retrieving this information.
	State string `json:"state"`

	// URL of the auth server (eg, PDS or entryway)
	AuthServerURL string `json:"authserver_url"`

	// If the flow started with an account identifier (DID or handle), it should be persisted, to verify against the initial token response.
	AccountDID *syntax.DID `json:"account_did,omitempty"`

	// XXX: if we started from handle, should probably trust / use the resolved DID?
	//AccountHandle *syntax.Handle `json:"account_handle,omitempty"`

	// Base URL of the "resource server" (eg, PDS), if the auth flow started with a host URL instead of an account identifier.
	HostURL *string `json:"host_url,omitempty"`

	// OAuth scope string (space-separated list)
	Scope string `json:"scope"`

	// unique token in URI format, which will be used by the client in the auth flow redirect
	RequestURI string `json:"request_uri"`

	// The secret token/nonce which a code challenge was generated from
	PKCEVerifier string `json:"pkce_verifier"`

	// Server-provided DPoP nonce from auth request (PAR)
	DpopAuthServerNonce string `json:"dpop_authserver_nonce"`

	// The secret cryptographic key generated by the client for this specific OAuth session
	// TODO: better name for this field
	DpopKeyMultibase string `json:"dpop_privatekey_multibase"`
}

// Persisted information about an OAuth session. Used to resume an active session.
type SessionData struct {
	// Account DID for this session. Assuming only one active session per account, this can be used as "primary key" for storing and retrieving this infromation.
	AccountDID syntax.DID `json:"account_did"`

	// Base URL of the "resource server" (eg, PDS). Should include scheme, hostname, port; no path or auth info.
	HostURL string `json:"host_url"`

	// Base URL of the "auth server" (eg, PDS or entryway). Should include scheme, hostname, port; no path or auth info.
	AuthServerURL string `json:"authserver_url"`

	//XXX: persist this through... from initial request?
	//AuthServerTokenEndpoint string `json:"authserver_token_endpoint"`

	// Token which can be used directly against host ("resource server", eg PDS)
	AccessToken string `json:"access_token"`

	// Token which can be sent to auth server (eg, PDS or entryway) to get a new access token
	RefreshToken string `json:"refresh_token"`

	// Current auth server DPoP nonce
	DpopAuthServerNonce string `json:"dpop_authserver_nonce"`

	// Current host ("resource server", eg PDS) DPoP nonce
	DpopHostNonce string `json:"dpop_host_nonce"`

	// The secret cryptographic key generated by the client for this specific OAuth session
	DpopKeyMultibase string `json:"dpop_secret_key"`

	// TODO: also persist access token creation time / expiration time? In context that token might not be an easily parsed JWT
}

// The fields which are included in an initial token refresh request. These HTTP POST bodies are form-encoded, so use URL encoding syntax, not JSON.
type InitialTokenRequest struct {
	// Client ID, aka client metadata URL
	ClientID string `url:"client_id"`

	// Only used in initial token request. Client-specified URL that will get redirected to by auth server at end of user auth flow
	// XXX: is this really needed in the initial token request?
	RedirectURI string `url:"redirect_uri"`

	// Always `authorization_code`
	GrantType string `url:"grant_type"`

	// Refresh token.
	RefreshToken string `url:"refresh_token"`

	Code string `url:"code"`

	// PKCE verifier string. Only included in initial token request.
	CodeVerifier string `url:"code_verifier"`

	// Always "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	ClientAssertionType string `url:"client_assertion_type"`

	// Confidential client signed JWT
	ClientAssertion string `url:"client_assertion"`
}

// The fields which are included in a token refresh request. These HTTP POST bodies are form-encoded, so use URL encoding syntax, not JSON.
type RefreshTokenRequest struct {
	// Client ID, aka client metadata URL
	ClientID string `url:"client_id"`

	// Always `authorization_code`
	GrantType string `url:"grant_type"`

	// Refresh token.
	RefreshToken string `url:"refresh_token"`

	// Always "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	ClientAssertionType string `url:"client_assertion_type"`

	// Confidential client signed JWT
	ClientAssertion string `url:"client_assertion"`
}

// Expected respose from Auth Server token endpoint, both for initial token request and for refresh requests.
type TokenResponse struct {
	Subject string `json:"sub"`

	// Usually expected to be the scopes that the client requested, but technically only a subset may have been approved, or additional scopes granted (?).
	Scope string `json:"scope"`

	// Opaque access token, for requests to the resource server.
	AccessToken string `json:"access_token"`

	// Refresh token, for doing additional token requests to the auth server.
	RefreshToken string `json:"refresh_token"`
}
