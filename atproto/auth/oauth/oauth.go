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

var JWT_EXPIRATION_DURATION = 30 * time.Second

// Service-level client. Used to establish and refrsh OAuth sessions, but is not itself account or session specific, and can not be used directly to make API calls on behalf of a user.
type ClientApp struct {
	Client   *http.Client
	Resolver *Resolver
	Dir      identity.Directory
	Config   *ClientConfig
	Store    ClientAuthStore
}

type ClientConfig struct {
	ClientID    string
	CallbackURL string

	UserAgent string

	// For confidential clients, the private client assertion key. Note that while an interface is used here, only P-256 is allowed by the current specification.
	PrivateKey crypto.PrivateKey

	// ID for current client assertion key (should be provided if PrivateKey is)
	KeyID *string
}

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
		// TODO: some way to wire UserAgent through to identity directory
	}
	return app
}

func NewPublicConfig(clientID, callbackURL string) ClientConfig {
	c := ClientConfig{
		ClientID:    clientID,
		CallbackURL: callbackURL,
		UserAgent:   "indigo-sdk",
	}
	return c
}

func NewLocalhostConfig(callbackURL, scope string) ClientConfig {
	slog.Info("NewLocalhostConfig", "callbackURL", callbackURL)
	params := make(url.Values)
	params.Set("redirect_uri", callbackURL)
	params.Set("scope", scope)
	c := ClientConfig{
		ClientID:    fmt.Sprintf("http://localhost?%s", params.Encode()),
		CallbackURL: callbackURL,
		UserAgent:   "indigo-sdk",
	}
	slog.Info("DONE NewLocalhostConfig", "callbackURL", c.CallbackURL)
	return c
}

func (config *ClientConfig) IsConfidential() bool {
	return config.PrivateKey != nil && config.KeyID != nil
}

func (config *ClientConfig) AddClientSecret(priv crypto.PrivateKey, keyID string) {
	config.PrivateKey = priv
	config.KeyID = &keyID
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

// Returns a ClientMetadata struct with the required fields populated based on this client configuration. Clients may want to populate additional metadata fields on top of this response.
//
// NOTE: confidential clients currently must provide JWKSUri after the fact
func (config *ClientConfig) ClientMetadata(scope string) ClientMetadata {
	if scope == "" {
		scope = "atproto"
	}
	m := ClientMetadata{
		ClientID:                config.ClientID,
		ApplicationType:         strPtr("web"),
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		Scope:                   scope,
		ResponseTypes:           []string{"code"},
		RedirectURIs:            []string{config.CallbackURL},
		DpopBoundAccessTokens:   true,
		TokenEndpointAuthMethod: strPtr("none"),
	}
	if config.IsConfidential() {
		m.TokenEndpointAuthMethod = strPtr("private_key_jwt")
		m.TokenEndpointAuthSigningAlg = strPtr("ES256") // XXX
		// TODO: need to include 'use' or 'key_ops' for JWKS in the client metadata doc?
		//jwks := config.PublicJWKS()
		//m.JWKS = &jwks
	}
	return m
}

func (app *ClientApp) ResumeSession(ctx context.Context, did syntax.DID) (*ClientSession, error) {

	sd, err := app.Store.GetSession(ctx, did)
	if err != nil {
		return nil, err
	}

	sess := ClientSession{
		Client: app.Client,
		Config: app.Config,
		Data:   sd,
	}
	// XXX: configure token refresh callback

	// XXX: refactor this in to store layer?
	priv, err := crypto.ParsePrivateMultibase(sd.DpopPrivateKeyMultibase)
	if err != nil {
		return nil, err
	}
	sess.DpopPrivateKey = priv
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

func (cfg *ClientConfig) NewClientAssertion(authURL string) (string, error) {
	if !cfg.IsConfidential() {
		return "", fmt.Errorf("non-confidential client")
	}
	claims := clientAssertionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   cfg.ClientID,
			Subject:  cfg.ClientID,
			Audience: []string{authURL},
			ID:       randomNonce(),
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
			ID:        randomNonce(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(JWT_EXPIRATION_DURATION)),
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

// Sends PAR request to auth server
func (app *ClientApp) SendAuthRequest(ctx context.Context, authMeta *AuthServerMetadata, scope, loginHint string) (*AuthRequestData, error) {
	// TODO: pass as argument?
	httpClient := http.DefaultClient

	parURL := authMeta.PushedAuthorizationRequestEndpoint
	state := randomNonce()
	pkceVerifier := fmt.Sprintf("%s%s%s", randomNonce(), randomNonce(), randomNonce())

	// generate PKCE code challenge for use in PAR request
	codeChallenge := S256CodeChallenge(pkceVerifier)

	slog.Info("preparing PAR", "client_id", app.Config.ClientID, "callback_url", app.Config.CallbackURL)
	body := PushedAuthRequest{
		ClientID:            app.Config.ClientID,
		State:               state,
		RedirectURI:         app.Config.CallbackURL,
		Scope:               scope,
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
		body.ClientAssertionType = CLIENT_ASSERTION_JWT_BEARER
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

	dpopServerNonce := ""

	// create new key for the session
	dpopPrivKey, err := crypto.GeneratePrivateKeyP256()
	if err != nil {
		return nil, err
	}

	slog.Info("sending auth request", "scope", scope, "state", state, "redirectURI", app.Config.CallbackURL)

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

		resp, err = httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		// check if a nonce was provided
		dpopServerNonce = resp.Header.Get("DPoP-Nonce")
		if resp.StatusCode == 400 && dpopServerNonce != "" {
			// TODO: also check that body is JSON with an 'error' string field value of 'use_dpop_nonce'
			var errResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				slog.Warn("PAR request failed", "authServer", parURL, "err", err, "statusCode", resp.StatusCode)
			} else {
				slog.Warn("PAR request failed", "authServer", parURL, "resp", errResp, "statusCode", resp.StatusCode)
			}

			// loop around try again
			resp.Body.Close()
			continue
		}
		// otherwise process result
		break
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		var errResp map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			slog.Warn("PAR request failed", "authServer", parURL, "err", err, "statusCode", resp.StatusCode)
		} else {
			slog.Warn("PAR request failed", "authServer", parURL, "resp", errResp, "statusCode", resp.StatusCode)
		}
		return nil, fmt.Errorf("auth request (PAR) failed: HTTP %d", resp.StatusCode)
	}

	var parResp PushedAuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&parResp); err != nil {
		return nil, fmt.Errorf("auth request (PAR) response failed to decode: %w", err)
	}

	parInfo := AuthRequestData{
		State:                   state,
		AuthServerURL:           authMeta.Issuer,
		Scope:                   scope,
		PKCEVerifier:            pkceVerifier,
		RequestURI:              parResp.RequestURI,
		DpopAuthServerNonce:     dpopServerNonce,
		DpopPrivateKeyMultibase: dpopPrivKey.Multibase(),
	}

	return &parInfo, nil
}

func (app *ClientApp) SendInitialTokenRequest(ctx context.Context, authCode string, info AuthRequestData) (*TokenResponse, error) {

	// TODO: don't re-fetch? caching?
	authServerMeta, err := app.Resolver.ResolveAuthServerMetadata(ctx, info.AuthServerURL)
	if err != nil {
		return nil, err
	}

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
		body.ClientAssertionType = &CLIENT_ASSERTION_JWT_BEARER
		body.ClientAssertion = &clientAssertion
	}

	dpopPrivKey, err := crypto.ParsePrivateMultibase(info.DpopPrivateKeyMultibase)
	if err != nil {
		return nil, err
	}

	vals, err := query.Values(body)
	if err != nil {
		return nil, err
	}
	bodyBytes := []byte(vals.Encode())

	dpopServerNonce := info.DpopAuthServerNonce

	var resp *http.Response
	for range 2 {
		dpopJWT, err := NewAuthDPoP("POST", authServerMeta.TokenEndpoint, dpopServerNonce, dpopPrivKey)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", authServerMeta.TokenEndpoint, bytes.NewBuffer(bodyBytes))
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
		dpopServerNonce = resp.Header.Get("DPoP-Nonce")
		if resp.StatusCode == 400 && dpopServerNonce != "" {
			// TODO: also check that body is JSON with an 'error' string field value of 'use_dpop_nonce'
			var errResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "err", err, "statusCode", resp.StatusCode)
			} else {
				slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "resp", errResp, "statusCode", resp.StatusCode)
			}

			// loop around try again
			resp.Body.Close()
			continue
		}
		// otherwise process result
		break
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var errResp map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "err", err, "statusCode", resp.StatusCode)
		} else {
			slog.Warn("initial token request failed", "authServer", authServerMeta.TokenEndpoint, "resp", errResp, "statusCode", resp.StatusCode)
		}
		return nil, fmt.Errorf("initial token request failed: HTTP %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("token response failed to decode: %w", err)
	}

	return &tokenResp, nil
}

func (app *ClientApp) StartAuthFlow(ctx context.Context, username string) (string, error) {

	var authserverURL string
	var accountDID syntax.DID

	if strings.HasPrefix(username, "https://") {
		authserverURL = username
		username = ""
	} else {
		atid, err := syntax.ParseAtIdentifier(username)
		if err != nil {
			return "", fmt.Errorf("not a valid account identifier (%s): %w", username, err)
		}
		ident, err := app.Dir.Lookup(ctx, *atid)
		if err != nil {
			return "", fmt.Errorf("failed to resolve username (%s): %w", username, err)
		}
		host := ident.PDSEndpoint()
		if host == "" {
			return "", fmt.Errorf("identity does not link to an atproto host (PDS)")
		}

		// TODO: logger on ClientApp?
		logger := slog.Default().With("did", ident.DID, "handle", ident.Handle, "host", host)
		logger.Info("resolving to auth server metadata")
		authserverURL, err = app.Resolver.ResolveAuthServerURL(ctx, host)
		if err != nil {
			return "", fmt.Errorf("resolving auth server: %w", err)
		}
	}

	authserverMeta, err := app.Resolver.ResolveAuthServerMetadata(ctx, authserverURL)
	if err != nil {
		return "", fmt.Errorf("fetching auth server metadata: %w", err)
	}

	// XXX: scope from config
	scope := "atproto transition:generic"
	info, err := app.SendAuthRequest(ctx, authserverMeta, scope, username)
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

	// TODO: check that 'authorization_endpoint' is "safe" (?)
	redirectURL := fmt.Sprintf("%s?%s", authserverMeta.AuthorizationEndpoint, params.Encode())
	return redirectURL, nil
}

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

	// TODO: could be flexible instead of considering this a hard failure?
	if tokenResp.Scope != info.Scope {
		return nil, fmt.Errorf("token scope didn't match original request")
	}

	sessData := ClientSessionData{
		AccountDID:              accountDID,
		HostURL:                 hostURL,
		AuthServerURL:           info.AuthServerURL,
		AccessToken:             tokenResp.AccessToken,
		RefreshToken:            tokenResp.RefreshToken,
		DpopAuthServerNonce:     info.DpopAuthServerNonce,
		DpopHostNonce:           info.DpopAuthServerNonce, // bootstrap host nonce from authserver
		DpopPrivateKeyMultibase: info.DpopPrivateKeyMultibase,
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
