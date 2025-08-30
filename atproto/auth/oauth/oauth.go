package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-querystring/query"
)

var jwtExpirationDuration = 30 * time.Second

// Service-level client. Used to establish and refrsh OAuth sessions, but is not itself account or session specific, and can not be used directly to make API calls on behalf of a user.
type ClientApp struct {
	Client   *http.Client
	Resolver *Resolver
	Dir      identity.Directory
	Config   *ClientConfig
	Store    ClientAuthStore
}

// App-level client configuration data.
//
// Not to be confused with the [ClientMetadata] struct type, which represents a full client metadata JSON document.
type ClientConfig struct {
	// Full client identifier, which should be an HTTP URL
	ClientID string
	// Fully qualified callback URL
	CallbackURL string
	// Set of OAuth scope strings, which will be both declared in client metadata document and requested for every session. Must include "atproto".
	Scopes    []string
	UserAgent string

	// For confidential clients, the private client assertion key. Note that while an interface is used here, only P-256 is allowed by the current specification.
	PrivateKey crypto.PrivateKey

	// ID for current client assertion key (should be provided if PrivateKey is)
	KeyID *string
}

// Constructs a [ClientApp] based on configuration.
func NewClientApp(config *ClientConfig, store ClientAuthStore) *ClientApp {
	app := &ClientApp{
		Client:   http.DefaultClient,
		Resolver: NewResolver(),
		Dir:      identity.DefaultDirectory(),
		Config:   config,
		Store:    store,
	}
	if config.UserAgent != "" {
		app.Resolver.UserAgent = config.UserAgent

		// unpack DefaultDirectory nested type and insert UserAgent (and log failure in case default types change)
		dirAgent := false
		cdir, ok := app.Dir.(*identity.CacheDirectory)
		if ok {
			bdir, ok := cdir.Inner.(*identity.BaseDirectory)
			if ok {
				dirAgent = true
				bdir.UserAgent = config.UserAgent
			}
		}
		if !dirAgent {
			slog.Info("OAuth ClientApp identity directory User-Agent not configured")
		}
	}
	return app
}

// Creates a basic [ClientConfig] for use as a public (non-confidential) client. To upgrade to a confidential client, use this method and then [ClientConfig.SetClientSecret].
//
// The "scopes" array must include "atproto".
func NewPublicConfig(clientID, callbackURL string, scopes []string) ClientConfig {
	c := ClientConfig{
		ClientID:    clientID,
		CallbackURL: callbackURL,
		UserAgent:   "indigo-sdk",
		Scopes:      scopes,
	}
	return c
}

// Creates a basic [ClientConfig] for use with localhost developmnet. Such a client is always public (non-confidential).
//
// The "scopes" array must include "atproto".
func NewLocalhostConfig(callbackURL string, scopes []string) ClientConfig {
	params := make(url.Values)
	params.Set("redirect_uri", callbackURL)
	params.Set("scope", scopeStr(scopes))
	c := ClientConfig{
		ClientID:    fmt.Sprintf("http://localhost?%s", params.Encode()),
		CallbackURL: callbackURL,
		UserAgent:   "indigo-sdk",
		Scopes:      scopes,
	}
	return c
}

// Whether this is a "confidential" OAuth client (with configured client attestation key), versus "public" client.
func (config *ClientConfig) IsConfidential() bool {
	return config.PrivateKey != nil && config.KeyID != nil
}

func (config *ClientConfig) SetClientSecret(priv crypto.PrivateKey, keyID string) error {
	switch priv.(type) {
	case *crypto.PrivateKeyP256:
		// pass
	case *crypto.PrivateKeyK256:
		return fmt.Errorf("only P-256 (ES256) private keys supported for atproto OAuth")
	default:
		return fmt.Errorf("unknown private key type: %T", priv)
	}
	config.PrivateKey = priv
	config.KeyID = &keyID
	return nil
}

// Returns a "JWKS" representation of public keys for the client. This can be returned as JSON, as part of client metadata.
//
// If the client does not have any keys (eg, public client), returns an empty set.
func (config *ClientConfig) PublicJWKS() JWKS {

	jwks := JWKS{Keys: []crypto.JWK{}}

	// public client with no keys
	if config.PrivateKey == nil || config.KeyID == nil {
		return jwks
	}

	pub, err := config.PrivateKey.PublicKey()
	if err != nil {
		return jwks
	}
	jwk, err := pub.JWK()
	if err != nil {
		return jwks
	}
	jwk.KeyID = config.KeyID

	jwks.Keys = []crypto.JWK{*jwk}
	return jwks
}

// helper to turn a list of scope strings in to a single space-separated scope string
func scopeStr(scopes []string) string {
	return strings.Join(scopes, " ")
}

// Returns a [ClientMetadata] struct with the required fields populated based on this client configuration. Clients may want to populate additional metadata fields on top of this response.
//
// NOTE: confidential clients currently must provide JWKSURI after the fact
func (config *ClientConfig) ClientMetadata() ClientMetadata {
	m := ClientMetadata{
		ClientID:                config.ClientID,
		ApplicationType:         strPtr("web"),
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		Scope:                   scopeStr(config.Scopes),
		ResponseTypes:           []string{"code"},
		RedirectURIs:            []string{config.CallbackURL},
		DPoPBoundAccessTokens:   true,
		TokenEndpointAuthMethod: "none",
	}
	if config.IsConfidential() {
		m.TokenEndpointAuthMethod = "private_key_jwt"
		// NOTE: the key type is always ES256
		m.TokenEndpointAuthSigningAlg = strPtr("ES256")

		// TODO: need to include 'use' or 'key_ops' for JWKS in the client metadata doc?
		//jwks := config.PublicJWKS()
		//m.JWKS = &jwks
	}
	return m
}

// High-level helper for fetching session data from store, based on account DID and session identifier.
func (app *ClientApp) ResumeSession(ctx context.Context, did syntax.DID, sessionID string) (*ClientSession, error) {

	sd, err := app.Store.GetSession(ctx, did, sessionID)
	if err != nil {
		return nil, err
	}

	sess := ClientSession{
		Client: app.Client,
		Config: app.Config,
		Data:   sd,
	}

	// configure callback for updating session data
	if app.Store != nil {
		sess.PersistSessionCallback = func(ctx context.Context, data *ClientSessionData) {
			slog.Debug("storing updated session data", "did", data.AccountDID, "session_id", data.SessionID)
			err := app.Store.SaveSession(ctx, *data)
			if err != nil {
				slog.Error("failed to store updated session data", "did", data.AccountDID, "session_id", data.SessionID, "err", err)
			}
		}
	}

	// TODO: refactor this in to ClientAuthStore layer?
	priv, err := crypto.ParsePrivateMultibase(sd.DPoPPrivateKeyMultibase)
	if err != nil {
		return nil, err
	}
	sess.DPoPPrivateKey = priv
	return &sess, nil
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
	TargetURI       string  `json:"htu"`
	AccessTokenHash *string `json:"ath,omitempty"`
	Nonce           *string `json:"nonce,omitempty"`
}

// Low-level helper to generate and sign an OAuth confidential client assertion token (JWT).
func (cfg *ClientConfig) NewClientAssertion(authURL string) (string, error) {
	if !cfg.IsConfidential() {
		return "", fmt.Errorf("non-confidential client")
	}
	claims := clientAssertionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   cfg.ClientID,
			Subject:  cfg.ClientID,
			Audience: []string{authURL},
			ID:       secureRandomBase64(16),
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
	}

	signingMethod, err := keySigningMethod(cfg.PrivateKey)
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(signingMethod, claims)
	token.Header["kid"] = cfg.KeyID
	return token.SignedString(cfg.PrivateKey)
}

// Creates a DPoP token (JWT) for use with an OAuth Auth Server (not to be used with Resource Server). The returned JWT is not bound to an Access Token (no 'ath'), and does not indicate an issuer ('iss').
//
// This is used during initial auth request (PAR), initial token request, and subsequent refresh token requests. Note that a full [ClientSession] is not available in several of these circumstances, so this is a stand-alone function.
func NewAuthDPoP(httpMethod, url, dpopNonce string, privKey crypto.PrivateKey) (string, error) {

	claims := dpopClaims{
		HTTPMethod: httpMethod,
		TargetURI:  url,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        secureRandomBase64(16),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(jwtExpirationDuration)),
		},
	}
	if dpopNonce != "" {
		claims.Nonce = &dpopNonce
	}

	keyMethod, err := keySigningMethod(privKey)
	if err != nil {
		return "", err
	}

	// TODO: parse/cache this public JWK, for efficiency
	pub, err := privKey.PublicKey()
	if err != nil {
		return "", err
	}
	pubJWK, err := pub.JWK()
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(keyMethod, claims)
	token.Header["typ"] = "dpop+jwt"
	token.Header["jwk"] = pubJWK
	return token.SignedString(privKey)
}

// attempts to read an HTTP response body as JSON, and determine an error reason. always closes the response body
func parseAuthErrorReason(resp *http.Response, reqType string) string {
	defer resp.Body.Close()
	var errResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		slog.Warn("auth server request failed", "request", reqType, "statusCode", resp.StatusCode, "err", err)
		return "unknown"
	}
	slog.Warn("auth server request failed", "request", reqType, "statusCode", resp.StatusCode, "body", errResp)
	return fmt.Sprintf("%s", errResp["error"])
}

// Low-level helper to send PAR request to auth server, which involves starting PKCE and DPoP.
func (app *ClientApp) SendAuthRequest(ctx context.Context, authMeta *AuthServerMetadata, scopes []string, loginHint string) (*AuthRequestData, error) {

	parURL := authMeta.PushedAuthorizationRequestEndpoint
	state := secureRandomBase64(16)
	pkceVerifier := secureRandomBase64(48)

	// generate PKCE code challenge for use in PAR request
	codeChallenge := S256CodeChallenge(pkceVerifier)

	slog.Debug("preparing PAR", "client_id", app.Config.ClientID, "callback_url", app.Config.CallbackURL)
	body := PushedAuthRequest{
		ClientID:            app.Config.ClientID,
		State:               state,
		RedirectURI:         app.Config.CallbackURL,
		Scope:               scopeStr(scopes),
		ResponseType:        "code",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	}

	if app.Config.IsConfidential() {
		// self-signed JWT using private key in client metadata (confidential client)
		assertionJWT, err := app.Config.NewClientAssertion(authMeta.Issuer)
		if err != nil {
			return nil, err
		}
		body.ClientAssertionType = ClientAssertionJWTBearer
		body.ClientAssertion = assertionJWT
	}

	if loginHint != "" {
		body.LoginHint = &loginHint
	}
	vals, err := query.Values(body)
	if err != nil {
		return nil, err
	}
	bodyBytes := []byte(vals.Encode())

	// when starting a new session, we don't know the DPoP nonce yet
	dpopServerNonce := ""

	// create new key for the session
	dpopPrivKey, err := crypto.GeneratePrivateKeyP256()
	if err != nil {
		return nil, err
	}

	slog.Debug("sending auth request", "scopes", scopes, "state", state, "redirectURI", app.Config.CallbackURL)

	var resp *http.Response
	for range 2 {
		dpopJWT, err := NewAuthDPoP("POST", parURL, dpopServerNonce, dpopPrivKey)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", parURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("DPoP", dpopJWT)

		resp, err = app.Client.Do(req)
		if err != nil {
			return nil, err
		}

		// update DPoP Nonce
		dpopServerNonce = resp.Header.Get("DPoP-Nonce")

		// check for an error condition caused by an out of date DPoP nonce
		// note that the HTTP status code would be 400 Bad Request on token endpoint, not 401 Unauthorized like it would be on Resource Server requests
		if resp.StatusCode == http.StatusBadRequest && dpopServerNonce != "" {
			// parseAuthErrorReason() always closes resp.Body
			reason := parseAuthErrorReason(resp, "PAR")
			if reason == "use_dpop_nonce" {
				// already updated nonce value above; loop around and try again
				continue
			}
			return nil, fmt.Errorf("PAR request failed (HTTP %d): %s", resp.StatusCode, reason)
		}

		// otherwise process result
		break
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		reason := parseAuthErrorReason(resp, "PAR")
		return nil, fmt.Errorf("PAR request failed (HTTP %d): %s", resp.StatusCode, reason)
	}

	var parResp PushedAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&parResp); err != nil {
		return nil, fmt.Errorf("auth request (PAR) response failed to decode: %w", err)
	}

	parInfo := AuthRequestData{
		State:                   state,
		AuthServerURL:           authMeta.Issuer,
		Scopes:                  scopes,
		PKCEVerifier:            pkceVerifier,
		RequestURI:              parResp.RequestURI,
		AuthServerTokenEndpoint: authMeta.TokenEndpoint,
		DPoPAuthServerNonce:     dpopServerNonce,
		DPoPPrivateKeyMultibase: dpopPrivKey.Multibase(),
	}

	return &parInfo, nil
}

// Lower-level helper. This is usually invoked as part of [ClientApp.ProcessCallback].
func (app *ClientApp) SendInitialTokenRequest(ctx context.Context, authCode string, info AuthRequestData) (*TokenResponse, error) {

	body := InitialTokenRequest{
		ClientID:     app.Config.ClientID,
		RedirectURI:  app.Config.CallbackURL,
		GrantType:    "authorization_code",
		Code:         authCode,
		CodeVerifier: info.PKCEVerifier,
	}

	if app.Config.IsConfidential() {
		clientAssertion, err := app.Config.NewClientAssertion(info.AuthServerURL)
		if err != nil {
			return nil, err
		}
		body.ClientAssertionType = &ClientAssertionJWTBearer
		body.ClientAssertion = &clientAssertion
	}

	dpopPrivKey, err := crypto.ParsePrivateMultibase(info.DPoPPrivateKeyMultibase)
	if err != nil {
		return nil, err
	}

	vals, err := query.Values(body)
	if err != nil {
		return nil, err
	}
	bodyBytes := []byte(vals.Encode())

	dpopServerNonce := info.DPoPAuthServerNonce

	var resp *http.Response
	for range 2 {
		dpopJWT, err := NewAuthDPoP("POST", info.AuthServerTokenEndpoint, dpopServerNonce, dpopPrivKey)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", info.AuthServerTokenEndpoint, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("DPoP", dpopJWT)

		resp, err = app.Client.Do(req)
		if err != nil {
			return nil, err
		}

		// check if a nonce was provided
		dpopNonceHdr := resp.Header.Get("DPoP-Nonce")
		if dpopNonceHdr != "" && dpopNonceHdr != dpopServerNonce {
			dpopServerNonce = dpopNonceHdr
		}

		// check for an error condition caused by an out of date DPoP nonce
		// note that the HTTP status code would be 400 Bad Request on token endpoint, not 401 Unauthorized like it would be on Resource Server requests
		if resp.StatusCode == http.StatusBadRequest && dpopNonceHdr != "" {
			// parseAuthErrorReason() always closes resp.Body
			reason := parseAuthErrorReason(resp, "initial-token")
			if reason == "use_dpop_nonce" {
				// already updated nonce value above; loop around and try again
				continue
			}
			return nil, fmt.Errorf("initial token request failed (HTTP %d): %s", resp.StatusCode, reason)
		}

		// otherwise process result
		break
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		reason := parseAuthErrorReason(resp, "initial-token")
		return nil, fmt.Errorf("initial token request failed (HTTP %d): %s", resp.StatusCode, reason)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("token response failed to decode: %w", err)
	}

	return &tokenResp, nil
}

// High-level helper for starting a new session. Resolves identifier to resource server and auth server metadata, sends PAR request, persists request info to store, and returns a redirect URL.
//
// The `identifier` argument can be an atproto account identifier (handle or DID), or can be a URL to the account's auth server.
//
// The returned sting will be a web URL that the user should be redirected to (in browser) to approve the auth flow.
func (app *ClientApp) StartAuthFlow(ctx context.Context, identifier string) (string, error) {

	var authserverURL string
	var accountDID syntax.DID

	if strings.HasPrefix(identifier, "https://") {
		authserverURL = identifier
		identifier = ""
	} else {
		atid, err := syntax.ParseAtIdentifier(identifier)
		if err != nil {
			return "", fmt.Errorf("not a valid account identifier (%s): %w", identifier, err)
		}
		ident, err := app.Dir.Lookup(ctx, *atid)
		if err != nil {
			return "", fmt.Errorf("failed to resolve username (%s): %w", identifier, err)
		}
		accountDID = ident.DID
		host := ident.PDSEndpoint()
		if host == "" {
			return "", fmt.Errorf("identity does not link to an atproto host (PDS)")
		}

		// TODO: logger on ClientApp?
		logger := slog.Default().With("did", ident.DID, "handle", ident.Handle, "host", host)
		logger.Debug("resolving to auth server metadata")
		authserverURL, err = app.Resolver.ResolveAuthServerURL(ctx, host)
		if err != nil {
			return "", fmt.Errorf("resolving auth server: %w", err)
		}
	}

	authserverMeta, err := app.Resolver.ResolveAuthServerMetadata(ctx, authserverURL)
	if err != nil {
		return "", fmt.Errorf("fetching auth server metadata: %w", err)
	}

	info, err := app.SendAuthRequest(ctx, authserverMeta, app.Config.Scopes, identifier)
	if err != nil {
		return "", fmt.Errorf("auth request failed: %w", err)
	}

	if accountDID != "" {
		info.AccountDID = &accountDID
	}

	// persist auth request info
	app.Store.SaveAuthRequestInfo(ctx, *info)

	params := url.Values{}
	params.Set("client_id", app.Config.ClientID)
	params.Set("request_uri", info.RequestURI)

	// AuthorizationEndpoint was already checked to be a clean URL
	// TODO: could do additional SSRF checks on the redirect domain here
	redirectURL := fmt.Sprintf("%s?%s", authserverMeta.AuthorizationEndpoint, params.Encode())
	return redirectURL, nil
}

// High-level helper for completing auth flow: verifies callback query parameters against persisted auth request info, makes initial token request to the auth server, validates account identifier, and persists session data.
func (app *ClientApp) ProcessCallback(ctx context.Context, params url.Values) (*ClientSessionData, error) {

	state := params.Get("state")
	authserverURL := params.Get("iss")
	authCode := params.Get("code")
	if state == "" || authserverURL == "" || authCode == "" {
		return nil, fmt.Errorf("missing required query param")
	}

	info, err := app.Store.GetAuthRequestInfo(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("loading auth request info: %w", err)
	}

	if info.State != state || info.AuthServerURL != authserverURL {
		return nil, fmt.Errorf("callback params don't match request info")
	}

	tokenResp, err := app.SendInitialTokenRequest(ctx, authCode, *info)
	if err != nil {
		return nil, fmt.Errorf("initial token request: %w", err)
	}

	// verify against account/server from start of login
	var accountDID syntax.DID
	var hostURL string
	if info.AccountDID != nil {
		// if we started with an account DID, verify it against the subject
		accountDID = *info.AccountDID
		if tokenResp.Subject != info.AccountDID.String() {
			return nil, fmt.Errorf("token subject didn't match original DID")
		}
		// identity lookup for PDS hostname; this should be cached
		ident, err := app.Dir.LookupDID(ctx, accountDID)
		if err != nil {
			return nil, err
		}
		hostURL = ident.PDSEndpoint()
	} else {
		// if we started with an auth server URL, resolve and verify the identity
		accountDID, err = syntax.ParseDID(tokenResp.Subject)
		if err != nil {
			return nil, err
		}
		ident, err := app.Dir.LookupDID(ctx, accountDID)
		if err != nil {
			return nil, err
		}
		hostURL = ident.PDSEndpoint()
		res, err := app.Resolver.ResolveAuthServerURL(ctx, hostURL)
		if err != nil {
			return nil, fmt.Errorf("resolving auth server: %w", err)
		}
		if res != authserverURL {
			return nil, fmt.Errorf("token subject auth server did not match original")
		}
	}

	sessData := ClientSessionData{
		AccountDID:              accountDID,
		SessionID:               info.State,
		HostURL:                 hostURL,
		AuthServerURL:           info.AuthServerURL,
		AuthServerTokenEndpoint: info.AuthServerTokenEndpoint,
		Scopes:                  strings.Split(tokenResp.Scope, " "),
		AccessToken:             tokenResp.AccessToken,
		RefreshToken:            tokenResp.RefreshToken,
		DPoPAuthServerNonce:     info.DPoPAuthServerNonce,
		DPoPHostNonce:           info.DPoPAuthServerNonce, // bootstrap host nonce from authserver
		DPoPPrivateKeyMultibase: info.DPoPPrivateKeyMultibase,
	}
	if err := app.Store.SaveSession(ctx, sessData); err != nil {
		return nil, err
	}
	if err := app.Store.DeleteAuthRequestInfo(ctx, state); err != nil {
		// only log on failure to delete state info
		slog.Warn("failed to delete auth request info", "state", state, "did", accountDID, "authserver", info.AuthServerURL, "err", err)
	}
	return &sessData, nil
}
