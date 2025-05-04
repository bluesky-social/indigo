package oauth

import (
	"encoding/json"
	"encoding/base64"
	"crypto/sha256"
	"errors"
	"context"
	"bytes"
	"fmt"
	"io"
	"net/url"
	"slices"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"

	"github.com/google/go-querystring/query"
	"github.com/golang-jwt/jwt/v5"
)

const (
	GrantAuthorizationCode = "authorization_code"
	GrantRefreshToken      = "refresh_token"
)

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
	JWKSUri *string `json:"jwks_uri"`

	// human-readable name of the client
	ClientName *string `json:"client_name"`

	// not to be confused with client_id, this is a homepage URL for the client. If provided, the client_uri must have the same hostname as client_id.
	ClientURI *string `json:"client_uri"`

	// URL to client logo. Only https: URIs are allowed.
	LogoURI *string `json:"logo_uri"`

	// URL to human-readable terms of service (ToS) for the client. Only https: URIs are allowed.
	TosURI *string `json:"tos_uri"`

	// URL to human-readable privacy policy for the client. Only https: URIs are allowed.
	PolicyURI *string `json:"policy_uri"`
}

func (m *ClientMetadata) Validate() error {
	// TODO: validate field syntax, possibly including consistency against a provided URL / clientID
	return nil
}

type AuthRequest struct {
	// identifies the client software
	ClientID string `json:"client_id"`

	// must be `code`
	ResponseType string `json:"response_type"`

	// the PKCE challenge value
	CodeChallenge string `json:"code_challenge"`

	// which code challenge method is used, for example `S256`
	CodeChallengeMethod string `json:"code_challenge_method"`

	// random token used to verify the authorization request against the response
	State string `json:"state"`

	// must match against URIs declared in client metadata and have a format consistent with the application_type declared in the client metadata
	RedirectURI string `json:"redirect_uri"`

	// Space-separated. Must be a subset of the scopes declared in client metadata. Must include `atproto`.
	Scope string `json:"scope"`

	// used by confidential clients to describe the client authentication mechanism
	ClientAssertionType *string `json:"client_assertion_type,omitempty"`

	// only used for confidential clients, for client authentication
	ClientAssertion *string `json:"client_assertion,omitempty"`

	// account identifier to be used for login
	LoginHint *string `json:"login_hint,omitempty"`
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

var ErrInvalidAuthServerMetadata = errors.New("invalid auth server metadata")

func (m *AuthServerMetadata) Validate() error {
	// TODO: check that issues matches domain this metadata document was fetched from

	if m.Issuer == "" {
		return fmt.Errorf("%w: empty issuer")
	}
	u, err := url.Parse(m.Issuer)
	if err != nil {
		return err
	}
	if u.Scheme != "https" || u.Port() != "" || u.Path != "" || u.Fragment != "" || u.RawQuery != "" {
		return fmt.Errorf("%w: issuer URL")
	}
	if !slices.Contains(m.ResponseTypesSupported, "code") {
		return fmt.Errorf("%w: response_types_supported must include 'code'")
	}
	if !slices.Contains(m.GrantTypesSupported, "authorization_code") {
		return fmt.Errorf("%w: grant_types_supported must include 'authorization_code'")
	}
	if !slices.Contains(m.GrantTypesSupported, "refresh_token") {
		return fmt.Errorf("%w: grant_types_supported must include 'refresh_token'")
	}
	if !slices.Contains(m.CodeChallengeMethodsSupported, "S256") {
		return fmt.Errorf("%w: code_challenge_method must include 'S256'")
	}
	if !slices.Contains(m.TokenEndpointAuthMethodsSupoorted, "none") {
		return fmt.Errorf("%w: token_endpoint_auth_methods_supported must include 'none'")
	}
	if !slices.Contains(m.TokenEndpointAuthMethodsSupoorted, "private_key_jwt") {
		return fmt.Errorf("%w: token_endpoint_auth_methods_supported must include 'private_key_jwt'")
	}
	if !slices.Contains(m.TokenEndpointAuthSigningAlgValuesSupported, "ES256") {
		return fmt.Errorf("%w: token_endpoint_auth_signing_alg_values_supported must include 'ES256'")
	}
	if !slices.Contains(m.ScopesSupported, "atproto") {
		return fmt.Errorf("%w: scopes_supported must include 'atproto'")
	}
	if !m.AuthorizationReponseISSParameterSupported {
		return fmt.Errorf("%w: authorization_response_iss_parameter_supported must be true")
	}
	if !m.RequirePushedAuthorizationRequests {
		return fmt.Errorf("%w: require_pushed_authorization_requests must be true")
	}
	if m.PushedAuthorizationRequestEndpoint == "" {
		return fmt.Errorf("%w: pushed_authorization_request_endpoint is required")
	}
	if !slices.Contains(m.DPoPSigningAlgValuesSupported, "ES256") {
		return fmt.Errorf("%w: dpop_signing_alg_values_supported must include 'ES256'")
	}
	if m.RequireRequestURIRegistration != nil && *m.RequireRequestURIRegistration != true {
		return fmt.Errorf("%w: require_request_uri_registration must be undefined or true")
	}
	if !m.ClientIDMetadataDocumentSupported {
		return fmt.Errorf("%w: client_id_metadata_document_supported must be true")
	}
	return nil
}

type OAuthClient struct {
	Client   *http.Client
	ClientID string
	TTL      time.Duration
	DpopSecretKey crypto.PrivateKey
	DpopPublicJWK crypto.JWK
	ClientSecretKey crypto.PrivateKey
	//HostURL string
	AuthServerURL string
}

func NewOAuthClient(clientID string) OAuthClient {
	// TODO: include SSRF protections on http.Client{} by default
	c := OAuthClient{
		Client: http.DefaultClient,
		ClientID: clientID,
		TTL:    30 * time.Second,
	}
	return c
}

type OAuthProtectedResource struct {
	AuthorizationServers []string `json:"authorization_servers"`
}

// Resolves a resources server URL (eg, PDS URL) to an auth server URL (eg, entryway URL). They might be the same server!
//
// Ensures that the returned URL is valid (eg, parses as a URL).
func (c *OAuthClient) ResolveAuthServer(ctx context.Context, hostURL string) (string, error) {
	hu, err := url.Parse(hostURL)
	if err != nil {
		return "", err
	}
	// TODO: check against other resource server rules?
	if hu.Scheme != "https" || hu.Hostname() == "" || hu.Port() != "" {
		return "", fmt.Errorf("not a valid public host URL: %s", hostURL)
	}

	u := fmt.Sprintf("https://%s/.well-known/oauth-protected-resource", hu.Hostname())

	// NOTE: this allows redirects
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching protected resource document: %w", err)
	}
	defer resp.Body.Close()

	// intentionally check for exactly HTTP 200 (not just 2xx)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP error fetching protected resource document: %d", resp.StatusCode)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var body OAuthProtectedResource
	if err := json.Unmarshal(respBytes, &body); err != nil {
		return "", fmt.Errorf("invalid protected resource document: %w", err)
	}
	if len(body.AuthorizationServers) < 1 {
		return "", fmt.Errorf("no auth server URL in protected resource document")
	}
	authURL := body.AuthorizationServers[0]
	au, err := url.Parse(body.AuthorizationServers[0])
	if err != nil {
		return "", fmt.Errorf("invalid auth server URL: %w", err)
	}
	if au.Scheme != "https" || au.Hostname() == "" || au.Port() != "" {
		return "", fmt.Errorf("not a valid public auth server URL: %s", authURL)
	}
	// XXX:
	c.AuthServerURL = authURL
	return authURL, nil
}

// Validates the auth server metadata before returning.
func (c *OAuthClient) FetchAuthServerMeta(hostURL string) (*AuthServerMetadata, error) {
	su, err := url.Parse(hostURL)
	if err != nil {
		return nil, err
	}
	// TODO: check against other resource server rules?
	if su.Scheme != "https" || su.Hostname() == "" || su.Port() != "" {
		return nil, fmt.Errorf("not a valid public host URL: %s", hostURL)
	}

	u := fmt.Sprintf("https://%s/.well-known/oauth-authorization-server", su.Hostname())

	// NOTE: this allows redirects
	resp, err := c.Client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("fetching auth server metadata: %w", err)
	}
	defer resp.Body.Close()

	// NOTE: maybe any HTTP 2xx should be allowed?
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error fetching auth server metadata: %d", resp.StatusCode)
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var body AuthServerMetadata
	if err := json.Unmarshal(respBytes, &body); err != nil {
		return nil, fmt.Errorf("invalid protected resource document: %w", err)
	}

	if err := body.Validate(); err != nil {
		return nil, err
	}
	return &body, nil
}

type clientAssertionClaims struct {
	jwt.RegisteredClaims

	HTTPMethod      string  `json:"htm"`
	TargetURI       string  `json:"hti"`
	AccessTokenHash *string `json:"ath,omitempty"`
	Nonce           *string `json:"nonce,omitempty"`
}

type dpopClaims struct {
	jwt.RegisteredClaims

	HTTPMethod      string  `json:"htm"`
	TargetURI       string  `json:"hti"`
	AccessTokenHash *string `json:"ath,omitempty"`
	Nonce           *string `json:"nonce,omitempty"`
}

// TODO: make this a method on OAuthClient?
func (c *OAuthClient) NewClientAssertionJWT(clientID, authURL string, clientSecretKey crypto.PrivateKey) (string, error) {
	claims := clientAssertionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   c.ClientID,
			Subject:  c.ClientID,
			Audience: []string{authURL},
			ID:       randomNonce(),
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
	}

	var keyMethod jwt.SigningMethod
	switch clientSecretKey.(type) {
	case *crypto.PrivateKeyP256:
		keyMethod = signingMethodES256
	case *crypto.PrivateKeyK256:
		keyMethod = signingMethodES256K
	default:
		return "", fmt.Errorf("unknown clientSecretKey type")
	}

	token := jwt.NewWithClaims(keyMethod, claims)
	// TODO: insert key ID (kid) in to `token.Header["kid"]?
	return token.SignedString(clientSecretKey)
}

func (c *OAuthClient) NewDPoPJWT(httpMethod, url, dpopNonce string) (string, error) {

	claims := dpopClaims{
		HTTPMethod: httpMethod,
		TargetURI: url,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:       randomNonce(),
			IssuedAt: jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(c.TTL)),
		},
	}
	if dpopNonce != "" {
		claims.Nonce = &dpopNonce
	}

	// XXX: refactor to helper method
	var keyMethod jwt.SigningMethod
	switch c.ClientSecretKey.(type) {
	case *crypto.PrivateKeyP256:
		keyMethod = signingMethodES256
	case *crypto.PrivateKeyK256:
		keyMethod = signingMethodES256K
	default:
		return "", fmt.Errorf("unknown clientSecretKey type")
	}

	token := jwt.NewWithClaims(keyMethod, claims)
	token.Header["typ"] = "dpop+jwt"
	token.Header["jwk"] = c.DpopPublicJWK
	return token.SignedString(c.ClientSecretKey)
}

type PushedAuthRequest struct {
	ClientID string `json:"client_id" url:"client_id"`
	State string `json:"state" url:"state"`
	RedirectURI string `json:"redirect_uri" url:"redirect_uri"`
	Scope string `json:"scope" url:"scope"`
	LoginHint *string `json:"login_hint,omitempty" url:"login_hint,omitempty"`
	ResponseType string `json:"response_type" url:"response_type"`
	ClientAssertionType string `json:"client_assertion_type" url:"client_assertion_type"`
	ClientAssertion string `json:"client_assertion" url:"client_assertion"`
	CodeChallenge string `json:"code_challenge" url:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method" url:"code_challenge_method"`
}

// Sends PAR request to auth server
func (c *OAuthClient) SendAuthRequest(ctx context.Context, authMeta *AuthServerMetadata, redirectURI, loginHint, scope string) error {
	parURL := authMeta.PushedAuthorizationRequestEndpoint
	state := randomNonce()
	pkceVerifier := randomNonce()

	// generate PKCE code challenge for use in PAR request
	codeChallenge := S256CodeChallenge(pkceVerifier)

	// self-signed JWT using private key in client metadata (confidential client)
	// TODO: make "confidential client" mode optional
	assertionJWT, err := c.NewClientAssertionJWT(c.ClientID, authMeta.Issuer, c.ClientSecretKey)
	if err != nil {
		return err
	}

	body := PushedAuthRequest {
		ClientID: c.ClientID,
		State: state,
		RedirectURI: redirectURI,
		Scope: scope,
		ResponseType: "code",
		ClientAssertionType: "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		ClientAssertion: assertionJWT,
		CodeChallenge: codeChallenge,
		CodeChallengeMethod: "S256",
	}
	if loginHint != "" {
		body.LoginHint = &loginHint
	}
	vals, err := query.Values(body)
	if err != nil {
		return err
	}
	bodyBytes := []byte(vals.Encode())

	dpopServerNonce := ""
	dpopJWT, err := c.NewDPoPJWT("POST", parURL, dpopServerNonce)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", parURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", dpopJWT)

	resp, err := c.Client.Do(req)
	if err != nil {
		return err
	}

	// XXX: check if we need to retry with DPoP header

	_ = resp
	return nil
}

func (c *OAuthClient) DoWithDPoP(ctx context.Context, method, url string, reader) (http.Response, err) {
}

func S256CodeChallenge(raw string) string {
	b := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(b[:])
}
