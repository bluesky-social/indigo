package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-querystring/query"
)

var JWT_EXPIRATION_DURATION = 30 * time.Second

// Service-level client. Used to establish and refrsh OAuth sessions, but is not itself account or session specific, and can not be used directly to make API calls on behalf of a user.
type OAuthClient struct {
	Client   *http.Client
	Resolver *Resolver
	Config   *ClientConfig
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

func (conf *ClientConfig) IsConfidential() bool {
	return conf.PrivateKey != nil && conf.KeyID != nil
}

func NewClientConfig(clientID, callbackURL string) ClientConfig {
	c := ClientConfig{
		ClientID:    clientID,
		CallbackURL: callbackURL,
	}
	return c
}

// Returns public JWK corresponding to the client's (private) attestation key.
//
// If the client does not have a key (eg, a non-confidential client), returns an error.
func (c *ClientConfig) PublicJWK() (*crypto.JWK, error) {
	if c.PrivateKey == nil || c.KeyID == nil {
		return nil, fmt.Errorf("non-confidential client has no public JWK")
	}
	pub, err := c.PrivateKey.PublicKey()
	if err != nil {
		return nil, err
	}
	jwk, err := pub.JWK()
	if err != nil {
		return nil, err
	}
	jwk.KeyID = c.KeyID
	return jwk, nil
}

// Returns a "JWKS" representation of public keys for the client. This can be returned as JSON, as part of client metadata.
//
// If the client does not have any keys, returns an empty set.
func (c *ClientConfig) PublicJWKS() JWKS {
	// public client with no keys
	if c.PrivateKey == nil {
		return JWKS{}
	}
	jwk, err := c.PublicJWK()
	if err != nil {
		return JWKS{}
	}
	jwks := JWKS{
		Keys: []crypto.JWK{*jwk},
	}
	return jwks
}

// Returns a ClientMetadata struct with the required fields populated based on this client configuration. Clients may want to populate additional metadata fields on top of this response.
//
// TODO: confidential clients currently must provide JWKSUri after the fact
func (c *ClientConfig) ClientMetadata(scope string) ClientMetadata {
	if scope == "" {
		scope = "atproto"
	}
	m := ClientMetadata{
		ClientID:              c.ClientID,
		ApplicationType:       strPtr("web"),
		GrantTypes:            []string{"authorization_code", "refresh_token"},
		Scope:                 scope,
		ResponseTypes:         []string{"code"},
		RedirectURIs:          []string{c.CallbackURL},
		DpopBoundAccessTokens: true,
	}
	if c.IsConfidential() {
		m.TokenEndpointAuthMethod = strPtr("private_key_jwt")
		m.TokenEndpointAuthSigningAlg = strPtr("ES256") // XXX
		// TODO: what is the correct format for in-line JWKS?
		//m.JWKS = c.JWKS()
	}
	return m
}

type Session struct {
	// HTTP client used for token refresh requests
	Client *http.Client

	Config         *ClientConfig
	Data           *SessionData
	DpopPrivateKey crypto.PrivateKey
}

func ResumeSession(config *ClientConfig, data *SessionData) (*Session, error) {
	sess := Session{
		Client: http.DefaultClient,
		Config: config,
		Data:   data,
	}
	priv, err := crypto.ParsePrivateMultibase(data.DpopPrivateKeyMultibase)
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

func (conf *ClientConfig) NewAssertionJWT(authURL string) (string, error) {
	if !conf.IsConfidential() {
		return "", fmt.Errorf("non-confidential client")
	}
	claims := clientAssertionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   conf.ClientID,
			Subject:  conf.ClientID,
			Audience: []string{authURL},
			ID:       randomNonce(),
			IssuedAt: jwt.NewNumericDate(time.Now()),
		},
	}

	signingMethod, err := keySigningMethod(conf.PrivateKey)
	if err != nil {
		return "", err
	}

	token := jwt.NewWithClaims(signingMethod, claims)
	token.Header["kid"] = conf.KeyID
	return token.SignedString(conf.PrivateKey)
}

func NewDPoPJWT(httpMethod, url, dpopNonce string, privKey crypto.PrivateKey) (string, error) {

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

	// TODO: parse/cache this elsewhere
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
func (c *ClientConfig) SendAuthRequest(ctx context.Context, authMeta *AuthServerMetadata, loginHint, scope string) (*AuthRequestData, error) {
	// TODO: pass as argument?
	httpClient := http.DefaultClient

	parURL := authMeta.PushedAuthorizationRequestEndpoint
	state := randomNonce()
	pkceVerifier := fmt.Sprintf("%s%s%s", randomNonce(), randomNonce(), randomNonce())

	// generate PKCE code challenge for use in PAR request
	codeChallenge := S256CodeChallenge(pkceVerifier)

	// self-signed JWT using private key in client metadata (confidential client)
	// TODO: make "confidential client" mode optional
	assertionJWT, err := c.NewAssertionJWT(authMeta.Issuer)
	if err != nil {
		return nil, err
	}

	body := PushedAuthRequest{
		ClientID:            c.ClientID,
		State:               state,
		RedirectURI:         c.CallbackURL,
		Scope:               scope,
		ResponseType:        "code",
		ClientAssertionType: CLIENT_ASSERTION_JWT_BEARER,
		ClientAssertion:     assertionJWT,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
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

	slog.Info("sending auth request", "scope", scope, "state", state, "redirectURI", c.CallbackURL)

	var resp *http.Response
	for range 2 {
		dpopJWT, err := NewDPoPJWT("POST", parURL, dpopServerNonce, dpopPrivKey)
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

func (c *ClientConfig) SendInitialTokenRequest(ctx context.Context, authCode string, info AuthRequestData) (*TokenResponse, error) {

	clientAssertion, err := c.NewAssertionJWT(info.AuthServerURL)
	if err != nil {
		return nil, err
	}

	// XXX: pass in?
	resolv := NewResolver()
	httpClient := http.DefaultClient

	// TODO: don't re-fetch? caching?
	authServerMeta, err := resolv.ResolveAuthServerMetadata(ctx, info.AuthServerURL)
	if err != nil {
		return nil, err
	}

	body := InitialTokenRequest{
		ClientID:            c.ClientID,
		RedirectURI:         c.CallbackURL,
		GrantType:           "authorization_code",
		Code:                authCode,
		CodeVerifier:        info.PKCEVerifier,
		ClientAssertionType: &CLIENT_ASSERTION_JWT_BEARER,
		ClientAssertion:     &clientAssertion,
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
		dpopJWT, err := NewDPoPJWT("POST", authServerMeta.TokenEndpoint, dpopServerNonce, dpopPrivKey)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", authServerMeta.TokenEndpoint, bytes.NewBuffer(bodyBytes))
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

func (sess *Session) RefreshTokens(ctx context.Context) error {

	// TODO: assuming confidential client
	clientAssertion, err := sess.Config.NewAssertionJWT(sess.Data.AuthServerURL)
	if err != nil {
		return err
	}

	body := RefreshTokenRequest{
		ClientID:            sess.Config.ClientID,
		GrantType:           "authorization_code",
		RefreshToken:        sess.Data.RefreshToken,
		ClientAssertionType: &CLIENT_ASSERTION_JWT_BEARER,
		ClientAssertion:     &clientAssertion,
	}

	vals, err := query.Values(body)
	if err != nil {
		return err
	}
	bodyBytes := []byte(vals.Encode())

	// XXX: persist this back to the data?
	dpopServerNonce := sess.Data.DpopAuthServerNonce
	tokenURL := sess.Data.AuthServerTokenEndpoint

	var resp *http.Response
	for range 2 {
		dpopJWT, err := NewDPoPJWT("POST", tokenURL, dpopServerNonce, sess.DpopPrivateKey)
		if err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, bytes.NewBuffer(bodyBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("DPoP", dpopJWT)

		resp, err = sess.Client.Do(req)
		if err != nil {
			return err
		}

		// check if a nonce was provided
		dpopServerNonce = resp.Header.Get("DPoP-Nonce")
		if resp.StatusCode == 400 && dpopServerNonce != "" {
			// TODO: also check that body is JSON with an 'error' string field value of 'use_dpop_nonce'
			var errResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				slog.Warn("initial token request failed", "authServer", tokenURL, "err", err, "statusCode", resp.StatusCode)
			} else {
				slog.Warn("initial token request failed", "authServer", tokenURL, "resp", errResp, "statusCode", resp.StatusCode)
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
			slog.Warn("initial token request failed", "authServer", tokenURL, "err", err, "statusCode", resp.StatusCode)
		} else {
			slog.Warn("initial token request failed", "authServer", tokenURL, "resp", errResp, "statusCode", resp.StatusCode)
		}
		return fmt.Errorf("initial token request failed: HTTP %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("token response failed to decode: %w", err)
	}
	// XXX: more validation of response?

	sess.Data.AccessToken = tokenResp.AccessToken
	sess.Data.RefreshToken = tokenResp.RefreshToken

	return nil
}

func (sess *Session) NewAccessDPoP(method, reqURL string) (string, error) {

	ath := S256CodeChallenge(sess.Data.AccessToken)
	claims := dpopClaims{
		HTTPMethod:      method,
		TargetURI:       reqURL,
		AccessTokenHash: &ath,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    sess.Data.AuthServerURL,
			ID:        randomNonce(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(JWT_EXPIRATION_DURATION)),
		},
	}
	if sess.Data.DpopHostNonce != "" {
		claims.Nonce = &sess.Data.DpopHostNonce
	}

	keyMethod, err := keySigningMethod(sess.DpopPrivateKey)
	if err != nil {
		return "", err
	}

	// TODO: parse/cache this elsewhere
	pub, err := sess.DpopPrivateKey.PublicKey()
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
	return token.SignedString(sess.DpopPrivateKey)
}

func (sess *Session) DoWithAuth(c *http.Client, req *http.Request, endpoint syntax.NSID) (*http.Response, error) {

	// XXX: copy URL and strip query params
	u := req.URL.String()

	dpopServerNonce := sess.Data.DpopHostNonce
	var resp *http.Response
	for range 2 {
		dpopJWT, err := sess.NewAccessDPoP(req.Method, u)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", fmt.Sprintf("DPoP %s", sess.Data.AccessToken))
		req.Header.Set("DPoP", dpopJWT)

		resp, err = c.Do(req)
		if err != nil {
			return nil, err
		}

		// check if a nonce was provided
		dpopServerNonce = resp.Header.Get("DPoP-Nonce")
		if resp.StatusCode == 400 && dpopServerNonce != "" {
			// TODO: also check that body is JSON with an 'error' string field value of 'use_dpop_nonce'
			var errResp map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
				slog.Warn("authorized request failed", "url", u, "err", err, "statusCode", resp.StatusCode)
			} else {
				slog.Warn("authorized request failed", "url", u, "resp", errResp, "statusCode", resp.StatusCode)
			}

			// XXX: doesn't really work, body might be drained second time
			// loop around try again
			resp.Body.Close()
			continue
		}
		// otherwise process result
		break
	}
	// TODO: check for auth-specific errors, and return them as err
	return resp, nil
}
