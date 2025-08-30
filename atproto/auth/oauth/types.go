package oauth

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

var ClientAssertionJWTBearer string = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

var (
	ErrInvalidAuthServerMetadata = errors.New("invalid auth server metadata")
	ErrInvalidClientMetadata     = errors.New("invalid client metadata doc")
)

type JWKS struct {
	Keys []crypto.JWK `json:"keys"`
}

// Expected response type from looking up OAuth Protected Resource information on a server (eg, a PDS instance)
type ProtectedResourceMetadata struct {
	// are there other fields worth including?

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

	// Confidential clients must set this to `private_key_jwt`; public must be `none`.
	// In some sense this field is "optional" (including in atproto OAuth specs), but it is effectively required, because the default value is invalid for atproto OAuth.
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`

	// `none` is never allowed here. The current recommended and most-supported algorithm is ES256, but this may evolve over time.
	TokenEndpointAuthSigningAlg *string `json:"token_endpoint_auth_signing_alg,omitempty"`

	// DPoP is mandatory for all clients, so this must be present and true
	DPoPBoundAccessTokens bool `json:"dpop_bound_access_tokens"`

	// confidential clients must supply at least one public key in JWK format for use with JWT client authentication. Either this field or the `jwks_uri` field must be provided for confidential clients, but not both.
	JWKS *JWKS `json:"jwks,omitempty"`

	// URL pointing to a JWKS JSON object. See `jwks` above for details.
	JWKSURI *string `json:"jwks_uri,omitempty"`

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

// returns 'true' if client metadata indicates that this is a confidential client
func (m *ClientMetadata) IsConfidential() bool {
	if (m.JWKSURI != nil || (m.JWKS != nil && len(m.JWKS.Keys) > 0)) && m.TokenEndpointAuthMethod == "private_key_jwt" {
		return true
	}

	return false
}

func (m *ClientMetadata) Validate(clientID string) error {

	if m.ClientID == "" || m.ClientID != clientID {
		return fmt.Errorf("%w: client_id", ErrInvalidClientMetadata)
	}

	if m.ApplicationType != nil && !slices.Contains([]string{"web", "native"}, *m.ApplicationType) {
		return fmt.Errorf("%w: application_type must be 'web', 'native', or undefined", ErrInvalidClientMetadata)
	}

	if !slices.Contains(m.GrantTypes, "authorization_code") {
		return fmt.Errorf("%w: grant_type must include 'authorization_code'", ErrInvalidClientMetadata)
	}

	scopes := strings.Split(m.Scope, " ")
	if !slices.Contains(scopes, "atproto") {
		return fmt.Errorf("%w: scope must include 'atproto'", ErrInvalidClientMetadata)
	}

	if !slices.Contains(m.ResponseTypes, "code") {
		return fmt.Errorf("%w: response_types must include 'code'", ErrInvalidClientMetadata)
	}

	if len(m.RedirectURIs) == 0 {
		return fmt.Errorf("%w: redirect_uris must have at least one element", ErrInvalidClientMetadata)
	}

	// 'web' redirect URLs have more restrictions
	if m.ApplicationType == nil || *m.ApplicationType == "web" {
		for _, ru := range m.RedirectURIs {
			u, err := url.Parse(ru)
			if err != nil {
				return fmt.Errorf("%w: invalid web redirect_uris: %w", ErrInvalidClientMetadata, err)
			}
			if u.Scheme != "https" && u.Hostname() != "127.0.0.1" {
				return fmt.Errorf("%w: web redirect_uris must have 'https' scheme", ErrInvalidClientMetadata)
			}
		}
	}

	if !(m.TokenEndpointAuthMethod == "none" || m.TokenEndpointAuthMethod == "private_key_jwt") {
		return fmt.Errorf("%w: unsupported token_endpoint_auth_method", ErrInvalidClientMetadata)
	}

	if m.TokenEndpointAuthSigningAlg != nil && *m.TokenEndpointAuthSigningAlg == "none" {
		// NOTE: what if this is a public client?
		return fmt.Errorf("%w: token_endpoint_auth_signing_alg must not be 'none'", ErrInvalidClientMetadata)
	}

	if !m.DPoPBoundAccessTokens {
		return fmt.Errorf("%w: dpop_bound_access_tokens must be true (DPoP is required)", ErrInvalidClientMetadata)
	}

	if m.JWKSURI != nil && *m.JWKSURI == "" {
		return fmt.Errorf("%w: jwks_uri must be valid URL (when provided)", ErrInvalidClientMetadata)
	}

	// NOTE: metadata URLs are not validated (they are not an error for overall metadata doc)

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

	// must include S256
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`

	// must include both none (public clients) and private_key_jwt (confidential clients)
	TokenEndpointAuthMethodsSupoorted []string `json:"token_endpoint_auth_methods_supported"`

	// must not include `none`. Must include ES256 for now.
	TokenEndpointAuthSigningAlgValuesSupported []string `json:"token_endpoint_auth_signing_alg_values_supported"`

	// must include atproto. If supporting the transitional grants, they should be included here as well.
	ScopesSupported []string `json:"scopes_supported"`

	// must be true
	AuthorizationReponseISSParameterSupported bool `json:"authorization_response_iss_parameter_supported"`

	// must be true
	RequirePushedAuthorizationRequests bool `json:"require_pushed_authorization_requests"`

	// corresponds to the PAR endpoint URL
	PushedAuthorizationRequestEndpoint string `json:"pushed_authorization_request_endpoint"`

	// currently must include ES256
	DPoPSigningAlgValuesSupported []string `json:"dpop_signing_alg_values_supported"`

	// default is true; does not need to be set explicitly, but must not be false
	RequireRequestURIRegistration *bool `json:"require_request_uri_registration,omitempty"`

	// must be true
	ClientIDMetadataDocumentSupported bool `json:"client_id_metadata_document_supported"`
}

func (m *AuthServerMetadata) Validate(serverURL string) error {

	if m.Issuer == "" {
		return fmt.Errorf("%w: empty issuer", ErrInvalidAuthServerMetadata)
	}
	u, err := url.Parse(m.Issuer)
	if err != nil {
		return fmt.Errorf("%w: invalid issuer URL: %w", ErrInvalidAuthServerMetadata, err)
	}
	if u.Scheme != "https" || u.Port() != "" || u.Path != "" || u.Fragment != "" || u.RawQuery != "" {
		return fmt.Errorf("%w: issuer URL", ErrInvalidAuthServerMetadata)
	}

	// check that Issuer matches domain this metadata document was fetched from
	srvu, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Errorf("%w: invalid request URL: %w", ErrInvalidAuthServerMetadata, err)
	}
	if u.Scheme != srvu.Scheme || u.Host != srvu.Host {
		return fmt.Errorf("%w: issuer must match request URL", ErrInvalidAuthServerMetadata)
	}

	// check that authorization endpoint is a valid HTTPS URL with no fragment or query params (we will be appending query params latter)
	aeurl, err := url.Parse(m.AuthorizationEndpoint)
	if err != nil {
		return fmt.Errorf("%w: invalid auth endpoint URL (%s): %w", ErrInvalidAuthServerMetadata, m.AuthorizationEndpoint, err)
	}
	if aeurl.Scheme != "https" || u.Fragment != "" || u.RawQuery != "" {
		return fmt.Errorf("%w: invalid auth endpoint URL: %s", ErrInvalidAuthServerMetadata, m.AuthorizationEndpoint)
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

	// Optional hint to auth server of what expected auth behavior should be. Eg, 'create', 'none', 'consent', 'login', 'select_account'
	Prompt *string `url:"prompt,omitempty"`

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

	// OAuth scope strings
	Scopes []string `json:"scopes"`

	// unique token in URI format, which will be used by the client in the auth flow redirect
	RequestURI string `json:"request_uri"`

	// Full token endpoint URL
	AuthServerTokenEndpoint string `json:"authserver_token_endpoint"`

	// The secret token/nonce which a code challenge was generated from
	PKCEVerifier string `json:"pkce_verifier"`

	// Server-provided DPoP nonce from auth request (PAR)
	DPoPAuthServerNonce string `json:"dpop_authserver_nonce"`

	// The secret cryptographic key generated by the client for this specific OAuth session
	DPoPPrivateKeyMultibase string `json:"dpop_privatekey_multibase"`
}

// The fields which are included in an initial token refresh request. These HTTP POST bodies are form-encoded, so use URL encoding syntax, not JSON.
type InitialTokenRequest struct {
	// Client ID, aka client metadata URL
	ClientID string `url:"client_id"`

	// Only used in initial token request. Auth server will validate that this matches the redirect URI used during the auth flow (resulting in the auth code)
	RedirectURI string `url:"redirect_uri"`

	// Always `authorization_code`
	GrantType string `url:"grant_type"`

	// Refresh token
	RefreshToken string `url:"refresh_token"`

	// Authorization Code provided by the Auth Server via callback at the end of the auth request flow
	Code string `url:"code"`

	// PKCE verifier string. Only included in initial token request
	CodeVerifier string `url:"code_verifier"`

	// For confidential clients, must be "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	ClientAssertionType *string `url:"client_assertion_type"`

	// For confidential clients, the signed client assertion JWT
	ClientAssertion *string `url:"client_assertion"`
}

// The fields which are included in a token refresh request. These HTTP POST bodies are form-encoded, so use URL encoding syntax, not JSON.
type RefreshTokenRequest struct {
	// Client ID, aka client metadata URL
	ClientID string `url:"client_id"`

	// Always `authorization_code`
	GrantType string `url:"grant_type"`

	// Refresh token.
	RefreshToken string `url:"refresh_token"`

	// For confidential clients, must be "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	ClientAssertionType *string `url:"client_assertion_type"`

	// For confidential clients, the signed client assertion JWT
	ClientAssertion *string `url:"client_assertion"`
}

// Expected response from Auth Server token endpoint, both for initial token request and for refresh requests.
type TokenResponse struct {
	Subject string `json:"sub"`

	// Usually expected to be the scopes that the client requested, but technically only a subset may have been approved, or additional scopes granted (?).
	Scope string `json:"scope"`

	// Opaque access token, for requests to the resource server.
	AccessToken string `json:"access_token"`

	// Refresh token, for doing additional token requests to the auth server.
	RefreshToken string `json:"refresh_token"`
}
